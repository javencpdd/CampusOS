package ai

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleProviderChatCompletion(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		if req.URL.String() != "https://ai.example.test/v1/chat/completions" {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-secret" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}
		var payload openAIChatRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.Model != "campus-model" || len(payload.Messages) != 1 || payload.Messages[0].Content != "hello" {
			t.Fatalf("unexpected payload: %#v", payload)
		}
		return jsonResponse(http.StatusOK, `{
			"model": "campus-model",
			"choices": [
				{"message": {"role": "assistant", "content": "hi"}, "finish_reason": "stop"}
			],
			"usage": {"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}
		}`), nil
	})
	provider, err := NewOpenAICompatibleProvider(OpenAICompatibleConfig{
		BaseURL: "https://ai.example.test/v1",
		APIKey:  "test-secret",
		Model:   "campus-model",
		Timeout: time.Second,
	}, WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	response, err := provider.ChatCompletion(t.Context(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("chat completion: %v", err)
	}
	if response.Provider != "openai-compatible" || response.Content != "hi" || response.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestOpenAICompatibleProviderRedactsAPIKeyFromErrors(t *testing.T) {
	transport := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusUnauthorized, `{"error":"bad key test-secret"}`), nil
	})
	provider, err := NewOpenAICompatibleProvider(OpenAICompatibleConfig{
		BaseURL: "https://ai.example.test/v1",
		APIKey:  "test-secret",
		Model:   "campus-model",
	}, WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	_, err = provider.ChatCompletion(t.Context(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatalf("expected provider error")
	}
	if strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("error leaked API key: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected redacted marker, got: %v", err)
	}
}

func TestOpenAICompatibleProviderRejectsMissingAPIKey(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider(OpenAICompatibleConfig{
		BaseURL: "https://ai.example.test/v1",
		Model:   "campus-model",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	_, err = provider.ChatCompletion(t.Context(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "API key is required") {
		t.Fatalf("expected missing API key error, got %v", err)
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
