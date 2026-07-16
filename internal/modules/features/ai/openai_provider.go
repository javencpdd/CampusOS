package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	return provider, nil
}

func (p *OpenAICompatibleProvider) Name() string {
	return p.config.providerName()
}

func (p *OpenAICompatibleProvider) ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	if err := request.validate(); err != nil {
		return nil, err
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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, newGatewayError(ErrProviderResponse, "read provider response", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := redactSecret(string(respBody), p.config.APIKey)
		return nil, newProviderHTTPError(ErrProviderResponse, resp.StatusCode, message, nil)
	}

	var parsed openAIChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, newGatewayError(ErrProviderResponse, "decode provider response", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, newGatewayError(ErrProviderResponse, "provider response has no choices", nil)
	}
	content := parsed.Choices[0].Message.Content
	if content == "" {
		return nil, newGatewayError(ErrProviderResponse, "provider response content is empty", nil)
	}
	return &ChatResponse{
		Provider:     p.Name(),
		Model:        parsed.Model,
		Content:      content,
		FinishReason: parsed.Choices[0].FinishReason,
		Usage: TokenUsage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
			TotalTokens:      parsed.Usage.TotalTokens,
		},
	}, nil
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

func redactSecret(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[REDACTED]")
}

type openAIChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type openAIChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (p *OpenAICompatibleProvider) String() string {
	return fmt.Sprintf("%s %s", p.Name(), p.config.Model)
}
