package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store"
)

type fakeAttemptRepository struct {
	claim       store.AttemptClaim
	successes   int
	failures    int
	failureCode string
	retryable   bool
	renewError  error
}

type blockingExecutor struct{}

func (blockingExecutor) Execute(ctx context.Context, _ string, _ json.RawMessage) (ExecutionResult, error) {
	<-ctx.Done()
	return ExecutionResult{}, ctx.Err()
}

func (f *fakeAttemptRepository) ClaimAttempt(context.Context, uuid.UUID, string, time.Duration) (store.AttemptClaim, error) {
	return f.claim, nil
}
func (f *fakeAttemptRepository) RenewLease(context.Context, store.AttemptClaim, time.Duration) error {
	return f.renewError
}
func (f *fakeAttemptRepository) CompleteAttemptSuccess(_ context.Context, _ store.AttemptClaim, output json.RawMessage, _ *domain.Usage) error {
	if !json.Valid(output) {
		return errors.New("invalid output")
	}
	f.successes++
	return nil
}
func (f *fakeAttemptRepository) CompleteAttemptFailure(_ context.Context, _ store.AttemptClaim, code, _ string, retryable bool, _ int) error {
	f.failures++
	f.failureCode = code
	f.retryable = retryable
	return nil
}

func TestWorkerCompletesSuccessfulExecution(t *testing.T) {
	t.Parallel()
	repository := &fakeAttemptRepository{claim: store.AttemptClaim{NodeType: "llm", Input: json.RawMessage(`{"prompt":"hello"}`)}}
	processor, err := New(repository, DeterministicMock{}, "worker-test", 15*time.Second, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := processor.Handle(context.Background(), uuid.New()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if repository.successes != 1 || repository.failures != 0 {
		t.Fatalf("successes=%d failures=%d", repository.successes, repository.failures)
	}
}

func TestWorkerRecordsPermanentToolFailure(t *testing.T) {
	t.Parallel()
	repository := &fakeAttemptRepository{claim: store.AttemptClaim{NodeType: "tool", Input: json.RawMessage(`{}`)}}
	processor, err := New(repository, DeterministicMock{}, "worker-test", 15*time.Second, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := processor.Handle(context.Background(), uuid.New()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if repository.failures != 1 || repository.failureCode != "tool_not_configured" || repository.retryable {
		t.Fatalf("failures=%d code=%q retryable=%v", repository.failures, repository.failureCode, repository.retryable)
	}
}

func TestWorkerEnforcesConfiguredNodeDeadline(t *testing.T) {
	t.Parallel()
	repository := &fakeAttemptRepository{claim: store.AttemptClaim{NodeType: "llm", Input: json.RawMessage(`{}`), NodeTimeout: 5 * time.Millisecond}}
	processor, err := New(repository, blockingExecutor{}, "worker-test", 15*time.Second, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := processor.Handle(context.Background(), uuid.New()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if repository.failures != 1 || repository.failureCode != "node_deadline_exceeded" || repository.retryable {
		t.Fatalf("failures=%d code=%q retryable=%v", repository.failures, repository.failureCode, repository.retryable)
	}
}

func TestWorkerEnforcesEarlierRunDeadline(t *testing.T) {
	t.Parallel()
	deadline := time.Now().Add(5 * time.Millisecond)
	repository := &fakeAttemptRepository{claim: store.AttemptClaim{NodeType: "llm", Input: json.RawMessage(`{}`), NodeTimeout: time.Minute, RunDeadline: &deadline}}
	processor, err := New(repository, blockingExecutor{}, "worker-test", 15*time.Second, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := processor.Handle(context.Background(), uuid.New()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if repository.failures != 1 || repository.failureCode != "run_deadline_exceeded" || repository.retryable {
		t.Fatalf("failures=%d code=%q retryable=%v", repository.failures, repository.failureCode, repository.retryable)
	}
}

func TestWorkerCancelsExecutionWhenLeaseBecomesInvalid(t *testing.T) {
	t.Parallel()
	repository := &fakeAttemptRepository{
		claim:      store.AttemptClaim{NodeType: "llm", Input: json.RawMessage(`{}`)},
		renewError: store.ErrRunCancelled,
	}
	processor, err := New(repository, blockingExecutor{}, "worker-test", 15*time.Second, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	processor.lease = 15 * time.Millisecond
	if err := processor.Handle(context.Background(), uuid.New()); !errors.Is(err, store.ErrRunCancelled) {
		t.Fatalf("Handle() error = %v, want ErrRunCancelled", err)
	}
	if repository.successes != 0 || repository.failures != 0 {
		t.Fatalf("stale worker recorded completion: successes=%d failures=%d", repository.successes, repository.failures)
	}
}
