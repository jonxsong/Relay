package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store/db"
)

func (s *Store) RecoverExpiredAttempts(ctx context.Context, batchSize int32, maxRetries int) (int, error) {
	if batchSize < 1 || batchSize > 1000 {
		return 0, errors.New("recovery batch size must be between 1 and 1000")
	}
	if maxRetries < 0 || maxRetries > 100 {
		return 0, errors.New("max retries must be between 0 and 100")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin recovery transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	attempts, err := q.LockExpiredAttempts(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("lock expired attempts: %w", err)
	}
	recovered := 0
	for _, attempt := range attempts {
		node, err := q.GetNodeRunByID(ctx, attempt.NodeRunID)
		if err != nil {
			return recovered, classifyReadError("get expired attempt node", err)
		}
		run, err := q.GetRunByID(ctx, node.RunID)
		if err != nil {
			return recovered, classifyReadError("get expired attempt run", err)
		}
		rows, err := q.ExpireAttempt(ctx, db.ExpireAttemptParams{ID: attempt.ID, FencingToken: attempt.FencingToken})
		if err != nil {
			return recovered, fmt.Errorf("expire attempt: %w", err)
		}
		if rows != 1 {
			continue
		}

		if run.Status == string(domain.RunCancelled) {
			if _, err := q.CancelRunningNode(ctx, node.ID); err != nil {
				return recovered, fmt.Errorf("cancel expired node: %w", err)
			}
			if err := appendEvent(ctx, q, node.RunID, "node.cancelled", &node.ID, &attempt.ID, json.RawMessage(`{"reason":"run_cancelled"}`)); err != nil {
				return recovered, err
			}
			recovered++
			continue
		}

		if int(attempt.AttemptNo) <= maxRetries {
			policy := domain.RetryPolicy{MaxRetries: maxRetries, BaseDelay: time.Second, MaxDelay: time.Minute, Jitter: 0.2}
			delay, err := policy.Delay(int(attempt.AttemptNo), rand.Float64())
			if err != nil {
				return recovered, fmt.Errorf("calculate recovery delay: %w", err)
			}
			rows, err := q.ScheduleNodeRetry(ctx, db.ScheduleNodeRetryParams{
				ID: node.ID, NextAttemptAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(delay), Valid: true},
			})
			if err != nil {
				return recovered, fmt.Errorf("schedule recovered attempt: %w", err)
			}
			if rows != 1 {
				continue
			}
			payload, _ := json.Marshal(map[string]any{"attempt_no": attempt.AttemptNo, "delay_ms": delay.Milliseconds(), "reason": "lease_expired"})
			if err := appendEvent(ctx, q, node.RunID, "node.retry_scheduled", &node.ID, &attempt.ID, payload); err != nil {
				return recovered, err
			}
		} else {
			code := "lease_expired"
			message := "worker lease expired and retry budget was exhausted"
			if _, err := q.CompleteNodeRunFailure(ctx, db.CompleteNodeRunFailureParams{ID: node.ID, ErrorCode: &code, ErrorMessage: message}); err != nil {
				return recovered, fmt.Errorf("fail expired node: %w", err)
			}
			if _, err := q.SkipUnstartedNodes(ctx, node.RunID); err != nil {
				return recovered, fmt.Errorf("skip nodes after recovery exhaustion: %w", err)
			}
			if _, err := q.CompleteRunFailure(ctx, db.CompleteRunFailureParams{ID: node.RunID, ErrorCode: &code, ErrorMessage: message}); err != nil {
				return recovered, fmt.Errorf("fail run after recovery exhaustion: %w", err)
			}
			payload, _ := json.Marshal(map[string]any{"attempt_no": attempt.AttemptNo, "reason": code})
			if err := appendEvent(ctx, q, node.RunID, "node.failed", &node.ID, &attempt.ID, payload); err != nil {
				return recovered, err
			}
			if err := appendEvent(ctx, q, node.RunID, "run.failed", nil, nil, payload); err != nil {
				return recovered, err
			}
		}
		recovered++
	}
	if err := tx.Commit(ctx); err != nil {
		return recovered, classifyWriteError("commit recovery batch", err)
	}
	return recovered, nil
}
