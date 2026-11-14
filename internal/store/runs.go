package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store/db"
)

var (
	ErrIdempotencyConflict   = errors.New("idempotency key was used with a different request")
	ErrIdempotencyInProgress = errors.New("idempotent request is still in progress")
)

type SubmitRunParams struct {
	OwnerID            uuid.UUID
	WorkflowID         uuid.UUID
	IdempotencyKey     string
	Input              json.RawMessage
	CostBudgetMicrousd *int64
	Deadline           *time.Time
}

type SubmittedRun struct {
	Run      db.WorkflowRun
	Existing bool
}

func (s *Store) SubmitRun(ctx context.Context, params SubmitRunParams) (SubmittedRun, error) {
	params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
	if len(params.IdempotencyKey) < 1 || len(params.IdempotencyKey) > 255 {
		return SubmittedRun{}, errors.New("idempotency key must be between 1 and 255 characters")
	}
	if len(params.Input) == 0 {
		params.Input = json.RawMessage(`{}`)
	}
	normalizedInput, err := normalizeJSONObject(params.Input)
	if err != nil {
		return SubmittedRun{}, err
	}
	params.Input = normalizedInput
	if params.CostBudgetMicrousd != nil && *params.CostBudgetMicrousd < 0 {
		return SubmittedRun{}, errors.New("cost budget must not be negative")
	}
	if params.Deadline != nil {
		utcDeadline := params.Deadline.UTC()
		if !utcDeadline.After(time.Now().UTC()) {
			return SubmittedRun{}, errors.New("deadline must be in the future")
		}
		params.Deadline = &utcDeadline
	}
	requestHash, err := runRequestHash(params)
	if err != nil {
		return SubmittedRun{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return SubmittedRun{}, fmt.Errorf("begin run transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	version, err := q.GetLatestWorkflowVersion(ctx, db.GetLatestWorkflowVersionParams{WorkflowID: params.WorkflowID, OwnerID: params.OwnerID})
	if err != nil {
		return SubmittedRun{}, classifyReadError("get latest workflow version", err)
	}

	now := time.Now().UTC()
	scope := "workflow-run:" + params.WorkflowID.String()
	idempotencyID, insertErr := q.TryCreateIdempotencyKey(ctx, db.TryCreateIdempotencyKeyParams{
		OwnerID: params.OwnerID, Scope: scope, Key: params.IdempotencyKey, RequestSha256: requestHash,
		LockedUntil: pgtype.Timestamptz{Time: now.Add(30 * time.Second), Valid: true},
		ExpiresAt:   pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
	})
	if insertErr != nil && !errors.Is(insertErr, pgx.ErrNoRows) {
		return SubmittedRun{}, fmt.Errorf("claim idempotency key: %w", insertErr)
	}
	if errors.Is(insertErr, pgx.ErrNoRows) {
		existingKey, lockErr := q.LockIdempotencyKey(ctx, db.LockIdempotencyKeyParams{OwnerID: params.OwnerID, Scope: scope, Key: params.IdempotencyKey})
		if lockErr != nil {
			return SubmittedRun{}, classifyReadError("lock idempotency key", lockErr)
		}
		if existingKey.RequestSha256 != requestHash {
			return SubmittedRun{}, ErrIdempotencyConflict
		}
		if existingKey.ResourceID.Valid {
			run, getErr := q.GetRun(ctx, db.GetRunParams{ID: existingKey.ResourceID.Bytes, OwnerID: params.OwnerID})
			if getErr != nil {
				return SubmittedRun{}, classifyReadError("get idempotent run", getErr)
			}
			if err := tx.Commit(ctx); err != nil {
				return SubmittedRun{}, fmt.Errorf("commit idempotent replay: %w", err)
			}
			return SubmittedRun{Run: run, Existing: true}, nil
		}
		return SubmittedRun{}, ErrIdempotencyInProgress
	}

	var definition domain.WorkflowDefinition
	if err := json.Unmarshal(version.Definition, &definition); err != nil {
		return SubmittedRun{}, fmt.Errorf("decode persisted workflow definition: %w", err)
	}
	if err := domain.ValidateWorkflow(definition, domain.DefaultWorkflowLimits); err != nil {
		return SubmittedRun{}, fmt.Errorf("persisted workflow definition is invalid: %w", err)
	}

	deadline := pgtype.Timestamptz{}
	if params.Deadline != nil {
		deadline = pgtype.Timestamptz{Time: params.Deadline.UTC(), Valid: true}
	}
	run, err := q.CreateWorkflowRun(ctx, db.CreateWorkflowRunParams{
		WorkflowVersionID:  version.ID,
		OwnerID:            params.OwnerID,
		Input:              params.Input,
		CostBudgetMicrousd: params.CostBudgetMicrousd,
		DeadlineAt:         deadline,
	})
	if err != nil {
		return SubmittedRun{}, classifyWriteError("create workflow run", err)
	}
	for _, node := range definition.Nodes {
		if _, err := q.CreateNodeRun(ctx, db.CreateNodeRunParams{
			RunID: run.ID, NodeKey: node.ID, NodeType: string(node.Type), DependencyCount: int32(len(node.DependsOn)),
		}); err != nil {
			return SubmittedRun{}, classifyWriteError("create node run", err)
		}
	}
	sequence, err := q.AllocateEventSequence(ctx, run.ID)
	if err != nil {
		return SubmittedRun{}, fmt.Errorf("allocate run event sequence: %w", err)
	}
	eventPayload, _ := json.Marshal(map[string]any{"workflow_version_id": version.ID, "node_count": len(definition.Nodes)})
	if _, err := q.AppendExecutionEvent(ctx, db.AppendExecutionEventParams{
		RunID: run.ID, Sequence: sequence, EventType: "run.created", Payload: eventPayload,
	}); err != nil {
		return SubmittedRun{}, classifyWriteError("append run created event", err)
	}
	responseBody, _ := json.Marshal(map[string]uuid.UUID{"run_id": run.ID})
	statusAccepted := int32(202)
	rows, err := q.CompleteIdempotencyKey(ctx, db.CompleteIdempotencyKeyParams{
		ResponseStatus: &statusAccepted,
		ResponseBody:   responseBody,
		ResourceID:     pgtype.UUID{Bytes: run.ID, Valid: true},
		ID:             idempotencyID,
	})
	if err != nil {
		return SubmittedRun{}, fmt.Errorf("complete idempotency record: %w", err)
	}
	if rows != 1 {
		return SubmittedRun{}, errors.New("complete idempotency record: stale claim")
	}
	if err := tx.Commit(ctx); err != nil {
		return SubmittedRun{}, classifyWriteError("commit run", err)
	}
	return SubmittedRun{Run: run}, nil
}

func (s *Store) GetRun(ctx context.Context, ownerID, runID uuid.UUID) (db.WorkflowRun, error) {
	run, err := s.queries.GetRun(ctx, db.GetRunParams{ID: runID, OwnerID: ownerID})
	if err != nil {
		return db.WorkflowRun{}, classifyReadError("get run", err)
	}
	return run, nil
}

func (s *Store) ListRuns(ctx context.Context, ownerID uuid.UUID, status *string, pageSize int32) ([]db.WorkflowRun, error) {
	runs, err := s.queries.ListRuns(ctx, db.ListRunsParams{OwnerID: ownerID, Status: status, PageSize: pageSize})
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	return runs, nil
}

func (s *Store) ListRunNodes(ctx context.Context, ownerID, runID uuid.UUID) ([]db.NodeRun, error) {
	if _, err := s.GetRun(ctx, ownerID, runID); err != nil {
		return nil, err
	}
	nodes, err := s.queries.ListRunNodes(ctx, db.ListRunNodesParams{RunID: runID, OwnerID: ownerID})
	if err != nil {
		return nil, fmt.Errorf("list run nodes: %w", err)
	}
	return nodes, nil
}

func (s *Store) ListNodeAttempts(ctx context.Context, ownerID, runID, nodeRunID uuid.UUID) ([]db.NodeAttempt, error) {
	if _, err := s.GetRun(ctx, ownerID, runID); err != nil {
		return nil, err
	}
	attempts, err := s.queries.ListNodeAttempts(ctx, db.ListNodeAttemptsParams{
		RunID: runID, NodeRunID: nodeRunID, OwnerID: ownerID,
	})
	if err != nil {
		return nil, fmt.Errorf("list node attempts: %w", err)
	}
	return attempts, nil
}

func (s *Store) ReplayEvents(ctx context.Context, ownerID, runID uuid.UUID, afterSequence int64, pageSize int32) ([]db.ExecutionEvent, error) {
	if _, err := s.GetRun(ctx, ownerID, runID); err != nil {
		return nil, err
	}
	events, err := s.queries.ReplayExecutionEvents(ctx, db.ReplayExecutionEventsParams{
		RunID: runID, AfterSequence: afterSequence, PageSize: pageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("replay execution events: %w", err)
	}
	return events, nil
}

func (s *Store) ListRunUsage(ctx context.Context, ownerID, runID uuid.UUID) ([]db.UsageRecord, error) {
	if _, err := s.GetRun(ctx, ownerID, runID); err != nil {
		return nil, err
	}
	records, err := s.queries.ListRunUsage(ctx, db.ListRunUsageParams{RunID: runID, OwnerID: ownerID})
	if err != nil {
		return nil, fmt.Errorf("list run usage: %w", err)
	}
	return records, nil
}

func (s *Store) CancelRun(ctx context.Context, ownerID, runID uuid.UUID) (db.WorkflowRun, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return db.WorkflowRun{}, fmt.Errorf("begin cancellation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	run, err := q.RequestRunCancellation(ctx, db.RequestRunCancellationParams{ID: runID, OwnerID: ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := q.GetRun(ctx, db.GetRunParams{ID: runID, OwnerID: ownerID})
		if getErr != nil {
			return db.WorkflowRun{}, classifyReadError("get run for cancellation", getErr)
		}
		if existing.Status != string(domain.RunCancelled) {
			return db.WorkflowRun{}, ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return db.WorkflowRun{}, fmt.Errorf("commit cancellation replay: %w", err)
		}
		return existing, nil
	}
	if err != nil {
		return db.WorkflowRun{}, classifyWriteError("request run cancellation", err)
	}
	if _, err := q.CancelInactiveNodes(ctx, runID); err != nil {
		return db.WorkflowRun{}, fmt.Errorf("cancel inactive nodes: %w", err)
	}
	sequence, err := q.AllocateEventSequence(ctx, runID)
	if err != nil {
		return db.WorkflowRun{}, fmt.Errorf("allocate cancellation event: %w", err)
	}
	if _, err := q.AppendExecutionEvent(ctx, db.AppendExecutionEventParams{
		RunID: runID, Sequence: sequence, EventType: "run.cancelled", Payload: json.RawMessage(`{}`),
	}); err != nil {
		return db.WorkflowRun{}, fmt.Errorf("append cancellation event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.WorkflowRun{}, classifyWriteError("commit cancellation", err)
	}
	return run, nil
}

func runRequestHash(params SubmitRunParams) (string, error) {
	payload, err := json.Marshal(struct {
		WorkflowID         uuid.UUID       `json:"workflow_id"`
		Input              json.RawMessage `json:"input"`
		CostBudgetMicrousd *int64          `json:"cost_budget_microusd,omitempty"`
		Deadline           *time.Time      `json:"deadline,omitempty"`
	}{params.WorkflowID, params.Input, params.CostBudgetMicrousd, params.Deadline})
	if err != nil {
		return "", fmt.Errorf("marshal idempotent request: %w", err)
	}
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:]), nil
}

func normalizeJSONObject(payload json.RawMessage) (json.RawMessage, error) {
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		return nil, fmt.Errorf("run input must be a JSON object: %w", err)
	}
	if object == nil {
		return nil, errors.New("run input must be a JSON object")
	}
	normalized, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("normalize run input: %w", err)
	}
	return normalized, nil
}
