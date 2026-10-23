package ai

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestTextContractClosedRequests(t *testing.T) {
	invalid := []string{
		`{"messages":[]}`, `{"messages":null}`, `{"messages":[{"role":"user","content":null}]}`,
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://no-fetch.invalid"}}]}]}`,
		`{"messages":[{"role":"user","content":"ok","image_url":"https://no-fetch.invalid"}]}`,
		`{"messages":[{"role":"tool","content":"ok"}]}`, `{"messages":[{"role":"user","content":"ok"}],"tools":[]}`,
		`{"messages":[{"role":"user","content":"ok"}],"stream":true}`, `{"messages":[{"role":"user","content":"ok"}],"modalities":["text","audio"]}`,
		`{"messages":[{"role":"user","content":"a","content":"b"}]}`, `{"messages":[{"role":"user","content":"ok"}],"MESSAGES":[]}`,
		`{"messages":[{"role":"user","content":"\ud800"}]}`, `{"messages":[{"role":"user","content":"ok"}],"temperature":3}`,
		`{"messages":[{"role":"user","content":"ok"}],"max_tokens":-1}`, `{"messages":[{"role":"user","content":"ok"}],"unknown":true}`,
	}
	for i, raw := range invalid {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			var r ChatRequest
			if err := json.Unmarshal([]byte(raw), &r); err == nil {
				t.Fatal("unsupported/ambiguous input accepted")
			}
		})
	}
	var r ChatRequest
	if err := json.Unmarshal([]byte(`{"messages":[{"role":"system","content":"instructions"},{"role":"user","content":"普通文本"}],"temperature":0.2,"max_tokens":32}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Messages[1].Content != "普通文本" {
		t.Fatal("valid text changed")
	}
}

type contractProvider struct {
	definition Definition
	calls      int
	run        func(context.Context, ChatRequest) (*ChatResponse, error)
}

func (p *contractProvider) Definition() Definition { return p.definition }
func (p *contractProvider) Name() string           { return "owned-text" }
func (p *contractProvider) ChatCompletion(ctx context.Context, r ChatRequest) (*ChatResponse, error) {
	p.calls++
	return p.run(ctx, r)
}

type contractLimiter struct{ calls int }

func (l *contractLimiter) Acquire(context.Context) (func(), error) { l.calls++; return func() {}, nil }
func TestTextContractRejectsBeforeLimiterAndProvider(t *testing.T) {
	p := &contractProvider{definition: textDefinition(), run: func(context.Context, ChatRequest) (*ChatResponse, error) {
		t.Fatal("must not execute")
		return nil, nil
	}}
	l := &contractLimiter{}
	g, err := NewGateway(p, GatewayConfig{DefaultModel: "owned-model", DefaultSource: "owned-source"}, WithLimiter(l))
	if err != nil {
		t.Fatal(err)
	}
	nan := math.NaN()
	infinity := math.Inf(1)
	cases := []ChatRequest{{}, {Messages: []Message{{Role: "tool", Content: "x"}}}, {Messages: []Message{{Role: "user", Content: strings.Repeat("x", MaxChatTextBytes+1)}}}, {Messages: []Message{{Role: "user", Content: "x"}}, Temperature: &nan}, {Messages: []Message{{Role: "user", Content: "x"}}, Temperature: &infinity}, {Messages: []Message{{Role: "user", Content: "x"}}, Model: "unapproved"}, {Messages: []Message{{Role: "user", Content: "x"}}, Source: "unapproved"}, {Messages: make([]Message, 65)}}
	for _, r := range cases {
		if response, err := g.ChatCompletion(t.Context(), r); err == nil || response != nil {
			t.Fatal("invalid input was not closed")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = g.ChatCompletion(ctx, ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}}); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancel lost", err)
	}
	if p.calls != 0 || l.calls != 0 {
		t.Fatal("validation spent execution/rate budget", p.calls, l.calls)
	}
	p.definition = Definition{}
	if _, err := NewGateway(p, GatewayConfig{}); err == nil {
		t.Fatal("missing capability defaults allowed")
	}
	p.definition = textDefinition()
	p.definition.Tools = true
	if _, err := NewGateway(p, GatewayConfig{}); err == nil {
		t.Fatal("unsupported declaration allowed")
	}
}
func TestTextContractUnknownAndCancelledProviderResults(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "safe-error", true: "late-cancel"}[cancelled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			logger := NewMemoryCallLogger()
			p := &contractProvider{definition: textDefinition(), run: func(context.Context, ChatRequest) (*ChatResponse, error) {
				if cancelled {
					cancel()
					return &ChatResponse{Content: "LATE_PRIVATE_RESULT"}, nil
				}
				return nil, errors.New("PRIVATE_PROMPT key=PRIVATE_SECRET https://private-endpoint.invalid")
			}}
			g, err := NewGateway(p, GatewayConfig{}, WithCallLogger(logger))
			if err != nil {
				t.Fatal(err)
			}
			result, err := g.ChatCompletion(ctx, ChatRequest{Messages: []Message{{Role: "user", Content: "PRIVATE_PROMPT"}}})
			if result != nil || err == nil {
				t.Fatal("unsafe outcome returned")
			}
			if cancelled && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			logs, _ := logger.List(t.Context(), 10)
			raw, _ := json.Marshal(logs)
			for _, marker := range []string{"PRIVATE_PROMPT", "PRIVATE_SECRET", "private-endpoint", "LATE_PRIVATE_RESULT"} {
				if strings.Contains(string(raw), marker) || strings.Contains(err.Error(), marker) {
					t.Fatal("payload/secret in error or logs")
				}
			}
			if p.calls != 1 || len(logs) != 1 || logs[0].Status != CallStatusError {
				t.Fatal("retry or incorrect success log", p.calls, logs)
			}
		})
	}
}
func TestTextContractRejectsUnsupportedProviderResponses(t *testing.T) {
	cases := []string{
		`{"choices":[]}`, `{"choices":[{"message":{"role":"assistant","content":[{"type":"text","text":"ok"}]},"finish_reason":"stop"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"ok","tool_calls":[{"name":"unsafe"}]},"finish_reason":"stop"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"ok","function_call":{"name":"unsafe"}},"finish_reason":"stop"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"ok","audio":{"data":"unsafe"}},"finish_reason":"stop"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"ok","image_url":"unsafe"},"finish_reason":"stop"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"a","content":"b"},"finish_reason":"stop"}]}`,
		`{"choices":[{"message":{"role":"tool","content":"ok"},"finish_reason":"stop"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"\ud800"},"finish_reason":"stop"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"total_tokens":-1}}`,
		`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"unknown_modality":"unsafe"}`,
		`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}` + strings.Repeat(" ", MaxChatResponseBytes),
	}
	for i, body := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			p, err := NewOpenAICompatibleProvider(OpenAICompatibleConfig{BaseURL: "https://owned.invalid/v1", APIKey: "owned-key", Model: "owned"}, WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return jsonResponse(200, body), nil })}))
			if err != nil {
				t.Fatal(err)
			}
			out, err := p.ChatCompletion(t.Context(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
			if out != nil || err == nil {
				t.Fatal("unsupported/truncated output accepted")
			}
		})
	}
}
func TestTextContractDefinitionCopiesDoNotChangeProvider(t *testing.T) {
	p, err := NewOpenAICompatibleProvider(OpenAICompatibleConfig{BaseURL: "https://owned.invalid/v1", Model: "owned"})
	if err != nil {
		t.Fatal(err)
	}
	d := p.Definition()
	d.InputModalities[0] = "audio"
	if p.Definition().validate() != nil {
		t.Fatal("mutable declaration escaped")
	}
}
