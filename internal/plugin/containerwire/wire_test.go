package containerwire

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

type oneListener struct {
	c    net.Conn
	used atomic.Bool
}

func (l *oneListener) Accept() (net.Conn, error) {
	if l.used.CompareAndSwap(false, true) {
		return l.c, nil
	}
	return nil, net.ErrClosed
}
func (l *oneListener) Close() error   { return nil }
func (l *oneListener) Addr() net.Addr { return pipeAddress{} }

func TestV5ContainerWireTransports(t *testing.T) {
	for _, transport := range []string{"http", "grpc"} {
		t.Run(transport, func(t *testing.T) {
			a, b := net.Pipe()
			t.Cleanup(func() { a.Close(); b.Close() })
			h := Handler(transport, func(method string, f Frame) (Frame, error) {
				if method != "Configure" || f.Kind != "configure" {
					t.Error("wrong method")
				}
				return Frame{Kind: "ready"}, nil
			})
			if transport == "grpc" {
				go (&http2.Server{MaxConcurrentStreams: 1, MaxReadFrameSize: 16384}).ServeConn(b, &http2.ServeConnOpts{Handler: h})
			} else {
				go (&http.Server{Handler: h}).Serve(&oneListener{c: b})
			}
			p := NewPipe(a, a, func() {})
			c, err := NewClient(p, transport)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			for i := 0; i < 3; i++ {
				f, err := c.Exchange(ctx, "Configure", Frame{Kind: "configure", Payload: []byte(`{}`)})
				if err != nil || f.Kind != "ready" {
					t.Fatal(f, err)
				}
			}
			if _, err := c.Exchange(ctx, "Arbitrary", Frame{}); err == nil {
				t.Fatal("unpublished method accepted")
			}
		})
	}
}
func TestV5ContainerWireRejectsMalformedAndUnbounded(t *testing.T) {
	for _, raw := range []string{`{}`, `{"contract":"wrong","kind":"ready"}`, `{"contract":"` + Contract + `","extra":true}`, `{"contract":"` + Contract + `","kind":"host","kind":"result"}`, `{"contract":"` + Contract + `","payload":{"x":1,"x":2}}`, `{"contract":"` + Contract + `"} {}`, strings.Repeat("x", MaxMessage+1)} {
		if _, err := Decode(strings.NewReader(raw), false); err == nil {
			t.Fatal("malformed frame admitted")
		}
	}
	b, _ := Encode(Frame{Kind: "ready"}, true)
	for _, raw := range [][]byte{b[:4], append(append([]byte{}, b...), b...), append([]byte{1}, b[1:]...)} {
		if _, err := Decode(strings.NewReader(string(raw)), true); err == nil {
			t.Fatal("invalid gRPC framing admitted")
		}
	}
	if _, err := Encode(Frame{Payload: []byte(`"` + strings.Repeat("x", MaxMessage) + `"`)}, false); err == nil {
		t.Fatal("unbounded output")
	}
	closed := false
	p := NewPipe(io.NopCloser(strings.NewReader(strings.Repeat("x", 300<<10))), nopWriter{}, func() { closed = true })
	if n, err := io.Copy(io.Discard, p); err == nil || n != 256<<10 || !closed {
		t.Fatal("raw traffic limit bypass", n, err, closed)
	}
}

type nopWriter struct{}

func (nopWriter) Write(b []byte) (int, error) { return len(b), nil }
func (nopWriter) Close() error                { return nil }
