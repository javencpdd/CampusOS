package ai

import (
	"context"
	"fmt"
)

type ErrorCode string

const (
	ErrInvalidRequest   ErrorCode = "invalid_request"
	ErrProviderConfig   ErrorCode = "provider_config"
	ErrProviderRequest  ErrorCode = "provider_request"
	ErrProviderResponse ErrorCode = "provider_response"
	ErrRateLimited      ErrorCode = "rate_limited"
	ErrContextCancelled ErrorCode = "context_cancelled"
	ErrContextDeadline  ErrorCode = "context_deadline"
)

type GatewayError struct {
	Code       ErrorCode
	Message    string
	StatusCode int
	Err        error
}

func (e *GatewayError) Error() string {
	if e == nil {
		return ""
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("ai: %s: status=%d: %s", e.Code, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("ai: %s: %s", e.Code, e.Message)
}

func (e *GatewayError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newGatewayError(code ErrorCode, message string, err error) *GatewayError {
	return &GatewayError{Code: code, Message: message, Err: err}
}

func newProviderHTTPError(code ErrorCode, statusCode int, message string, err error) *GatewayError {
	return &GatewayError{Code: code, StatusCode: statusCode, Message: message, Err: err}
}

type Provider interface {
	Name() string
	ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error)
}

type EmbeddingProvider interface {
	Embedding(ctx context.Context, request EmbeddingRequest) (*EmbeddingResponse, error)
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string            `json:"model,omitempty"`
	Messages    []Message         `json:"messages"`
	Temperature *float64          `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Source      string            `json:"source,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type ChatResponse struct {
	Provider     string     `json:"provider"`
	Model        string     `json:"model"`
	Content      string     `json:"content"`
	FinishReason string     `json:"finish_reason,omitempty"`
	Usage        TokenUsage `json:"usage"`
}

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

type EmbeddingRequest struct {
	Model  string   `json:"model,omitempty"`
	Input  []string `json:"input"`
	Source string   `json:"source,omitempty"`
}

type EmbeddingResponse struct {
	Provider   string      `json:"provider"`
	Model      string      `json:"model"`
	Embeddings [][]float64 `json:"embeddings"`
	Usage      TokenUsage  `json:"usage"`
}

func (r ChatRequest) validate() error {
	if len(r.Messages) == 0 {
		return newGatewayError(ErrInvalidRequest, "messages are required", nil)
	}
	for i, msg := range r.Messages {
		if msg.Role == "" {
			return newGatewayError(ErrInvalidRequest, fmt.Sprintf("messages[%d].role is required", i), nil)
		}
		if msg.Content == "" {
			return newGatewayError(ErrInvalidRequest, fmt.Sprintf("messages[%d].content is required", i), nil)
		}
	}
	return nil
}
