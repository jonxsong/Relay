package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store/db"
)

var (
	ErrStaleClaim   = errors.New("attempt claim is stale")
	ErrRunCancelled = errors.New("run was cancelled")
)

type AttemptClaim struct {
	AttemptID    uuid.UUID
	NodeRunID    uuid.UUID
	RunID        uuid.UUID
	NodeKey      string
	NodeType     string
	AttemptNo    int32
	FencingToken int64
	WorkerID     string
	Input        json.RawMessage
}

func (s *Store) ClaimAttempt(ctx context.Context, attemptID uuid.UUID, workerID string, leaseDuration time.Duration) (AttemptClaim, error) {
	if workerID == "" {
		return AttemptClaim{}, errors.New("worker id is required")
	}
	if leaseDuration < 5*time.Second || leaseDuration > 10*time.Minute {
		return AttemptClaim{}, errors.New("lease duration must be between 5s and 10m")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AttemptClaim{}, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	attempt, err := q.ClaimNodeAttempt(ctx, db.ClaimNodeAttemptParams{
		ID: attemptID, WorkerID: &workerID,
		LeaseExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(leaseDuration), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return AttemptClaim{}, ErrStaleClaim
	}
	if err != nil {
		return AttemptClaim{}, fmt.Errorf("claim node attempt: %w", err)
	}
	rows, err := q.MarkNodeRunning(ctx, attempt.NodeRunID)
	if err != nil {
		return AttemptClaim{}, fmt.Errorf("mark node running: %w", err)
	}
	if rows != 1 {
		return AttemptClaim{}, ErrStaleClaim
	}
	node, err := q.GetNodeRunByID(ctx, attempt.NodeRunID)
	if err != nil {
		return AttemptClaim{}, classifyReadError("get claimed node", err)
	}
	payload, _ := json.Marshal(map[string]any{"worker_id": workerID, "attempt_no": attempt.AttemptNo})
	if err := appendEvent(ctx, q, node.RunID, "node.running", &node.ID, &attempt.ID, payload); err != nil {
		return AttemptClaim{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AttemptClaim{}, classifyWriteError("commit attempt claim", err)
	}
	return AttemptClaim{
		AttemptID: attempt.ID, NodeRunID: node.ID, RunID: node.RunID, NodeKey: node.NodeKey, NodeType: node.NodeType,
		AttemptNo: attempt.AttemptNo, FencingToken: attempt.FencingToken, WorkerID: workerID, Input: attempt.Input,
	}, nil
}

func (s *Store) RenewLease(ctx context.Context, claim AttemptClaim, leaseDuration time.Duration) error {
	_, err := s.queries.RenewAttemptLease(ctx, db.RenewAttemptLeaseParams{
		ID: claim.AttemptID, FencingToken: claim.FencingToken, WorkerID: &claim.WorkerID,
		LeaseExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(leaseDuration), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStaleClaim
	}
	if err != nil {
		return fmt.Errorf("renew attempt lease: %w", err)
	}
	return nil
}

func (s *Store) CompleteAttemptSuccess(ctx context.Context, claim AttemptClaim, output json.RawMessage) error {
	if len(output) == 0 || !json.Valid(output) {
		return errors.New("node output must be valid JSON")
	}
	if len(output) > 1<<20 {
		return errors.New("node output exceeds 1 MiB")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin completion transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	run, err := q.GetRunByID(ctx, claim.RunID)
	if err != nil {
		return classifyReadError("get run for completion", err)
	}
	if run.Status == string(domain.RunCancelled) {
		if err := cancelClaim(ctx, q, claim); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit cancelled completion: %w", err)
		}
		return ErrRunCancelled
	}
	succeeded := string(domain.NodeSucceeded)
	retryable := false
	if _, err := q.CompleteNodeAttempt(ctx, db.CompleteNodeAttemptParams{
		Status: succeeded, Output: output, Retryable: &retryable, ID: claim.AttemptID,
		FencingToken: claim.FencingToken, WorkerID: &claim.WorkerID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return ErrStaleClaim
	} else if err != nil {
		return fmt.Errorf("complete node attempt: %w", err)
	}
	rows, err := q.CompleteNodeRunSuccess(ctx, db.CompleteNodeRunSuccessParams{ID: claim.NodeRunID, Output: output})
	if err != nil {
		return fmt.Errorf("complete node run: %w", err)
	}
	if rows != 1 {
		return ErrStaleClaim
	}
	if err := unlockDependants(ctx, q, claim); err != nil {
		return err
	}
	if err := appendEvent(ctx, q, claim.RunID, "node.succeeded", &claim.NodeRunID, &claim.AttemptID, output); err != nil {
		return err
	}
	remaining, err := q.CountNonTerminalNodes(ctx, claim.RunID)
	if err != nil {
		return fmt.Errorf("count remaining nodes: %w", err)
	}
	if remaining == 0 {
		completed, err := q.CompleteRunSuccess(ctx, db.CompleteRunSuccessParams{ID: claim.RunID, Output: output})
		if err != nil {
			return fmt.Errorf("complete run: %w", err)
		}
		if completed == 1 {
			if err := appendEvent(ctx, q, claim.RunID, "run.succeeded", nil, nil, output); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return classifyWriteError("commit successful completion", err)
	}
	return nil
}

func (s *Store) CompleteAttemptFailure(ctx context.Context, claim AttemptClaim, code, message string, retryable bool, maxRetries int) error {
	if code == "" {
		code = "execution_failed"
	}
	if len(message) > 4000 {
		message = message[:4000]
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin failure transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, err := q.CompleteNodeAttempt(ctx, db.CompleteNodeAttemptParams{
		Status: string(domain.NodeFailed), ErrorCode: &code, ErrorMessage: &message, Retryable: &retryable,
		ID: claim.AttemptID, FencingToken: claim.FencingToken, WorkerID: &claim.WorkerID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return ErrStaleClaim
	} else if err != nil {
		return fmt.Errorf("complete failed attempt: %w", err)
	}
	if retryable && int(claim.AttemptNo) <= maxRetries {
		policy := domain.RetryPolicy{MaxRetries: maxRetries, BaseDelay: time.Second, MaxDelay: time.Minute, Jitter: 0.2}
		delay, err := policy.Delay(int(claim.AttemptNo), rand.Float64())
		if err != nil {
			return fmt.Errorf("calculate node retry delay: %w", err)
		}
		rows, err := q.ScheduleNodeRetry(ctx, db.ScheduleNodeRetryParams{
			ID: claim.NodeRunID, NextAttemptAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(delay), Valid: true},
		})
		if err != nil {
			return fmt.Errorf("schedule node retry: %w", err)
		}
		if rows != 1 {
			return ErrStaleClaim
		}
		payload, _ := json.Marshal(map[string]any{"attempt_no": claim.AttemptNo, "delay_ms": delay.Milliseconds(), "error_code": code})
		if err := appendEvent(ctx, q, claim.RunID, "node.retry_scheduled", &claim.NodeRunID, &claim.AttemptID, payload); err != nil {
			return err
		}
	} else {
		rows, err := q.CompleteNodeRunFailure(ctx, db.CompleteNodeRunFailureParams{ID: claim.NodeRunID, ErrorCode: &code, ErrorMessage: message})
		if err != nil {
			return fmt.Errorf("fail node run: %w", err)
		}
		if rows != 1 {
			return ErrStaleClaim
		}
		if _, err := q.SkipUnstartedNodes(ctx, claim.RunID); err != nil {
			return fmt.Errorf("skip downstream nodes: %w", err)
		}
		if _, err := q.CompleteRunFailure(ctx, db.CompleteRunFailureParams{ID: claim.RunID, ErrorCode: &code, ErrorMessage: message}); err != nil {
			return fmt.Errorf("fail workflow run: %w", err)
		}
		payload, _ := json.Marshal(map[string]any{"error_code": code, "message": message, "retryable": retryable})
		if err := appendEvent(ctx, q, claim.RunID, "node.failed", &claim.NodeRunID, &claim.AttemptID, payload); err != nil {
			return err
		}
		if err := appendEvent(ctx, q, claim.RunID, "run.failed", nil, nil, payload); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return classifyWriteError("commit failed completion", err)
	}
	return nil
}

func cancelClaim(ctx context.Context, q *db.Queries, claim AttemptClaim) error {
	cancelled := string(domain.NodeCancelled)
	retryable := false
	code := "run_cancelled"
	message := "run was cancelled while the node was executing"
	if _, err := q.CompleteNodeAttempt(ctx, db.CompleteNodeAttemptParams{
		Status: cancelled, ErrorCode: &code, ErrorMessage: &message, Retryable: &retryable,
		ID: claim.AttemptID, FencingToken: claim.FencingToken, WorkerID: &claim.WorkerID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return ErrStaleClaim
	} else if err != nil {
		return fmt.Errorf("cancel node attempt: %w", err)
	}
	if _, err := q.CancelRunningNode(ctx, claim.NodeRunID); err != nil {
		return fmt.Errorf("cancel running node: %w", err)
	}
	return appendEvent(ctx, q, claim.RunID, "node.cancelled", &claim.NodeRunID, &claim.AttemptID, json.RawMessage(`{"reason":"run_cancelled"}`))
}

func unlockDependants(ctx context.Context, q *db.Queries, claim AttemptClaim) error {
	definitionJSON, err := q.GetWorkflowDefinitionForRun(ctx, claim.RunID)
	if err != nil {
		return classifyReadError("get workflow definition for completion", err)
	}
	var definition domain.WorkflowDefinition
	if err := json.Unmarshal(definitionJSON, &definition); err != nil {
		return fmt.Errorf("decode workflow definition for completion: %w", err)
	}
	for _, node := range definition.Nodes {
		dependsOnCompletedNode := false
		for _, dependency := range node.DependsOn {
			if dependency == claim.NodeKey {
				dependsOnCompletedNode = true
				break
			}
		}
		if !dependsOnCompletedNode {
			continue
		}
		if _, err := q.MarkDependencySucceeded(ctx, db.MarkDependencySucceededParams{RunID: claim.RunID, NodeKey: node.ID}); err != nil {
			return fmt.Errorf("unlock dependant %q: %w", node.ID, err)
		}
	}
	return nil
}
