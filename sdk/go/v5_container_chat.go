package campusos

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	v5ContainerContract = "campusos.container-pipe/v1"
	v5ContainerService  = "/campusos.container.v1.Runtime/"
	v5ContainerMaxJSON  = 64 << 10
	v5ContainerMaxFrame = 96 << 10
	v5ContainerMaxHead  = 8 << 10
)

// ErrV5ContainerChatProtocol is deliberately generic: input, configuration and
// handler errors never expose their contents to the pipe or the returned error.
var ErrV5ContainerChatProtocol = errors.New("plugin.container_protocol_invalid")

// V5RuntimeConfiguration contains ordinary, schema-validated configuration
// values only. The host does not send Secret references, credentials or owners.
// Each call receives independent copies; these bytes are never authority.
type V5RuntimeConfiguration struct {
	System json.RawMessage `json:"system,omitempty"`
	User   json.RawMessage `json:"user,omitempty"`
}

// V5ContainerChatHandler must honor ctx. DeadlineMS is a remaining duration,
// never an absolute timestamp. Native process cancellation and current
// Grant/Consent/owner checks remain the host's responsibility.
type V5ContainerChatHandler func(context.Context, V5RuntimeConfiguration, V5ChatRequest) (V5ChatResponse, error)

type v5ContainerFrame struct {
	Contract string          `json:"contract"`
	Kind     string          `json:"kind"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Failed   bool            `json:"failed,omitempty"`
}

// ServeV5ContainerChat serves sequential HTTP/1.1 exchanges on an existing
// exclusive stdin/stdout pipe. It never listens, dials, reads environment
// credentials, or exposes Host API/Event/HostResult methods. It owns input for
// the duration of the call; cancellation closes it to interrupt blocked reads.
// A handler is synchronous, so arbitrary handler code still requires the
// host's native timeout/kill boundary. No detached handler can return late data.
func ServeV5ContainerChat(ctx context.Context, input io.ReadCloser, output io.Writer, handler V5ContainerChatHandler) error {
	if ctx == nil || input == nil || output == nil || handler == nil {
		return ErrV5ContainerChatProtocol
	}
	stop := context.AfterFunc(ctx, func() {
		_ = input.Close()
		if closer, ok := output.(io.Closer); ok {
			_ = closer.Close()
		}
	})
	defer stop()
	defer input.Close()
	bounded := &v5ContainerReader{input: input}
	reader := bufio.NewReaderSize(bounded, 4096)
	var configuration V5RuntimeConfiguration
	configured := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		bounded.remaining = v5ContainerMaxFrame + v5ContainerMaxHead + 4096
		request, err := http.ReadRequest(reader)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return ErrV5ContainerChatProtocol
		}
		method := strings.TrimPrefix(request.RequestURI, v5ContainerService)
		if !v5ContainerHTTPRequest(request, method) {
			_ = v5ContainerWrite(output, 400, []byte(`{"error":"plugin.container_protocol_invalid"}`))
			return ErrV5ContainerChatProtocol
		}
		raw, err := io.ReadAll(io.LimitReader(request.Body, v5ContainerMaxFrame+1))
		_ = request.Body.Close()
		if err != nil || len(raw) > v5ContainerMaxFrame {
			return ErrV5ContainerChatProtocol
		}
		frame, err := v5ContainerRequest(raw)
		if err != nil || frame.Kind != strings.ToLower(method) {
			_ = v5ContainerWrite(output, 400, []byte(`{"error":"plugin.container_protocol_invalid"}`))
			return ErrV5ContainerChatProtocol
		}
		reply := v5ContainerFrame{Contract: v5ContainerContract, Kind: "ready"}
		switch method {
		case "Configure":
			configuration, err = v5ContainerConfiguration(frame.Payload)
			configured = err == nil
		case "Health":
			if !configured || len(frame.Payload) != 0 {
				err = ErrV5ContainerChatProtocol
			}
		case "Invoke":
			var call V5ChatRequest
			call, err = v5ContainerChatRequest(frame.Payload)
			if !configured {
				err = ErrV5ContainerChatProtocol
			}
			if err == nil {
				reply = v5ContainerInvoke(ctx, handler, configuration, call)
			}
		}
		if err != nil {
			_ = v5ContainerWrite(output, 400, []byte(`{"error":"plugin.container_protocol_invalid"}`))
			return ErrV5ContainerChatProtocol
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		wire, err := json.Marshal(reply)
		if err != nil || len(wire) > v5ContainerMaxFrame || v5ContainerWrite(output, 200, wire) != nil {
			return ErrV5ContainerChatProtocol
		}
		if request.Close {
			return nil
		}
	}
}

type v5ContainerReader struct {
	input     io.Reader
	remaining int
}

func (r *v5ContainerReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, ErrV5ContainerChatProtocol
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.input.Read(p)
	r.remaining -= n
	return n, err
}

func v5ContainerHTTPRequest(r *http.Request, method string) bool {
	if r.Method != http.MethodPost || r.Proto != "HTTP/1.1" || r.Host != "container" || r.RequestURI != v5ContainerService+method ||
		(method != "Configure" && method != "Health" && method != "Invoke") || r.Header.Get("Content-Type") != "application/json" ||
		r.ContentLength < 1 || r.ContentLength > v5ContainerMaxFrame || len(r.TransferEncoding) != 0 || len(r.Trailer) != 0 ||
		r.Header.Get("Content-Encoding") != "" || r.Header.Get("Expect") != "" || r.Header.Get("Upgrade") != "" {
		return false
	}
	head := len(r.Method) + len(r.RequestURI) + len(r.Proto) + len(r.Host) + 16
	for k, values := range r.Header {
		if len(values) != 1 {
			return false
		}
		head += len(k) + len(values[0]) + 4
	}
	return head <= v5ContainerMaxHead
}

func v5ContainerWrite(w io.Writer, status int, body []byte) error {
	response := &http.Response{StatusCode: status, ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{"Content-Type": []string{"application/json"}, "Content-Length": []string{strconv.Itoa(len(body))}}, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}
	return response.Write(w)
}

func v5ContainerRequest(raw []byte) (v5ContainerFrame, error) {
	var frame v5ContainerFrame
	fields, err := v5ContainerObject(raw, []string{"contract", "kind", "payload"}, []string{"contract", "kind"}, v5ContainerMaxFrame)
	if err != nil || json.Unmarshal(fields["contract"], &frame.Contract) != nil || frame.Contract != v5ContainerContract || json.Unmarshal(fields["kind"], &frame.Kind) != nil {
		return frame, ErrV5ContainerChatProtocol
	}
	frame.Payload = fields["payload"]
	return frame, nil
}

func v5ContainerConfiguration(raw []byte) (V5RuntimeConfiguration, error) {
	var out V5RuntimeConfiguration
	fields, err := v5ContainerObject(raw, []string{"system", "user"}, nil, v5ContainerMaxJSON)
	if err != nil {
		return out, err
	}
	for _, key := range []string{"system", "user"} {
		if value, ok := fields[key]; ok {
			if _, err := v5ContainerObject(value, nil, nil, v5ContainerMaxJSON); err != nil {
				return out, err
			}
		}
	}
	out.System, out.User = bytes.Clone(fields["system"]), bytes.Clone(fields["user"])
	return out, nil
}

func v5ContainerChatRequest(raw []byte) (V5ChatRequest, error) {
	var call V5ChatRequest
	keys := []string{"request_id", "contribution_id", "instance_generation", "deadline_ms", "prompt", "context_class"}
	if _, err := v5ContainerObject(raw, keys, keys, v5ContainerMaxJSON); err != nil || json.Unmarshal(raw, &call) != nil ||
		!v5ContainerText(call.RequestID, 1, 128) || !v5ContainerText(call.ContributionID, 1, 128) || call.InstanceGeneration < 1 ||
		call.DeadlineMS < 1 || call.DeadlineMS > 300000 || !v5ContainerText(call.Prompt, 1, 12000) || call.ContextClass != "none" {
		return call, ErrV5ContainerChatProtocol
	}
	return call, nil
}

func v5ContainerInvoke(ctx context.Context, handler V5ContainerChatHandler, configuration V5RuntimeConfiguration, call V5ChatRequest) (reply v5ContainerFrame) {
	reply = v5ContainerFrame{Contract: v5ContainerContract, Kind: "result", Failed: true}
	defer func() { _ = recover() }()
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(call.DeadlineMS)*time.Millisecond)
	defer cancel()
	if callCtx.Err() != nil {
		return reply
	}
	copied := V5RuntimeConfiguration{System: bytes.Clone(configuration.System), User: bytes.Clone(configuration.User)}
	response, err := handler(callCtx, copied, call)
	if err != nil || callCtx.Err() != nil || response.RequestID != call.RequestID || response.ContributionID != call.ContributionID || response.InstanceGeneration != call.InstanceGeneration ||
		!v5ContainerText(response.Text, 0, 50000) || response.InputTokens < 0 || response.OutputTokens < 0 || (response.FinishReason != "completed" && response.FinishReason != "length") {
		return reply
	}
	wire, err := json.Marshal(response)
	if err != nil || len(wire) > v5ContainerMaxJSON || callCtx.Err() != nil {
		return reply
	}
	reply.Failed, reply.Payload = false, wire
	return reply
}

func v5ContainerText(s string, min, max int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) >= min && utf8.RuneCountInString(s) <= max
}

// Reject duplicate keys before decoding into typed values: encoding/json alone
// accepts them. The depth and byte bounds cover configuration as well as DTOs.
func v5ContainerObject(raw []byte, allowed, required []string, max int) (map[string]json.RawMessage, error) {
	if len(raw) < 2 || len(raw) > max || !utf8.Valid(raw) {
		return nil, ErrV5ContainerChatProtocol
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if !v5ContainerUnique(d, 0) || d.Decode(new(any)) != io.EOF {
		return nil, ErrV5ContainerChatProtocol
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, ErrV5ContainerChatProtocol
	}
	if allowed != nil {
		for key := range object {
			found := false
			for _, known := range allowed {
				found = found || known == key
			}
			if !found {
				return nil, ErrV5ContainerChatProtocol
			}
		}
	}
	for _, key := range required {
		if _, ok := object[key]; !ok {
			return nil, ErrV5ContainerChatProtocol
		}
	}
	return object, nil
}

func v5ContainerUnique(d *json.Decoder, depth int) bool {
	if depth > 32 {
		return false
	}
	token, err := d.Token()
	if err != nil {
		return false
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return true
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			s, ok := key.(string)
			if err != nil || !ok || seen[s] || !v5ContainerUnique(d, depth+1) {
				return false
			}
			seen[s] = true
		}
		last, err := d.Token()
		return err == nil && last == json.Delim('}')
	case '[':
		for d.More() {
			if !v5ContainerUnique(d, depth+1) {
				return false
			}
		}
		last, err := d.Token()
		return err == nil && last == json.Delim(']')
	}
	return false
}
