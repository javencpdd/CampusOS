package ai

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGatewayChatCompletionLogsSuccess(t *testing.T) {
	logger := NewMemoryCallLogger()
	provider := fakeProvider{
		name: "fake-ai",
		response: &ChatResponse{
			Provider: "fake-ai",
			Model:    "campus-model",
			Content:  "ok",
			Usage:    TokenUsage{TotalTokens: 7},
		},
	}
	start := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	ticks := []time.Time{start, start.Add(50 * time.Millisecond)}
	gateway, err := NewGateway(provider, GatewayConfig{
		DefaultModel:  "campus-model",
		DefaultSource: "unit-test",
	}, WithCallLogger(logger), WithClock(func() time.Time {
		next := ticks[0]
		ticks = ticks[1:]
		return next
	}))
	if err != nil {
		t.Fatalf("new gateway: %v", err)
	}

	response, err := gateway.ChatCompletion(t.Context(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("chat completion: %v", err)
	}
	if response.Content != "ok" {
		t.Fatalf("unexpected response: %#v", response)
	}
	logs := logger.List(10)
	if len(logs) != 1 {
		t.Fatalf("expected one log, got %#v", logs)
	}
	if logs[0].Status != CallStatusSuccess || logs[0].Source != "unit-test" || logs[0].Usage.TotalTokens != 7 {
		t.Fatalf("unexpected log: %#v", logs[0])
	}
	if logs[0].Duration != 50*time.Millisecond {
		t.Fatalf("unexpected duration: %s", logs[0].Duration)
	}
}

func TestGatewayChatCompletionLogsProviderError(t *testing.T) {
	logger := NewMemoryCallLogger()
	gateway, err := NewGateway(fakeProvider{
		name: "fake-ai",
		err:  errors.New("provider down"),
	}, GatewayConfig{}, WithCallLogger(logger))
	if err != nil {
		t.Fatalf("new gateway: %v", err)
	}

	_, err = gateway.ChatCompletion(t.Context(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatalf("expected provider error")
	}
	logs := logger.List(10)
	if len(logs) != 1 || logs[0].Status != CallStatusError || logs[0].Error == "" {
		t.Fatalf("unexpected logs: %#v", logs)
	}
}

func TestGatewayRateLimit(t *testing.T) {
	logger := NewMemoryCallLogger()
	limiter := NewInMemoryLimiter(0, 1)
	now := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	gateway, err := NewGateway(fakeProvider{
		name:     "fake-ai",
		response: &ChatResponse{Provider: "fake-ai", Model: "campus-model", Content: "ok"},
	}, GatewayConfig{}, WithCallLogger(logger), WithLimiter(limiter))
	if err != nil {
		t.Fatalf("new gateway: %v", err)
	}

	request := ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}}
	if _, err := gateway.ChatCompletion(t.Context(), request); err != nil {
		t.Fatalf("first call should pass: %v", err)
	}
	if _, err := gateway.ChatCompletion(t.Context(), request); err == nil {
		t.Fatalf("second call should be rate limited")
	}
	logs := logger.List(10)
	if len(logs) != 2 || logs[1].Status != CallStatusRateLimited {
		t.Fatalf("unexpected logs: %#v", logs)
	}
}

type fakeProvider struct {
	name     string
	response *ChatResponse
	err      error
}

func (p fakeProvider) Name() string {
	return p.name
}

func (p fakeProvider) ChatCompletion(_ context.Context, request ChatRequest) (*ChatResponse, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	if p.err != nil {
		return nil, p.err
	}
	return p.response, nil
}
