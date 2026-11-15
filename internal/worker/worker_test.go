package worker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store"
)

type fakeAttemptRepository struct {
	claim             store.AttemptClaim
	successes         int
	failures          int
	failureCode       string
	retryable         bool
	renewError        error
	renewStarted      chan struct{}
	releaseRenew      chan struct{}
	completionStarted chan struct{}
	renewOnce         sync.Once
	renewActive       atomic.Bool
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
	if f.renewStarted != nil {
		f.renewActive.Store(true)
		f.renewOnce.Do(func() { close(f.renewStarted) })
		<-f.releaseRenew
		f.renewActive.Store(false)
	}
	return f.renewError
}
func (f *fakeAttemptRepository) CompleteAttemptSuccess(_ context.Context, _ store.AttemptClaim, output json.RawMessage, _ *domain.Usage) error {
	if f.completionStarted != nil {
		close(f.completionStarted)
	}
	if f.renewActive.Load() {
		return errors.New("completion overlapped lease renewal")
	}
	if !json.Valid(output) {
		return errors.New("invalid output")
	}
	f.successes++
	return nil
}

type waitForRenewalExecutor struct {
	renewStarted <-chan struct{}
}

func (executor waitForRenewalExecutor) Execute(context.Context, string, json.RawMessage) (ExecutionResult, error) {
	<-executor.renewStarted
	return ExecutionResult{Output: json.RawMessage(`{"ok":true}`)}, nil
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

func TestWorkerWaitsForInFlightLeaseRenewalBeforeCompletion(t *testing.T) {
	t.Parallel()
	renewStarted := make(chan struct{})
	releaseRenew := make(chan struct{})
	completionStarted := make(chan struct{})
	repository := &fakeAttemptRepository{
		claim:             store.AttemptClaim{NodeType: "llm", Input: json.RawMessage(`{}`)},
		renewStarted:      renewStarted,
		releaseRenew:      releaseRenew,
		completionStarted: completionStarted,
	}
	processor, err := New(repository, waitForRenewalExecutor{renewStarted: renewStarted}, "worker-test", 15*time.Second, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	processor.lease = 15 * time.Millisecond

	handleResult := make(chan error, 1)
	go func() { handleResult <- processor.Handle(context.Background(), uuid.New()) }()
	<-renewStarted
	select {
	case <-completionStarted:
		t.Fatal("completion started while lease renewal was still in flight")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseRenew)
	if err := <-handleResult; err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if repository.successes != 1 {
		t.Fatalf("successes=%d, want 1", repository.successes)
	}
}
