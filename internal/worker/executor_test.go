package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
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
	if string(first) != string(second) {
		t.Fatalf("mock output differs: %s != %s", first, second)
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
