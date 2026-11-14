package relaymcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jonxsong/Relay/internal/schema"
	"github.com/jonxsong/Relay/internal/worker"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxToolPages = 20

type Executor struct {
	connections map[string]Connection
	connector   Connector
}

type toolInput struct {
	Connection         string          `json:"connection"`
	Tool               string          `json:"tool"`
	Arguments          json.RawMessage `json:"arguments,omitempty"`
	SideEffectApproved bool            `json:"side_effect_approved,omitempty"`
}

func NewExecutor(connections []Connection, connector Connector) (*Executor, error) {
	if len(connections) == 0 {
		return nil, errors.New("at least one MCP connection is required")
	}
	if connector == nil {
		connector = SDKConnector{}
	}
	byName := make(map[string]Connection, len(connections))
	for _, connection := range connections {
		if _, exists := byName[connection.Name]; exists {
			return nil, fmt.Errorf("duplicate MCP connection %q", connection.Name)
		}
		byName[connection.Name] = connection
	}
	return &Executor{connections: byName, connector: connector}, nil
}

func (e *Executor) Execute(ctx context.Context, nodeType string, input json.RawMessage) (worker.ExecutionResult, error) {
	if nodeType != "tool" {
		return worker.ExecutionResult{}, executionError("unsupported_node_type", false, "MCP executor only accepts tool nodes")
	}
	request, connection, policy, err := e.validateRequest(input)
	if err != nil {
		return worker.ExecutionResult{}, err
	}
	if policy.RequiresApproval && !request.SideEffectApproved {
		return worker.ExecutionResult{}, executionError("tool_approval_required", false, "side-effecting tool requires explicit approval")
	}

	requestContext, cancel := context.WithTimeout(ctx, connection.Timeout)
	defer cancel()
	session, err := e.connector.Connect(requestContext, connection)
	if err != nil {
		return worker.ExecutionResult{}, executionError("tool_connection_failed", true, "MCP connection failed")
	}
	defer session.Close()

	tool, err := findTool(requestContext, session, request.Tool)
	if err != nil {
		return worker.ExecutionResult{}, err
	}
	var arguments any = map[string]any{}
	if len(request.Arguments) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(request.Arguments))
		decoder.UseNumber()
		if err := decoder.Decode(&arguments); err != nil {
			return worker.ExecutionResult{}, executionError("invalid_tool_input", false, "tool arguments must be valid JSON")
		}
		if _, ok := arguments.(map[string]any); !ok {
			return worker.ExecutionResult{}, executionError("invalid_tool_input", false, "tool arguments must be a JSON object")
		}
	}
	if err := schema.Validate(tool.InputSchema, arguments); err != nil {
		return worker.ExecutionResult{}, executionError("invalid_tool_input", false, "tool arguments do not match the declared schema")
	}
	result, err := session.CallTool(requestContext, &sdk.CallToolParams{Name: request.Tool, Arguments: arguments})
	if err != nil {
		return worker.ExecutionResult{}, executionError("tool_call_failed", true, "MCP tool call failed")
	}
	if result == nil {
		return worker.ExecutionResult{}, executionError("tool_protocol_error", true, "MCP server returned an empty result")
	}
	if result.IsError {
		return worker.ExecutionResult{}, executionError("tool_returned_error", false, "MCP tool returned an error")
	}
	if tool.OutputSchema != nil {
		if result.StructuredContent == nil {
			return worker.ExecutionResult{}, executionError("invalid_tool_output", false, "tool omitted required structured output")
		}
		if err := schema.Validate(tool.OutputSchema, result.StructuredContent); err != nil {
			return worker.ExecutionResult{}, executionError("invalid_tool_output", false, "tool output does not match the declared schema")
		}
	}
	output, err := marshalBoundedResult(result, connection.MaxResultBytes)
	if err != nil {
		return worker.ExecutionResult{}, err
	}
	return worker.ExecutionResult{Output: output}, nil
}

func (e *Executor) validateRequest(input json.RawMessage) (toolInput, Connection, ToolPolicy, error) {
	var request toolInput
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, Connection{}, ToolPolicy{}, executionError("invalid_tool_input", false, "decode tool input")
	}
	request.Connection = strings.TrimSpace(request.Connection)
	request.Tool = strings.TrimSpace(request.Tool)
	connection, exists := e.connections[request.Connection]
	if !exists {
		return request, Connection{}, ToolPolicy{}, executionError("tool_connection_denied", false, "MCP connection is not allowlisted")
	}
	policy, exists := connection.AllowedTools[request.Tool]
	if !exists {
		return request, connection, ToolPolicy{}, executionError("tool_denied", false, "MCP tool is not allowlisted")
	}
	return request, connection, policy, nil
}

func findTool(ctx context.Context, session Session, name string) (*sdk.Tool, error) {
	var cursor string
	for page := 0; page < maxToolPages; page++ {
		params := &sdk.ListToolsParams{}
		params.Cursor = cursor
		result, err := session.ListTools(ctx, params)
		if err != nil {
			return nil, executionError("tool_discovery_failed", true, "MCP tool discovery failed")
		}
		for _, tool := range result.Tools {
			if tool != nil && tool.Name == name {
				return tool, nil
			}
		}
		if result.NextCursor == "" {
			return nil, executionError("tool_unavailable", false, "allowlisted MCP tool is not advertised by the server")
		}
		cursor = result.NextCursor
	}
	return nil, executionError("tool_discovery_failed", false, "MCP tool list exceeded the pagination limit")
}

func marshalBoundedResult(result *sdk.CallToolResult, maximum int64) (json.RawMessage, error) {
	var buffer bytes.Buffer
	limited := &limitedWriter{writer: &buffer, remaining: maximum + 1}
	encoder := json.NewEncoder(limited)
	encoder.SetEscapeHTML(true)
	value := map[string]any{"content": result.Content, "structured_content": result.StructuredContent}
	if err := encoder.Encode(value); err != nil && !errors.Is(err, errResultTooLarge) {
		return nil, executionError("tool_protocol_error", false, "encode MCP tool result")
	}
	if limited.exceeded || int64(buffer.Len()) > maximum {
		return nil, executionError("tool_output_too_large", false, "MCP tool output exceeded the configured limit")
	}
	return bytes.TrimSpace(buffer.Bytes()), nil
}

var errResultTooLarge = errors.New("result too large")

type limitedWriter struct {
	writer    io.Writer
	remaining int64
	exceeded  bool
}

func (w *limitedWriter) Write(value []byte) (int, error) {
	if int64(len(value)) > w.remaining {
		value = value[:w.remaining]
		w.exceeded = true
	}
	written, err := w.writer.Write(value)
	w.remaining -= int64(written)
	if w.exceeded && err == nil {
		return written, errResultTooLarge
	}
	return written, err
}

func executionError(code string, retryable bool, safeMessage string) error {
	return &worker.ExecutionError{Code: code, Retryable: retryable, Err: errors.New(safeMessage)}
}
