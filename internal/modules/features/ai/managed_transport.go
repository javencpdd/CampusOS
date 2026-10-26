package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
)

type localModelEgress struct {
	endpoint string
	client   *http.Client
}

func newLocalModelEgress(p ManagedProfile) (managedEgress, error) {
	u, e := url.Parse(p.Endpoint)
	if e != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() || u.Port() == "" {
		return nil, ErrManagedUnavailable
	}
	target := u.Host
	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, MaxResponseHeaderBytes: 32 << 10, ResponseHeaderTimeout: timeout, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != target {
			return nil, ErrManagedDenied
		}
		return (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", target)
	}}
	return &localModelEgress{endpoint: p.Endpoint, client: &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (l *localModelEgress) Do(ctx context.Context, r security.EgressRequest) (security.EgressResponse, error) {
	if r.URL != l.endpoint || r.Method != "POST" || !r.DenyRedirects || len(r.Body) > MaxChatResponseBytes {
		return security.EgressResponse{}, ErrManagedDenied
	}
	req, e := http.NewRequestWithContext(ctx, "POST", r.URL, bytes.NewReader(r.Body))
	if e != nil {
		return security.EgressResponse{}, ErrManagedUnavailable
	}
	req.Header = r.Headers.Clone()
	response, e := l.client.Do(req)
	if e != nil {
		return security.EgressResponse{}, classifyContextError(e)
	}
	defer response.Body.Close()
	body, e := io.ReadAll(io.LimitReader(response.Body, MaxChatResponseBytes+1))
	if e != nil {
		return security.EgressResponse{}, classifyContextError(e)
	}
	if len(body) > MaxChatResponseBytes {
		return security.EgressResponse{}, ErrManagedUnavailable
	}
	return security.EgressResponse{StatusCode: response.StatusCode, Body: body}, nil
}
func (s *ManagedService) execute(ctx context.Context, p ManagedProfile, b ManagedBinding, request ChatRequest, before func(context.Context) error) (*ChatResponse, error) {
	if e := before(ctx); e != nil {
		return nil, e
	}
	secret, e := s.secret(ctx, p)
	if e != nil {
		return nil, e
	}
	defer clear(secret)
	var transport managedEgress
	if p.Definition == "builtin.local-openai.text" {
		transport, e = newLocalModelEgress(p)
	} else {
		u, _ := url.Parse(p.Endpoint)
		transport, e = s.remote(security.EgressPolicy{AllowedOrigins: []string{u.Scheme + "://" + u.Host}, MaxRequestBytes: MaxChatResponseBytes, MaxResponseBytes: MaxChatResponseBytes, Timeout: time.Duration(p.TimeoutMS) * time.Millisecond})
	}
	if e != nil {
		return nil, ErrManagedUnavailable
	}
	body, e := json.Marshal(openAIChatRequest{Model: p.Model, Messages: request.Messages, Temperature: request.Temperature, MaxTokens: request.MaxTokens})
	if e != nil {
		return nil, ErrManagedUnavailable
	}
	defer clear(body)
	headers := http.Header{"Content-Type": []string{"application/json"}}
	if len(secret) > 0 {
		headers.Set("Authorization", "Bearer "+string(secret))
	}
	if e = before(ctx); e != nil {
		return nil, e
	}
	response, e := transport.Do(ctx, security.EgressRequest{Method: "POST", URL: p.Endpoint, Headers: headers, Body: body, DenyRedirects: true})
	headers.Del("Authorization")
	if e != nil {
		return nil, classifyContextError(e)
	}
	defer clear(response.Body)
	if e = before(ctx); e != nil {
		return nil, e
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, newProviderHTTPError(ErrProviderResponse, response.StatusCode, "provider_response", nil)
	}
	var parsed openAIChatResponse
	if decodeTextResponse(response.Body, &parsed) != nil || len(parsed.Choices) != 1 {
		return nil, newGatewayError(ErrProviderResponse, "provider_response", nil)
	}
	choice := parsed.Choices[0]
	if parsed.Model != p.Model || choice.Message.Role != "assistant" || forbiddenResponseValue(choice.Message.ToolCalls) || forbiddenResponseValue(choice.Message.FunctionCall) || forbiddenResponseValue(choice.Message.Audio) || forbiddenResponseValue(choice.Message.Refusal) || forbiddenResponseValue(choice.Message.Annotations) || (choice.FinishReason != "stop" && choice.FinishReason != "length") {
		return nil, newGatewayError(ErrProviderResponse, "provider_response", nil)
	}
	out := &ChatResponse{Provider: p.Definition, Model: p.Model, Content: choice.Message.Content, FinishReason: choice.FinishReason, Usage: TokenUsage{PromptTokens: parsed.Usage.PromptTokens, CompletionTokens: parsed.Usage.CompletionTokens, TotalTokens: parsed.Usage.TotalTokens}}
	if validateTextResponse(out) != nil || out.Usage.PromptTokens > 1000000000 || out.Usage.CompletionTokens > request.MaxTokens || out.Usage.TotalTokens > 1000000000 {
		return nil, newGatewayError(ErrProviderResponse, "provider_response", nil)
	}
	return out, nil
}
