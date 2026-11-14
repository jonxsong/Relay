package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store/db"
)

type ScheduledAttempt struct {
	AttemptID uuid.UUID
	NodeRunID uuid.UUID
	RunID     uuid.UUID
	NodeKey   string
	NodeType  string
	Topic     string
}

type taskEnvelope struct {
	AttemptID uuid.UUID `json:"attempt_id"`
	NodeRunID uuid.UUID `json:"node_run_id"`
	RunID     uuid.UUID `json:"run_id"`
	NodeKey   string    `json:"node_key"`
	NodeType  string    `json:"node_type"`
}

func (s *Store) ScheduleOne(ctx context.Context, globalConcurrency, runConcurrency int32) (ScheduledAttempt, bool, error) {
	if globalConcurrency < 1 || runConcurrency < 1 || runConcurrency > globalConcurrency {
		return ScheduledAttempt{}, false, errors.New("invalid scheduler concurrency limits")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ScheduledAttempt{}, false, fmt.Errorf("begin scheduler transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if err := q.AcquireSchedulerLock(ctx); err != nil {
		return ScheduledAttempt{}, false, fmt.Errorf("acquire scheduler lock: %w", err)
	}
	active, err := q.CountActiveNodes(ctx)
	if err != nil {
		return ScheduledAttempt{}, false, fmt.Errorf("count active nodes: %w", err)
	}
	if active >= globalConcurrency {
		if err := tx.Commit(ctx); err != nil {
			return ScheduledAttempt{}, false, fmt.Errorf("commit scheduler capacity check: %w", err)
		}
		return ScheduledAttempt{}, false, nil
	}
	candidate, err := q.LockOneReadyNode(ctx, runConcurrency)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return ScheduledAttempt{}, false, fmt.Errorf("commit empty scheduler pass: %w", err)
		}
		return ScheduledAttempt{}, false, nil
	}
	if err != nil {
		return ScheduledAttempt{}, false, fmt.Errorf("lock ready node: %w", err)
	}

	input, err := resolveNodeInput(ctx, q, candidate)
	if err != nil {
		return ScheduledAttempt{}, false, err
	}
	attempt, err := q.QueueNodeAttempt(ctx, db.QueueNodeAttemptParams{NodeRunID: candidate.ID, Input: input})
	if err != nil {
		return ScheduledAttempt{}, false, classifyWriteError("queue node attempt", err)
	}
	topic := "relay.tasks." + candidate.NodeType
	envelope := taskEnvelope{
		AttemptID: attempt.ID, NodeRunID: candidate.ID, RunID: candidate.RunID, NodeKey: candidate.NodeKey, NodeType: candidate.NodeType,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return ScheduledAttempt{}, false, fmt.Errorf("marshal task envelope: %w", err)
	}
	if _, err := q.CreateOutboxEvent(ctx, db.CreateOutboxEventParams{
		AggregateType: "node_attempt", AggregateID: attempt.ID, Topic: topic,
		DeduplicationID: "attempt:" + attempt.ID.String(), Payload: payload,
	}); err != nil {
		return ScheduledAttempt{}, false, classifyWriteError("create task outbox event", err)
	}
	startedRows, err := q.MarkRunRunning(ctx, candidate.RunID)
	if err != nil {
		return ScheduledAttempt{}, false, fmt.Errorf("mark run running: %w", err)
	}
	if startedRows == 1 {
		if err := appendEvent(ctx, q, candidate.RunID, "run.started", nil, nil, json.RawMessage(`{}`)); err != nil {
			return ScheduledAttempt{}, false, err
		}
	}
	if err := appendEvent(ctx, q, candidate.RunID, "node.queued", &candidate.ID, &attempt.ID, payload); err != nil {
		return ScheduledAttempt{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ScheduledAttempt{}, false, classifyWriteError("commit scheduled attempt", err)
	}
	return ScheduledAttempt{
		AttemptID: attempt.ID, NodeRunID: candidate.ID, RunID: candidate.RunID,
		NodeKey: candidate.NodeKey, NodeType: candidate.NodeType, Topic: topic,
	}, true, nil
}

func resolveNodeInput(ctx context.Context, q *db.Queries, candidate db.LockOneReadyNodeRow) ([]byte, error) {
	var definition domain.WorkflowDefinition
	if err := json.Unmarshal(candidate.WorkflowDefinition, &definition); err != nil {
		return nil, fmt.Errorf("decode workflow definition for scheduling: %w", err)
	}
	var selected *domain.NodeDefinition
	for i := range definition.Nodes {
		if definition.Nodes[i].ID == candidate.NodeKey {
			selected = &definition.Nodes[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("workflow definition does not contain node %q", candidate.NodeKey)
	}
	var runInput map[string]any
	if err := json.Unmarshal(candidate.RunInput, &runInput); err != nil {
		return nil, fmt.Errorf("decode run input: %w", err)
	}
	rows, err := q.ListNodeOutputsForRun(ctx, candidate.RunID)
	if err != nil {
		return nil, fmt.Errorf("load dependency outputs: %w", err)
	}
	outputs := make(map[string]any, len(rows))
	for _, row := range rows {
		var output any
		if err := json.Unmarshal(row.Output, &output); err != nil {
			return nil, fmt.Errorf("decode output for node %q: %w", row.NodeKey, err)
		}
		outputs[row.NodeKey] = output
	}
	resolved, err := domain.ResolveInputs(*selected, domain.BindingContext{RunInput: runInput, NodeOutputs: outputs})
	if err != nil {
		return nil, fmt.Errorf("resolve inputs for node %q: %w", candidate.NodeKey, err)
	}
	input, err := json.Marshal(resolved)
	if err != nil {
		return nil, fmt.Errorf("marshal inputs for node %q: %w", candidate.NodeKey, err)
	}
	return input, nil
}

func appendEvent(ctx context.Context, q *db.Queries, runID uuid.UUID, eventType string, nodeRunID, attemptID *uuid.UUID, payload json.RawMessage) error {
	sequence, err := q.AllocateEventSequence(ctx, runID)
	if err != nil {
		return fmt.Errorf("allocate %s event: %w", eventType, err)
	}
	nodeID := uuidToPG(nodeRunID)
	attempt := uuidToPG(attemptID)
	if _, err := q.AppendExecutionEvent(ctx, db.AppendExecutionEventParams{
		RunID: runID, Sequence: sequence, EventType: eventType, NodeRunID: nodeID, AttemptID: attempt, Payload: payload,
	}); err != nil {
		return fmt.Errorf("append %s event: %w", eventType, err)
	}
	return nil
}

func uuidToPG(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *value, Valid: true}
}
