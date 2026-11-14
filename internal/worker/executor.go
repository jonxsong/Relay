package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jonxsong/Relay/internal/domain"
)

type Executor interface {
	Execute(context.Context, string, json.RawMessage) (ExecutionResult, error)
}

type ExecutionResult struct {
	Output json.RawMessage
	Usage  *domain.Usage
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

type DeterministicMock struct {
	Delay time.Duration
}

func (m DeterministicMock) Execute(ctx context.Context, nodeType string, input json.RawMessage) (ExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionResult{}, &ExecutionError{Code: "cancelled", Retryable: true, Err: err}
	}
	if m.Delay > 0 {
		timer := time.NewTimer(m.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ExecutionResult{}, &ExecutionError{Code: "cancelled", Retryable: true, Err: ctx.Err()}
		case <-timer.C:
		}
	}
	var value map[string]any
	if err := json.Unmarshal(input, &value); err != nil {
		return ExecutionResult{}, &ExecutionError{Code: "invalid_input", Err: fmt.Errorf("decode node input: %w", err)}
	}
	switch nodeType {
	case "transform":
		output, err := json.Marshal(map[string]any{"value": value, "provider": "mock"})
		return ExecutionResult{Output: output}, err
	case "join":
		output, err := json.Marshal(map[string]any{"joined": value, "provider": "mock"})
		return ExecutionResult{Output: output}, err
	case "llm":
		hash := sha256.Sum256(input)
		output, err := json.Marshal(map[string]any{
			"text":  "deterministic mock response " + hex.EncodeToString(hash[:6]),
			"model": "relay-mock-v1",
		})
		zero := int64(0)
		return ExecutionResult{Output: output, Usage: &domain.Usage{
			Provider: "mock", Model: "relay-mock-v1", InputTokens: &zero, OutputTokens: &zero, CostMicrousd: &zero,
		}}, err
	case "tool":
		return ExecutionResult{}, &ExecutionError{Code: "tool_not_configured", Err: errors.New("tool execution requires an allowlisted MCP connection")}
	default:
		return ExecutionResult{}, &ExecutionError{Code: "unknown_node_type", Err: fmt.Errorf("unknown node type %q", nodeType)}
	}
}
