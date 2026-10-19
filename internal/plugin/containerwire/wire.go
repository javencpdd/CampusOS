// Package containerwire implements the finite v5 Container pipe transport.
// A private stdin/stdout connection carries HTTP/1.1 or unary gRPC over HTTP/2;
// there are no listeners, callback URLs, credentials or ambient network access.
package containerwire

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
)

const Contract = "campusos.container-pipe/v1"
const MaxMessage = 96 << 10
const Service = "/campusos.container.v1.Runtime/"

var ErrProtocol = errors.New("plugin.container_protocol_invalid")

type Frame struct {
	Contract   string          `json:"contract"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Capability string          `json:"capability,omitempty"`
	Failed     bool            `json:"failed,omitempty"`
}

func Encode(f Frame, grpc bool) ([]byte, error) {
	f.Contract = Contract
	b, err := json.Marshal(f)
	if err != nil || len(b) > MaxMessage {
		return nil, ErrProtocol
	}
	if !grpc {
		return b, nil
	}
	out := make([]byte, 5, len(b)+5)
	binary.BigEndian.PutUint32(out[1:], uint32(len(b)))
	return append(out, b...), nil
}

func Decode(r io.Reader, grpc bool) (Frame, error) {
	var f Frame
	b, err := io.ReadAll(io.LimitReader(r, MaxMessage+6))
	if err != nil || len(b) > MaxMessage+5 {
		return f, ErrProtocol
	}
	if grpc {
		if len(b) < 5 || b[0] != 0 || int(binary.BigEndian.Uint32(b[1:5])) != len(b)-5 {
			return f, ErrProtocol
		}
		b = b[5:]
	}
	if len(b) > MaxMessage {
		return f, ErrProtocol
	}
	unique := json.NewDecoder(bytes.NewReader(b))
	unique.UseNumber()
	if !uniqueValue(unique, 0) {
		return Frame{}, ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil || d.Decode(new(any)) != io.EOF || f.Contract != Contract {
		return Frame{}, ErrProtocol
	}
	return f, nil
}

func uniqueValue(d *json.Decoder, depth int) bool {
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
			key, e := d.Token()
			s, ok := key.(string)
			if e != nil || !ok || seen[s] {
				return false
			}
			seen[s] = true
			if !uniqueValue(d, depth+1) {
				return false
			}
		}
		last, e := d.Token()
		return e == nil && last == json.Delim('}')
	case '[':
		for d.More() {
			if !uniqueValue(d, depth+1) {
				return false
			}
		}
		last, e := d.Token()
		return e == nil && last == json.Delim(']')
	}
	return false
}

// PipeConn limits raw traffic (including headers/control frames) as well as
// decoded bodies. The host resets a finite budget before each unary exchange.
// Exceeding it tears down the actual process through Close, including idle spam.
type PipeConn struct {
	Reader    io.ReadCloser
	Writer    io.WriteCloser
	OnClose   func()
	remaining atomic.Int64
	closed    atomic.Bool
}

func NewPipe(r io.ReadCloser, w io.WriteCloser, close func()) *PipeConn {
	p := &PipeConn{Reader: r, Writer: w, OnClose: close}
	p.Reset()
	return p
}
func (p *PipeConn) Reset() { p.remaining.Store(256 << 10) }
func (p *PipeConn) Read(b []byte) (int, error) {
	left := p.remaining.Load()
	if left <= 0 {
		p.Close()
		return 0, ErrProtocol
	}
	if int64(len(b)) > left {
		b = b[:left]
	}
	n, e := p.Reader.Read(b)
	p.remaining.Add(-int64(n))
	return n, e
}
func (p *PipeConn) Write(b []byte) (int, error) { return p.Writer.Write(b) }
func (p *PipeConn) Close() error {
	if p.closed.CompareAndSwap(false, true) {
		p.Reader.Close()
		p.Writer.Close()
		if p.OnClose != nil {
			p.OnClose()
		}
	}
	return nil
}

type pipeAddress struct{}

func (pipeAddress) Network() string      { return "pipe" }
func (pipeAddress) String() string       { return "container" }
func (p *PipeConn) LocalAddr() net.Addr  { return pipeAddress{} }
func (p *PipeConn) RemoteAddr() net.Addr { return pipeAddress{} }

// Deadlines are enforced by the caller cancelling the owned runtime; pipe IO
// has no socket deadline and must never survive the parent call's cancellation.
func (p *PipeConn) SetDeadline(time.Time) error      { return nil }
func (p *PipeConn) SetReadDeadline(time.Time) error  { return nil }
func (p *PipeConn) SetWriteDeadline(time.Time) error { return nil }

type Client struct {
	conn      *PipeConn
	transport http.RoundTripper
	grpc      bool
}

func NewClient(p *PipeConn, transport string) (*Client, error) {
	c := &Client{conn: p, grpc: transport == "grpc"}
	switch transport {
	case "http":
		var used atomic.Bool
		c.transport = &http.Transport{Proxy: nil, DisableCompression: true, MaxResponseHeaderBytes: 8192, MaxConnsPerHost: 1, DialContext: func(context.Context, string, string) (net.Conn, error) {
			if !used.CompareAndSwap(false, true) {
				return nil, ErrProtocol
			}
			return p, nil
		}}
	case "grpc":
		t := &http2.Transport{AllowHTTP: true, DisableCompression: true, MaxHeaderListSize: 8192, MaxReadFrameSize: 16384}
		cc, err := t.NewClientConn(p)
		if err != nil {
			return nil, ErrProtocol
		}
		c.transport = cc
	default:
		return nil, ErrProtocol
	}
	return c, nil
}
func (c *Client) Exchange(ctx context.Context, method string, f Frame) (Frame, error) {
	switch method {
	case "Configure", "Invoke", "Event", "Health", "HostResult":
	default:
		return Frame{}, ErrProtocol
	}
	b, err := Encode(f, c.grpc)
	if err != nil {
		return Frame{}, err
	}
	c.conn.Reset()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://container"+Service+method, bytes.NewReader(b))
	if err != nil {
		return Frame{}, ErrProtocol
	}
	content := "application/json"
	if c.grpc {
		content = "application/grpc+json"
		req.Header.Set("te", "trailers")
		req.Header.Set("grpc-timeout", "5000m")
	}
	req.Header.Set("Content-Type", content)
	resp, err := c.transport.RoundTrip(req)
	if err != nil {
		return Frame{}, ErrProtocol
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != content || resp.Header.Get("Content-Encoding") != "" {
		return Frame{}, ErrProtocol
	}
	f, err = Decode(resp.Body, c.grpc)
	if c.grpc && (resp.ProtoMajor != 2 || resp.Trailer.Get("grpc-status") != "0" || resp.Header.Get("grpc-encoding") != "") {
		return Frame{}, ErrProtocol
	}
	if err != nil {
		return Frame{}, err
	}
	return f, nil
}

// Handler is used by the owned acceptance fixture. It is deliberately outside
// the public SDK (08a). No credentials or host authority are available here.
func Handler(transport string, fn func(string, Frame) (Frame, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grpc := transport == "grpc"
		content := "application/json"
		if grpc {
			content = "application/grpc+json"
		}
		method := strings.TrimPrefix(r.URL.Path, Service)
		if r.Method != "POST" || !strings.HasPrefix(r.URL.Path, Service) || r.Header.Get("Content-Type") != content {
			http.Error(w, "invalid", 400)
			return
		}
		f, err := Decode(r.Body, grpc)
		if err != nil {
			http.Error(w, "invalid", 400)
			return
		}
		out, err := fn(method, f)
		if err != nil {
			http.Error(w, "failed", 500)
			return
		}
		b, err := Encode(out, grpc)
		if err != nil {
			http.Error(w, "invalid", 500)
			return
		}
		w.Header().Set("Content-Type", content)
		if grpc {
			w.Header().Set("Trailer", "grpc-status")
		}
		w.WriteHeader(200)
		_, _ = w.Write(b)
		if grpc {
			w.Header().Set("grpc-status", "0")
		}
	})
}
