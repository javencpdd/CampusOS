package campusos

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func v5ChatTestRequest() V5ChatRequest {
	return V5ChatRequest{RequestID: "chat-1", ContributionID: "chat-main", InstanceGeneration: 17, DeadlineMS: 3000, Prompt: "你好", ContextClass: "none"}
}

func v5ChatTestHandler(_ context.Context, _ V5RuntimeConfiguration, request V5ChatRequest) (V5ChatResponse, error) {
	return V5ChatResponse{RequestID: request.RequestID, ContributionID: request.ContributionID, InstanceGeneration: request.InstanceGeneration, Text: "Echo: " + request.Prompt, FinishReason: "completed"}, nil
}

func v5ChatTestFrame(t *testing.T, kind string, payload any) []byte {
	t.Helper()
	frame := map[string]any{"contract": v5ContainerContract, "kind": kind}
	if payload != nil {
		frame["payload"] = payload
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func v5ChatTestHTTP(method string, body []byte) []byte {
	return append([]byte(fmt.Sprintf("POST %s%s HTTP/1.1\r\nHost: container\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", v5ContainerService, method, len(body))), body...)
}

func v5ChatTestRead(t *testing.T, reader *bufio.Reader) (int, v5ContainerFrame, []byte) {
	t.Helper()
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var frame v5ContainerFrame
	if response.StatusCode == 200 {
		if err := json.Unmarshal(raw, &frame); err != nil || frame.Contract != v5ContainerContract {
			t.Fatal("invalid response frame", err)
		}
	}
	return response.StatusCode, frame, raw
}

func v5ChatTestRun(t *testing.T, raw []byte, handler V5ContainerChatHandler) ([]byte, error) {
	t.Helper()
	var output bytes.Buffer
	err := ServeV5ContainerChat(context.Background(), io.NopCloser(bytes.NewReader(raw)), &output, handler)
	return output.Bytes(), err
}

func v5ChatTestConfigure(t *testing.T) []byte {
	return v5ChatTestHTTP("Configure", v5ChatTestFrame(t, "configure", json.RawMessage(`{"system":{"prefix":"system: "},"user":{"prefix":"本人: "}}`)))
}

func TestServeV5ContainerChatHTTPConfigurationIsolation(t *testing.T) {
	request := v5ChatTestRequest()
	raw := v5ChatTestConfigure(t)
	raw = append(raw, v5ChatTestHTTP("Health", v5ChatTestFrame(t, "health", nil))...)
	for range 2 {
		raw = append(raw, v5ChatTestHTTP("Invoke", v5ChatTestFrame(t, "invoke", request))...)
	}
	calls := 0
	output, err := v5ChatTestRun(t, raw, func(ctx context.Context, config V5RuntimeConfiguration, call V5ChatRequest) (V5ChatResponse, error) {
		calls++
		if string(config.System) != `{"prefix":"system: "}` || string(config.User) != `{"prefix":"本人: "}` || call != request {
			t.Fatal("configuration or DTO changed between calls")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("handler has no relative deadline")
		}
		config.System[0], config.User[0] = '!', '!'
		return v5ChatTestHandler(ctx, config, call)
	})
	if err != nil || calls != 2 {
		t.Fatal("valid sequential calls failed", err, calls)
	}
	reader := bufio.NewReader(bytes.NewReader(output))
	for range 2 {
		status, frame, _ := v5ChatTestRead(t, reader)
		if status != 200 || frame.Kind != "ready" || frame.Failed || len(frame.Payload) != 0 {
			t.Fatal("Configure/Health readiness invalid")
		}
	}
	for range 2 {
		status, frame, _ := v5ChatTestRead(t, reader)
		var response V5ChatResponse
		if status != 200 || frame.Kind != "result" || frame.Failed || json.Unmarshal(frame.Payload, &response) != nil || response.Text != "Echo: 你好" || response.InstanceGeneration != 17 {
			t.Fatal("typed response invalid")
		}
	}
}

func TestServeV5ContainerChatRejectsInvalidProtocol(t *testing.T) {
	valid := v5ChatTestHTTP("Health", v5ChatTestFrame(t, "health", nil))
	cases := map[string][]byte{
		"unknown-method":      v5ChatTestHTTP("Event", v5ChatTestFrame(t, "event", nil)),
		"host-result":         v5ChatTestHTTP("HostResult", v5ChatTestFrame(t, "host_result", nil)),
		"wrong-kind":          v5ChatTestHTTP("Health", v5ChatTestFrame(t, "invoke", nil)),
		"query":               bytes.Replace(valid, []byte("Health HTTP"), []byte("Health?x=1 HTTP"), 1),
		"wrong-verb":          bytes.Replace(valid, []byte("POST "), []byte("GET "), 1),
		"wrong-version":       bytes.Replace(valid, []byte("HTTP/1.1"), []byte("HTTP/1.0"), 1),
		"wrong-host":          bytes.Replace(valid, []byte("Host: container"), []byte("Host: other"), 1),
		"wrong-content":       bytes.Replace(valid, []byte("application/json"), []byte("text/plain"), 1),
		"duplicate-header":    bytes.Replace(valid, []byte("Content-Type: application/json\r\n"), []byte("Content-Type: application/json\r\nContent-Type: application/json\r\n"), 1),
		"truncated-body":      valid[:len(valid)-1],
		"duplicate-frame-key": v5ChatTestHTTP("Health", []byte(`{"contract":"campusos.container-pipe/v1","kind":"invoke","kind":"health"}`)),
		"unknown-frame-key":   v5ChatTestHTTP("Health", []byte(`{"contract":"campusos.container-pipe/v1","kind":"health","token":"SECRET-SENTINEL"}`)),
		"wrong-contract":      v5ChatTestHTTP("Health", []byte(`{"contract":"campusos.container-pipe/v2","kind":"health"}`)),
		"extra-json":          v5ChatTestHTTP("Health", []byte(`{"contract":"campusos.container-pipe/v1","kind":"health"}{}`)),
		"null-payload":        v5ChatTestHTTP("Health", v5ChatTestFrame(t, "health", json.RawMessage(`null`))),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			output, err := v5ChatTestRun(t, append(v5ChatTestConfigure(t), input...), v5ChatTestHandler)
			if !errors.Is(err, ErrV5ContainerChatProtocol) || bytes.Contains(output, []byte("SECRET-SENTINEL")) {
				t.Fatal("invalid protocol not closed generically", err)
			}
		})
	}
	for _, method := range []string{"Health", "Invoke"} {
		t.Run("before-configure-"+method, func(t *testing.T) {
			payload := any(nil)
			if method == "Invoke" {
				payload = v5ChatTestRequest()
			}
			_, err := v5ChatTestRun(t, v5ChatTestHTTP(method, v5ChatTestFrame(t, strings.ToLower(method), payload)), v5ChatTestHandler)
			if err != ErrV5ContainerChatProtocol {
				t.Fatal("unconfigured runtime accepted", err)
			}
		})
	}
}

func TestServeV5ContainerChatRejectsInvalidTypedRequests(t *testing.T) {
	good, _ := json.Marshal(v5ChatTestRequest())
	cases := map[string][]byte{
		"unknown":               append(append([]byte(nil), good[:len(good)-1]...), []byte(`,"owner":"SECRET-SENTINEL"}`)...),
		"duplicate":             bytes.Replace(good, []byte(`"deadline_ms":3000`), []byte(`"deadline_ms":3000,"deadline_ms":1`), 1),
		"absolute-deadline":     bytes.Replace(good, []byte(`"deadline_ms":3000`), []byte(`"deadline_ms":1800000000000`), 1),
		"zero-deadline":         bytes.Replace(good, []byte(`"deadline_ms":3000`), []byte(`"deadline_ms":0`), 1),
		"fractional-generation": bytes.Replace(good, []byte(`"instance_generation":17`), []byte(`"instance_generation":1.5`), 1),
		"public-context":        bytes.Replace(good, []byte(`"context_class":"none"`), []byte(`"context_class":"public"`), 1),
		"missing-prompt":        bytes.Replace(good, []byte(`"prompt":"你好",`), nil, 1),
		"null-prompt":           bytes.Replace(good, []byte(`"prompt":"你好"`), []byte(`"prompt":null`), 1),
		"zero-generation":       bytes.Replace(good, []byte(`"instance_generation":17`), []byte(`"instance_generation":0`), 1),
		"extra-json":            append(append([]byte(nil), good...), []byte(`{}`)...),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			// Raw framing intentionally avoids json.Marshal sanitizing malformed JSON.
			frame := append([]byte(`{"contract":"campusos.container-pipe/v1","kind":"invoke","payload":`), payload...)
			frame = append(frame, '}')
			called := false
			output, err := v5ChatTestRun(t, append(v5ChatTestConfigure(t), v5ChatTestHTTP("Invoke", frame)...), func(context.Context, V5RuntimeConfiguration, V5ChatRequest) (V5ChatResponse, error) {
				called = true
				return V5ChatResponse{}, nil
			})
			if err != ErrV5ContainerChatProtocol || called || bytes.Contains(output, []byte("SECRET-SENTINEL")) {
				t.Fatal("invalid typed request reached handler", err, called)
			}
		})
	}
}

func TestServeV5ContainerChatBoundsAndConfiguration(t *testing.T) {
	longRequest := v5ChatTestRequest()
	longRequest.Prompt = strings.Repeat("x", 12001)
	configCases := map[string]json.RawMessage{
		"secret-ref-field":  json.RawMessage(`{"system":{},"secret_refs":{"key":"SECRET-SENTINEL"}}`),
		"duplicate-config":  json.RawMessage(`{"system":{"prefix":"A","prefix":"B"}}`),
		"null-values":       json.RawMessage(`{"system":null}`),
		"non-object-values": json.RawMessage(`{"user":[]}`),
		"deep-json":         json.RawMessage(`{"system":{"nested":` + strings.Repeat("[", 33) + `0` + strings.Repeat("]", 33) + `}}`),
		"too-many-bytes":    json.RawMessage(`{"system":{"prefix":"` + strings.Repeat("x", v5ContainerMaxJSON) + `"}}`),
	}
	for name, payload := range configCases {
		t.Run(name, func(t *testing.T) {
			out, err := v5ChatTestRun(t, v5ChatTestHTTP("Configure", v5ChatTestFrame(t, "configure", payload)), v5ChatTestHandler)
			if err != ErrV5ContainerChatProtocol || bytes.Contains(out, []byte("SECRET-SENTINEL")) {
				t.Fatal("unsafe configuration accepted", err)
			}
		})
	}
	inputs := map[string][]byte{
		"prompt": append(v5ChatTestConfigure(t), v5ChatTestHTTP("Invoke", v5ChatTestFrame(t, "invoke", longRequest))...),
		"frame":  v5ChatTestHTTP("Configure", bytes.Repeat([]byte(" "), v5ContainerMaxFrame+1)),
		"header": []byte("POST " + v5ContainerService + "Health HTTP/1.1\r\nHost: container\r\nX-Large: " + strings.Repeat("x", 1<<20)),
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			_, err := v5ChatTestRun(t, input, v5ChatTestHandler)
			if err != ErrV5ContainerChatProtocol {
				t.Fatal("budget not enforced", err)
			}
		})
	}
}

func TestServeV5ContainerChatHandlerFailureClosed(t *testing.T) {
	cases := map[string]V5ContainerChatHandler{
		"error": func(context.Context, V5RuntimeConfiguration, V5ChatRequest) (V5ChatResponse, error) {
			return V5ChatResponse{}, errors.New("SECRET-SENTINEL")
		},
		"panic": func(context.Context, V5RuntimeConfiguration, V5ChatRequest) (V5ChatResponse, error) {
			panic("SECRET-SENTINEL")
		},
		"wrong-request": func(ctx context.Context, c V5RuntimeConfiguration, r V5ChatRequest) (V5ChatResponse, error) {
			out, _ := v5ChatTestHandler(ctx, c, r)
			out.RequestID = "other"
			return out, nil
		},
		"wrong-generation": func(ctx context.Context, c V5RuntimeConfiguration, r V5ChatRequest) (V5ChatResponse, error) {
			out, _ := v5ChatTestHandler(ctx, c, r)
			out.InstanceGeneration++
			return out, nil
		},
		"wrong-contribution": func(ctx context.Context, c V5RuntimeConfiguration, r V5ChatRequest) (V5ChatResponse, error) {
			out, _ := v5ChatTestHandler(ctx, c, r)
			out.ContributionID = "other"
			return out, nil
		},
		"negative-tokens": func(ctx context.Context, c V5RuntimeConfiguration, r V5ChatRequest) (V5ChatResponse, error) {
			out, _ := v5ChatTestHandler(ctx, c, r)
			out.InputTokens = -1
			return out, nil
		},
		"wrong-finish": func(ctx context.Context, c V5RuntimeConfiguration, r V5ChatRequest) (V5ChatResponse, error) {
			out, _ := v5ChatTestHandler(ctx, c, r)
			out.FinishReason = "secret"
			return out, nil
		},
		"too-long-response": func(ctx context.Context, c V5RuntimeConfiguration, r V5ChatRequest) (V5ChatResponse, error) {
			out, _ := v5ChatTestHandler(ctx, c, r)
			out.Text = strings.Repeat("x", 50001)
			return out, nil
		},
		"response-byte-budget": func(ctx context.Context, c V5RuntimeConfiguration, r V5ChatRequest) (V5ChatResponse, error) {
			out, _ := v5ChatTestHandler(ctx, c, r)
			out.Text = strings.Repeat("字", 30000)
			return out, nil
		},
		"invalid-utf8": func(ctx context.Context, c V5RuntimeConfiguration, r V5ChatRequest) (V5ChatResponse, error) {
			out, _ := v5ChatTestHandler(ctx, c, r)
			out.Text = string([]byte{0xff})
			return out, nil
		},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			raw := append(v5ChatTestConfigure(t), v5ChatTestHTTP("Invoke", v5ChatTestFrame(t, "invoke", v5ChatTestRequest()))...)
			out, err := v5ChatTestRun(t, raw, handler)
			if err != nil || bytes.Contains(out, []byte("SECRET-SENTINEL")) {
				t.Fatal("handler failure escaped", err)
			}
			reader := bufio.NewReader(bytes.NewReader(out))
			_, _, _ = v5ChatTestRead(t, reader)
			status, frame, _ := v5ChatTestRead(t, reader)
			if status != 200 || frame.Kind != "result" || !frame.Failed || len(frame.Payload) != 0 {
				t.Fatal("invalid or failed handler released a result")
			}
		})
	}
}

func TestServeV5ContainerChatDeadlineAndPipeCancellation(t *testing.T) {
	t.Run("relative-expiry", func(t *testing.T) {
		request := v5ChatTestRequest()
		request.DeadlineMS = 5
		raw := append(v5ChatTestConfigure(t), v5ChatTestHTTP("Invoke", v5ChatTestFrame(t, "invoke", request))...)
		out, err := v5ChatTestRun(t, raw, func(ctx context.Context, c V5RuntimeConfiguration, r V5ChatRequest) (V5ChatResponse, error) {
			<-ctx.Done()
			return v5ChatTestHandler(ctx, c, r)
		})
		if err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(bytes.NewReader(out))
		_, _, _ = v5ChatTestRead(t, reader)
		_, frame, _ := v5ChatTestRead(t, reader)
		if !frame.Failed || len(frame.Payload) != 0 {
			t.Fatal("expired handler result escaped")
		}
	})
	t.Run("real-pipes-without-listeners", func(t *testing.T) {
		input, hostOutput := io.Pipe()
		hostInput, output := io.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		defer hostOutput.Close()
		defer hostInput.Close()
		done := make(chan error, 1)
		go func() { done <- ServeV5ContainerChat(ctx, input, output, v5ChatTestHandler) }()
		reader := bufio.NewReader(hostInput)
		for _, raw := range [][]byte{v5ChatTestConfigure(t), v5ChatTestHTTP("Invoke", v5ChatTestFrame(t, "invoke", v5ChatTestRequest()))} {
			if _, err := hostOutput.Write(raw); err != nil {
				t.Fatal(err)
			}
			status, frame, _ := v5ChatTestRead(t, reader)
			if status != 200 || frame.Failed {
				t.Fatal("actual pipe exchange failed")
			}
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled blocked read did not close", err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancelled stdio read survived")
		}
	})
}
