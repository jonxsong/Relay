package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jonxsong/Relay/internal/store/db"
)

// ExpireOverdueRuns gives every elapsed run deadline a durable terminal state.
// Locking each run and fencing its active attempts makes late worker completions stale.
func (s *Store) ExpireOverdueRuns(ctx context.Context, batchSize int32) (int, error) {
	if batchSize < 1 || batchSize > 1000 {
		return 0, errors.New("deadline batch size must be between 1 and 1000")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin deadline transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	runs, err := q.LockOverdueRuns(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("lock overdue runs: %w", err)
	}
	for _, run := range runs {
		if _, err := q.FailAttemptsForRunDeadline(ctx, run.ID); err != nil {
			return 0, fmt.Errorf("fail overdue run attempts: %w", err)
		}
		nodes, err := q.FailNodesForRunDeadline(ctx, run.ID)
		if err != nil {
			return 0, fmt.Errorf("fail overdue run nodes: %w", err)
		}
		code := "run_deadline_exceeded"
		message := "run deadline elapsed before execution completed"
		updated, err := q.CompleteRunFailure(ctx, db.CompleteRunFailureParams{ID: run.ID, ErrorCode: &code, ErrorMessage: message})
		if err != nil {
			return 0, fmt.Errorf("fail overdue run: %w", err)
		}
		if updated != 1 {
			continue
		}
		payload, _ := json.Marshal(map[string]any{"error_code": code, "message": message})
		for _, node := range nodes {
			if err := appendEvent(ctx, q, run.ID, "node.failed", &node.ID, nil, payload); err != nil {
				return 0, err
			}
		}
		if err := appendEvent(ctx, q, run.ID, "run.failed", nil, nil, payload); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, classifyWriteError("commit deadline batch", err)
	}
	return len(runs), nil
}
