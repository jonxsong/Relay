package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDeterministicMockIsStable(t *testing.T) {
	t.Parallel()
	executor := DeterministicMock{}
	input := json.RawMessage(`{"prompt":"hello"}`)
	first, err := executor.Execute(context.Background(), "llm", input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	second, err := executor.Execute(context.Background(), "llm", input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(first.Output) != string(second.Output) {
		t.Fatalf("mock output differs: %s != %s", first.Output, second.Output)
	}
}

func TestDeterministicMockRefusesUnconfiguredTools(t *testing.T) {
	t.Parallel()
	_, err := (DeterministicMock{}).Execute(context.Background(), "tool", json.RawMessage(`{}`))
	var executionError *ExecutionError
	if !errors.As(err, &executionError) || executionError.Code != "tool_not_configured" || executionError.Retryable {
		t.Fatalf("Execute() error = %#v", err)
	}
}

func TestDeterministicMockDelayHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := (DeterministicMock{Delay: time.Second}).Execute(ctx, "llm", json.RawMessage(`{}`))
	var executionError *ExecutionError
	if !errors.As(err, &executionError) || executionError.Code != "cancelled" {
		t.Fatalf("Execute() error = %v, want cancelled ExecutionError", err)
	}
}
