package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultOpenAIProviderName = "openai-compatible"

type OpenAICompatibleConfig struct {
	Name    string
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

func (c OpenAICompatibleConfig) Redacted() map[string]interface{} {
	return map[string]interface{}{
		"name":               c.providerName(),
		"base_url":           c.BaseURL,
		"model":              c.Model,
		"timeout":            c.Timeout.String(),
		"api_key_configured": c.APIKey != "",
	}
}

func (c OpenAICompatibleConfig) providerName() string {
	if c.Name != "" {
		return c.Name
	}
	return defaultOpenAIProviderName
}

type OpenAICompatibleProvider struct {
	config OpenAICompatibleConfig
	client *http.Client
}

type OpenAIProviderOption func(*OpenAICompatibleProvider)

func WithHTTPClient(client *http.Client) OpenAIProviderOption {
	return func(p *OpenAICompatibleProvider) {
		if client != nil {
			p.client = client
		}
	}
}

func NewOpenAICompatibleProvider(config OpenAICompatibleConfig, opts ...OpenAIProviderOption) (*OpenAICompatibleProvider, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, newGatewayError(ErrProviderConfig, "base URL is required", nil)
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, newGatewayError(ErrProviderConfig, "model is required", nil)
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	provider := &OpenAICompatibleProvider{
		config: config,
		client: &http.Client{Timeout: timeout},
	}
	for _, opt := range opts {
		opt(provider)
	}
	// Never follow a provider redirect or silently retry a POST elsewhere.
	client := *provider.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	provider.client = &client
	return provider, nil
}

func (p *OpenAICompatibleProvider) Definition() Definition { return textDefinition() }

func (p *OpenAICompatibleProvider) Name() string {
	return p.config.providerName()
}

func (p *OpenAICompatibleProvider) ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, classifyContextError(err)
	}
	if err := request.validate(); err != nil {
		return nil, err
	}
	if request.Model != "" && request.Model != p.config.Model {
		return nil, newGatewayError(ErrInvalidRequest, "per-call model override is not supported", nil)
	}
	if strings.TrimSpace(p.config.APIKey) == "" {
		return nil, newGatewayError(ErrProviderConfig, "API key is required", nil)
	}
	model := request.Model
	if model == "" {
		model = p.config.Model
	}
	payload := openAIChatRequest{
		Model:       model,
		Messages:    request.Messages,
		Temperature: request.Temperature,
		MaxTokens:   request.MaxTokens,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, newGatewayError(ErrInvalidRequest, "marshal chat request", err)
	}

	endpoint := strings.TrimRight(p.config.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, newGatewayError(ErrProviderRequest, "create provider request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, classifyContextError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newProviderHTTPError(ErrProviderResponse, resp.StatusCode, "provider returned non-success status [REDACTED]", nil)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, MaxChatResponseBytes+1))
	if err != nil {
		return nil, classifyContextError(err)
	}
	if len(respBody) > MaxChatResponseBytes {
		return nil, newGatewayError(ErrProviderResponse, "provider response exceeds limit", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, classifyContextError(err)
	}

	var parsed openAIChatResponse
	if err := decodeTextResponse(respBody, &parsed); err != nil {
		return nil, newGatewayError(ErrProviderResponse, "decode provider response", err)
	}
	if len(parsed.Choices) != 1 {
		return nil, newGatewayError(ErrProviderResponse, "provider response has no choices", nil)
	}
	choice := parsed.Choices[0]
	if choice.Message.Role != "assistant" || forbiddenResponseValue(choice.Message.ToolCalls) || forbiddenResponseValue(choice.Message.FunctionCall) || forbiddenResponseValue(choice.Message.Audio) || forbiddenResponseValue(choice.Message.Refusal) || forbiddenResponseValue(choice.Message.Annotations) || (choice.FinishReason != "stop" && choice.FinishReason != "length") {
		return nil, newGatewayError(ErrProviderResponse, "unsupported AI response modality or finish reason", nil)
	}
	content := choice.Message.Content
	if content == "" {
		return nil, newGatewayError(ErrProviderResponse, "provider response content is empty", nil)
	}
	result := &ChatResponse{
		Provider:     p.Name(),
		Model:        parsed.Model,
		Content:      content,
		FinishReason: parsed.Choices[0].FinishReason,
		Usage: TokenUsage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
			TotalTokens:      parsed.Usage.TotalTokens,
		},
	}
	if err := validateTextResponse(result); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, classifyContextError(err)
	}
	return result, nil
}

func classifyContextError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return newGatewayError(ErrContextCancelled, "context cancelled", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newGatewayError(ErrContextDeadline, "context deadline exceeded", err)
	}
	return newGatewayError(ErrProviderRequest, "call provider", err)
}

type openAIChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type openAIChatResponse struct {
	ID                string  `json:"id"`
	Object            string  `json:"object"`
	Created           int64   `json:"created"`
	SystemFingerprint *string `json:"system_fingerprint"`
	ServiceTier       *string `json:"service_tier"`
	Model             string  `json:"model"`
	Choices           []struct {
		Index    int             `json:"index"`
		Logprobs json.RawMessage `json:"logprobs"`
		Message  struct {
			Role         string          `json:"role"`
			Content      string          `json:"content"`
			ToolCalls    json.RawMessage `json:"tool_calls"`
			FunctionCall json.RawMessage `json:"function_call"`
			Audio        json.RawMessage `json:"audio"`
			Refusal      json.RawMessage `json:"refusal"`
			Annotations  json.RawMessage `json:"annotations"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens      int             `json:"prompt_tokens"`
		CompletionTokens  int             `json:"completion_tokens"`
		TotalTokens       int             `json:"total_tokens"`
		PromptDetails     json.RawMessage `json:"prompt_tokens_details"`
		CompletionDetails json.RawMessage `json:"completion_tokens_details"`
	} `json:"usage"`
}

func (p *OpenAICompatibleProvider) String() string {
	return fmt.Sprintf("%s %s", p.Name(), p.config.Model)
}

func forbiddenResponseValue(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null" && value != "[]"
}

func decodeTextResponse(raw []byte, out *openAIChatResponse) error {
	// Wrap the response to reuse duplicate/Unicode/depth checks while permitting
	// the protocol's nullable response metadata, unlike required request fields.
	wrapped := append([]byte(`{"value":`), raw...)
	wrapped = append(wrapped, '}')
	if len(wrapped) > MaxChatResponseBytes || identityport.ValidateClosedHTTPObject(wrapped, []string{"value"}, MaxChatResponseBytes) != nil {
		return newGatewayError(ErrProviderResponse, "invalid AI response JSON", nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		return newGatewayError(ErrProviderResponse, "unsupported AI response fields", nil)
	}
	return nil
}
