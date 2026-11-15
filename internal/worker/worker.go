package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type AttemptRepository interface {
	ClaimAttempt(context.Context, uuid.UUID, string, time.Duration) (store.AttemptClaim, error)
	RenewLease(context.Context, store.AttemptClaim, time.Duration) error
	CompleteAttemptSuccess(context.Context, store.AttemptClaim, json.RawMessage, *domain.Usage) error
	CompleteAttemptFailure(context.Context, store.AttemptClaim, string, string, bool, int) error
}

type Worker struct {
	repository  AttemptRepository
	executor    Executor
	workerID    string
	lease       time.Duration
	maxRetries  int
	nodeTimeout time.Duration
}

func New(repository AttemptRepository, executor Executor, workerID string, lease time.Duration, maxRetries int) (*Worker, error) {
	if repository == nil || executor == nil {
		return nil, errors.New("worker repository and executor are required")
	}
	if workerID == "" {
		return nil, errors.New("worker id is required")
	}
	if lease < 15*time.Second || lease > 10*time.Minute {
		return nil, errors.New("worker lease must be between 15s and 10m")
	}
	if maxRetries < 0 || maxRetries > 100 {
		return nil, errors.New("max retries must be between 0 and 100")
	}
	return &Worker{repository: repository, executor: executor, workerID: workerID, lease: lease, maxRetries: maxRetries, nodeTimeout: 2 * time.Minute}, nil
}

func (w *Worker) Handle(ctx context.Context, attemptID uuid.UUID) (handleErr error) {
	ctx, span := otel.Tracer("github.com/jonxsong/Relay/worker").Start(ctx, "worker.attempt")
	span.SetAttributes(attribute.String("relay.attempt.id", attemptID.String()))
	defer func() {
		if handleErr != nil {
			span.RecordError(handleErr)
			span.SetStatus(codes.Error, "attempt failed")
		}
		span.End()
	}()
	claim, err := w.repository.ClaimAttempt(ctx, attemptID, w.workerID, w.lease)
	if err != nil {
		return err
	}
	span.SetAttributes(
		attribute.String("relay.run.id", claim.RunID.String()),
		attribute.String("relay.node.key", claim.NodeKey),
		attribute.String("relay.node.type", claim.NodeType),
		attribute.Int("relay.attempt.number", int(claim.AttemptNo)),
	)
	executionDeadline := time.Now().Add(w.nodeTimeout)
	deadlineCode := "node_deadline_exceeded"
	if claim.NodeTimeout > 0 {
		executionDeadline = time.Now().Add(claim.NodeTimeout)
	}
	if claim.RunDeadline != nil && claim.RunDeadline.Before(executionDeadline) {
		executionDeadline = *claim.RunDeadline
		deadlineCode = "run_deadline_exceeded"
	}
	executionContext, cancelExecution := context.WithDeadline(ctx, executionDeadline)
	defer cancelExecution()
	leaseErrors := make(chan error, 1)
	stopRenewal := make(chan struct{})
	renewalDone := make(chan struct{})
	go func() {
		defer close(renewalDone)
		w.renewLease(executionContext, claim, stopRenewal, leaseErrors, cancelExecution)
	}()

	result, executionErr := w.executor.Execute(executionContext, claim.NodeType, claim.Input)
	close(stopRenewal)
	// A renewal and completion both update the attempt row. Wait for any renewal
	// already in flight before opening the serializable completion transaction;
	// otherwise they can deterministically collide at the lease interval boundary.
	<-renewalDone
	select {
	case leaseErr := <-leaseErrors:
		if leaseErr != nil {
			return leaseErr
		}
	default:
	}
	if executionErr == nil {
		return w.repository.CompleteAttemptSuccess(ctx, claim, result.Output, result.Usage)
	}
	code := "execution_failed"
	retryable := false
	var typedError *ExecutionError
	if errors.Is(executionContext.Err(), context.DeadlineExceeded) {
		code = deadlineCode
		executionErr = fmt.Errorf("%s: execution deadline exceeded", deadlineCode)
	} else if errors.As(executionErr, &typedError) {
		code = typedError.Code
		retryable = typedError.Retryable
	}
	if err := w.repository.CompleteAttemptFailure(ctx, claim, code, executionErr.Error(), retryable, w.maxRetries); err != nil {
		return fmt.Errorf("record node failure: %w", err)
	}
	return nil
}

func (w *Worker) renewLease(ctx context.Context, claim store.AttemptClaim, stop <-chan struct{}, failures chan<- error, cancel context.CancelFunc) {
	interval := w.lease / 3
	if interval > 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			if err := w.repository.RenewLease(ctx, claim, w.lease); err != nil {
				select {
				case failures <- err:
				default:
				}
				cancel()
				return
			}
		}
	}
}
