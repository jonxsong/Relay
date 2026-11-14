package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/worker"
)

const defaultInstructions = "Return a concise, accurate response to the supplied prompt."

type responseCreator interface {
	New(context.Context, responses.ResponseNewParams, ...option.RequestOption) (*responses.Response, error)
}

type OpenAI struct {
	responses       responseCreator
	defaultModel    string
	maxOutputTokens int64
	timeout         time.Duration
}

type llmInput struct {
	Prompt       string `json:"prompt"`
	Instructions string `json:"instructions,omitempty"`
	Model        string `json:"model,omitempty"`
}

func NewOpenAI(apiKey, defaultModel string, maxOutputTokens int64, timeout time.Duration) (*OpenAI, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("OpenAI API key is required")
	}
	if strings.TrimSpace(defaultModel) == "" {
		return nil, errors.New("default OpenAI model is required")
	}
	if maxOutputTokens < 1 {
		return nil, errors.New("max output tokens must be positive")
	}
	if timeout <= 0 {
		return nil, errors.New("OpenAI timeout must be positive")
	}
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &OpenAI{
		responses:       &client.Responses,
		defaultModel:    defaultModel,
		maxOutputTokens: maxOutputTokens,
		timeout:         timeout,
	}, nil
}

func (o *OpenAI) Execute(ctx context.Context, nodeType string, input json.RawMessage) (worker.ExecutionResult, error) {
	if nodeType != "llm" {
		return worker.ExecutionResult{}, &worker.ExecutionError{
			Code: "unsupported_node_type",
			Err:  fmt.Errorf("OpenAI executor cannot run node type %q", nodeType),
		}
	}
	var request llmInput
	if err := json.Unmarshal(input, &request); err != nil {
		return worker.ExecutionResult{}, &worker.ExecutionError{Code: "invalid_input", Err: errors.New("decode LLM input")}
	}
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Prompt == "" {
		return worker.ExecutionResult{}, &worker.ExecutionError{Code: "invalid_input", Err: errors.New("LLM input prompt is required")}
	}
	request.Instructions = strings.TrimSpace(request.Instructions)
	if request.Instructions == "" {
		request.Instructions = defaultInstructions
	}
	request.Model = strings.TrimSpace(request.Model)
	if request.Model == "" {
		request.Model = o.defaultModel
	}

	requestContext, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	response, err := o.responses.New(requestContext, responses.ResponseNewParams{
		Input:           responses.ResponseNewParamsInputUnion{OfString: openai.String(request.Prompt)},
		Instructions:    openai.String(request.Instructions),
		Model:           request.Model,
		MaxOutputTokens: openai.Int(o.maxOutputTokens),
		Store:           openai.Bool(false),
	})
	if err != nil {
		return worker.ExecutionResult{}, classifyError(err)
	}
	if response == nil {
		return worker.ExecutionResult{}, &worker.ExecutionError{Code: "provider_protocol_error", Retryable: true, Err: errors.New("OpenAI returned an empty response")}
	}
	if response.Status != responses.ResponseStatusCompleted {
		return worker.ExecutionResult{}, &worker.ExecutionError{
			Code:      "provider_incomplete",
			Retryable: response.Status == responses.ResponseStatusInProgress || response.Status == responses.ResponseStatusQueued,
			Err:       fmt.Errorf("OpenAI response status %q", response.Status),
		}
	}
	output, err := json.Marshal(map[string]any{
		"text":        response.OutputText(),
		"response_id": response.ID,
		"model":       response.Model,
	})
	if err != nil {
		return worker.ExecutionResult{}, &worker.ExecutionError{Code: "provider_protocol_error", Err: errors.New("encode OpenAI response")}
	}
	inputTokens := response.Usage.InputTokens
	outputTokens := response.Usage.OutputTokens
	return worker.ExecutionResult{
		Output: output,
		Usage: &domain.Usage{
			Provider:     "openai",
			Model:        string(response.Model),
			InputTokens:  &inputTokens,
			OutputTokens: &outputTokens,
		},
	}, nil
}

func classifyError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &worker.ExecutionError{Code: "provider_timeout", Retryable: true, Err: err}
	}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		status := apiErr.StatusCode
		return &worker.ExecutionError{
			Code:      fmt.Sprintf("provider_http_%d", status),
			Retryable: status == http.StatusRequestTimeout || status == http.StatusConflict || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError,
			Err:       apiErr,
		}
	}
	return &worker.ExecutionError{Code: "provider_transport_error", Retryable: true, Err: errors.New("OpenAI request failed")}
}
