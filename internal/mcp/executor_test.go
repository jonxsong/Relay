package relaymcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jonxsong/Relay/internal/worker"
)

type fakeConnector struct {
	session *fakeSession
	calls   int
}

func (f *fakeConnector) Connect(context.Context, Connection) (Session, error) {
	f.calls++
	return f.session, nil
}

type fakeSession struct {
	tools      []*sdk.Tool
	result     *sdk.CallToolResult
	calledWith *sdk.CallToolParams
}

func (f *fakeSession) ListTools(context.Context, *sdk.ListToolsParams) (*sdk.ListToolsResult, error) {
	return &sdk.ListToolsResult{Tools: f.tools}, nil
}

func (f *fakeSession) CallTool(_ context.Context, params *sdk.CallToolParams) (*sdk.CallToolResult, error) {
	f.calledWith = params
	return f.result, nil
}

func (*fakeSession) Close() error { return nil }

func testConnection(policy ToolPolicy) Connection {
	return Connection{
		Name: "crm", Endpoint: "https://mcp.example.com/mcp", Timeout: time.Second, MaxResultBytes: 4096,
		AllowedTools: map[string]ToolPolicy{"lookup": policy},
	}
}

func TestExecutorCallsAllowlistedToolWithValidatedSchemas(t *testing.T) {
	inputSchema := map[string]any{
		"type": "object", "required": []string{"id"},
		"properties":           map[string]any{"id": map[string]any{"type": "string"}},
		"additionalProperties": false,
	}
	if err := validateSchema(inputSchema, map[string]any{"id": "42"}); err != nil {
		t.Fatalf("test schema is invalid: %v", err)
	}
	session := &fakeSession{
		tools: []*sdk.Tool{{
			Name:        "lookup",
			InputSchema: inputSchema,
			OutputSchema: map[string]any{
				"type": "object", "required": []string{"name"},
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
		}},
		result: &sdk.CallToolResult{StructuredContent: map[string]any{"name": "Relay"}},
	}
	connector := &fakeConnector{session: session}
	executor, err := NewExecutor([]Connection{testConnection(ToolPolicy{})}, connector)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), "tool", json.RawMessage(`{"connection":"crm","tool":"lookup","arguments":{"id":"42"}}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if session.calledWith == nil || session.calledWith.Name != "lookup" || !json.Valid(result.Output) {
		t.Fatalf("unexpected call or output: call=%#v output=%s", session.calledWith, result.Output)
	}
}

func TestExecutorRequiresApprovalBeforeConnecting(t *testing.T) {
	connector := &fakeConnector{session: &fakeSession{}}
	executor, _ := NewExecutor([]Connection{testConnection(ToolPolicy{RequiresApproval: true})}, connector)
	_, err := executor.Execute(context.Background(), "tool", json.RawMessage(`{"connection":"crm","tool":"lookup","arguments":{}}`))
	var executionErr *worker.ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != "tool_approval_required" {
		t.Fatalf("unexpected error: %#v", err)
	}
	if connector.calls != 0 {
		t.Fatal("connector called before approval")
	}
}

func TestExecutorRejectsNonAllowlistedToolBeforeConnecting(t *testing.T) {
	connector := &fakeConnector{session: &fakeSession{}}
	executor, _ := NewExecutor([]Connection{testConnection(ToolPolicy{})}, connector)
	_, err := executor.Execute(context.Background(), "tool", json.RawMessage(`{"connection":"crm","tool":"delete_everything","arguments":{}}`))
	var executionErr *worker.ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != "tool_denied" {
		t.Fatalf("unexpected error: %#v", err)
	}
	if connector.calls != 0 {
		t.Fatal("connector called for denied tool")
	}
}

func TestExecutorRejectsArgumentsThatViolateSchema(t *testing.T) {
	session := &fakeSession{tools: []*sdk.Tool{{
		Name: "lookup", InputSchema: map[string]any{
			"type": "object", "required": []string{"id"},
			"properties": map[string]any{"id": map[string]any{"type": "string"}},
		},
	}}}
	executor, _ := NewExecutor([]Connection{testConnection(ToolPolicy{})}, &fakeConnector{session: session})
	_, err := executor.Execute(context.Background(), "tool", json.RawMessage(`{"connection":"crm","tool":"lookup","arguments":{"id":7}}`))
	var executionErr *worker.ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != "invalid_tool_input" {
		t.Fatalf("unexpected error: %#v", err)
	}
	if session.calledWith != nil {
		t.Fatal("invalid arguments reached server")
	}
}
