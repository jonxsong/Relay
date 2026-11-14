package worker

import (
	"context"
	"encoding/json"
	"testing"
)

type recordingExecutor struct{ calls []string }

func (r *recordingExecutor) Execute(_ context.Context, nodeType string, _ json.RawMessage) (ExecutionResult, error) {
	r.calls = append(r.calls, nodeType)
	return ExecutionResult{Output: json.RawMessage(`{}`)}, nil
}

func TestRouterSeparatesProviderAndLocalExecution(t *testing.T) {
	llm, local := &recordingExecutor{}, &recordingExecutor{}
	router := Router{LLM: llm, Local: local}
	if _, err := router.Execute(context.Background(), "llm", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Execute(context.Background(), "transform", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if len(llm.calls) != 1 || llm.calls[0] != "llm" || len(local.calls) != 1 || local.calls[0] != "transform" {
		t.Fatalf("unexpected routes: llm=%v local=%v", llm.calls, local.calls)
	}
}
