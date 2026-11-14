package store

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store/db"
)

type OutboxPublisher func(context.Context, db.OutboxEvent) error

func (s *Store) DispatchOutbox(ctx context.Context, batchSize int32, publish OutboxPublisher) (int, error) {
	if batchSize < 1 || batchSize > 1000 {
		return 0, errors.New("outbox batch size must be between 1 and 1000")
	}
	if publish == nil {
		return 0, errors.New("outbox publisher is required")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin outbox transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	events, err := q.LockPendingOutboxEvents(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("lock outbox events: %w", err)
	}
	published := 0
	for _, event := range events {
		publishContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		publishErr := publish(publishContext, event)
		cancel()
		if publishErr == nil {
			rows, err := q.MarkOutboxPublished(ctx, event.ID)
			if err != nil {
				return published, fmt.Errorf("mark outbox event published: %w", err)
			}
			if rows != 1 {
				return published, errors.New("mark outbox event published: stale row")
			}
			published++
			continue
		}

		retryNumber := event.PublishAttempts + 1
		if retryNumber > 100 {
			retryNumber = 100
		}
		delay, delayErr := (domain.RetryPolicy{
			MaxRetries: 100, BaseDelay: time.Second, MaxDelay: time.Minute, Jitter: 0.2,
		}).Delay(int(retryNumber), rand.Float64())
		if delayErr != nil {
			return published, fmt.Errorf("calculate outbox retry delay: %w", delayErr)
		}
		rows, err := q.RecordOutboxFailure(ctx, db.RecordOutboxFailureParams{
			ID: event.ID, LastError: publishErr.Error(),
			AvailableAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(delay), Valid: true},
		})
		if err != nil {
			return published, fmt.Errorf("record outbox failure: %w", err)
		}
		if rows != 1 {
			return published, errors.New("record outbox failure: stale row")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return published, fmt.Errorf("commit outbox batch: %w", err)
	}
	return published, nil
}
