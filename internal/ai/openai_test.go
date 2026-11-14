package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/jonxsong/Relay/internal/worker"
)

type fakeResponses struct {
	response *responses.Response
	err      error
	params   responses.ResponseNewParams
}

func (f *fakeResponses) New(_ context.Context, params responses.ResponseNewParams, _ ...option.RequestOption) (*responses.Response, error) {
	f.params = params
	return f.response, f.err
}

func TestOpenAIExecuteRecordsSafeOutputAndUsage(t *testing.T) {
	fake := &fakeResponses{response: &responses.Response{
		ID: "resp_123", Status: responses.ResponseStatusCompleted, Model: shared.ResponsesModel("gpt-test"),
		Output: []responses.ResponseOutputItemUnion{{
			Type: "message", Role: "assistant", Status: "completed",
			Content: []responses.ResponseOutputMessageContentUnion{{Type: "output_text", Text: "hello"}},
		}},
		Usage: responses.ResponseUsage{InputTokens: 12, OutputTokens: 4},
	}}
	executor := &OpenAI{responses: fake, defaultModel: "gpt-default", maxOutputTokens: 128, timeout: time.Second}
	result, err := executor.Execute(context.Background(), "llm", json.RawMessage(`{"prompt":"Say hello"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Usage == nil || result.Usage.Provider != "openai" || *result.Usage.InputTokens != 12 || *result.Usage.OutputTokens != 4 {
		t.Fatalf("unexpected usage: %#v", result.Usage)
	}
	if got := fake.params.Input.OfString.Value; got != "Say hello" {
		t.Fatalf("prompt = %q", got)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if output["text"] != "hello" || output["response_id"] != "resp_123" {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestOpenAIExecuteRejectsMissingPrompt(t *testing.T) {
	executor := &OpenAI{responses: &fakeResponses{}, defaultModel: "gpt-default", maxOutputTokens: 128, timeout: time.Second}
	_, err := executor.Execute(context.Background(), "llm", json.RawMessage(`{}`))
	var executionErr *worker.ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != "invalid_input" || executionErr.Retryable {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestOpenAIClassifiesRetryableHTTPStatusWithoutLeakingBody(t *testing.T) {
	err := classifyError(&openai.Error{StatusCode: 429, Message: "secret provider detail"})
	var executionErr *worker.ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != "provider_http_429" || !executionErr.Retryable {
		t.Fatalf("unexpected error: %#v", err)
	}
	if got := executionErr.Error(); got != "OpenAI API error: 429 Too Many Requests" {
		t.Fatalf("unsafe or unexpected error: %q", got)
	}
}
