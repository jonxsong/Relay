package worker

import (
	"context"
	"encoding/json"
)

type Router struct {
	LLM   Executor
	Local Executor
	Tool  Executor
}

func (r Router) Execute(ctx context.Context, nodeType string, input json.RawMessage) (ExecutionResult, error) {
	switch nodeType {
	case "llm":
		return r.LLM.Execute(ctx, nodeType, input)
	case "tool":
		if r.Tool != nil {
			return r.Tool.Execute(ctx, nodeType, input)
		}
		return r.Local.Execute(ctx, nodeType, input)
	default:
		return r.Local.Execute(ctx, nodeType, input)
	}
}
