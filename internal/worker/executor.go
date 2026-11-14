package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

type Executor interface {
	Execute(context.Context, string, json.RawMessage) (json.RawMessage, error)
}

type ExecutionError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *ExecutionError) Error() string {
	return e.Err.Error()
}

func (e *ExecutionError) Unwrap() error {
	return e.Err
}

type DeterministicMock struct{}

func (DeterministicMock) Execute(ctx context.Context, nodeType string, input json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, &ExecutionError{Code: "cancelled", Retryable: true, Err: err}
	}
	var value map[string]any
	if err := json.Unmarshal(input, &value); err != nil {
		return nil, &ExecutionError{Code: "invalid_input", Err: fmt.Errorf("decode node input: %w", err)}
	}
	switch nodeType {
	case "transform":
		return json.Marshal(map[string]any{"value": value, "provider": "mock"})
	case "join":
		return json.Marshal(map[string]any{"joined": value, "provider": "mock"})
	case "llm":
		hash := sha256.Sum256(input)
		return json.Marshal(map[string]any{
			"text":  "deterministic mock response " + hex.EncodeToString(hash[:6]),
			"model": "relay-mock-v1",
		})
	case "tool":
		return nil, &ExecutionError{Code: "tool_not_configured", Err: errors.New("tool execution requires an allowlisted MCP connection")}
	default:
		return nil, &ExecutionError{Code: "unknown_node_type", Err: fmt.Errorf("unknown node type %q", nodeType)}
	}
}
