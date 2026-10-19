// Owned acceptance guest. This is never registered as a built-in provider or
// used as a trusted isolation probe for arbitrary third-party images.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/plugin/containerwire"
	"golang.org/x/net/http2"
)

type guest struct {
	request       map[string]any
	configuration string
	loop          bool
}

func (g *guest) reply(text string) containerwire.Frame {
	out := map[string]any{"request_id": g.request["request_id"], "contribution_id": g.request["contribution_id"], "instance_generation": g.request["instance_generation"], "text": text, "input_tokens": 1, "output_tokens": 1, "finish_reason": "completed"}
	b, _ := json.Marshal(out)
	return containerwire.Frame{Kind: "result", Payload: b}
}
func probe() string {
	proof := map[string]any{"uid": os.Getuid(), "configuration": true}
	status, _ := os.ReadFile("/proc/self/status")
	flags := map[string]string{}
	for _, line := range strings.Split(string(status), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && (k == "Seccomp" || k == "NoNewPrivs" || k == "CapEff") {
			flags[k] = strings.TrimSpace(v)
		}
	}
	proof["process_flags"] = flags
	controls := map[string]string{}
	for _, key := range []string{"memory.max", "memory.swap.max", "cpu.max", "cpu.stat", "pids.max"} {
		b, _ := os.ReadFile("/sys/fs/cgroup/" + key)
		controls[key] = strings.TrimSpace(string(b))
	}
	proof["cgroup"] = controls
	_, socket := os.Stat("/var/run/docker.sock")
	proof["socket_absent"] = os.IsNotExist(socket)
	proof["root_readonly"] = os.WriteFile("/forbidden", []byte("x"), 0600) != nil
	_, previous := os.Stat("/tmp/private-marker")
	proof["fresh_directory"] = os.IsNotExist(previous)
	proof["tmp_writable"] = os.WriteFile("/tmp/private-marker", []byte("owned"), 0600) == nil
	proof["tmp_quota"] = os.WriteFile("/tmp/overflow", make([]byte, 2<<20), 0600) != nil
	os.Remove("/tmp/overflow")
	keys := []string{}
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		keys = append(keys, key)
	}
	proof["environment_keys"] = keys
	interfaces, _ := net.Interfaces()
	names := []string{}
	for _, i := range interfaces {
		names = append(names, i.Name)
	}
	proof["interfaces"] = names
	conn, err := net.DialTimeout("tcp", "127.0.0.1:5432", 50*time.Millisecond)
	proof["database_unreachable"] = err != nil
	if conn != nil {
		conn.Close()
	}
	b, _ := json.Marshal(proof)
	return string(b)
}
func (g *guest) handle(method string, f containerwire.Frame) (containerwire.Frame, error) {
	switch method {
	case "Configure":
		if f.Kind != "configure" || !json.Valid(f.Payload) {
			return containerwire.Frame{}, fmt.Errorf("configuration")
		}
		g.configuration = string(f.Payload)
		return containerwire.Frame{Kind: "ready"}, nil
	case "Health":
		return containerwire.Frame{Kind: "ready"}, nil
	case "Event":
		if strings.Contains(string(f.Payload), "PRIVATE-BODY") {
			return containerwire.Frame{}, fmt.Errorf("private event")
		}
		return containerwire.Frame{Kind: "result", Payload: []byte(`{"ack":true}`)}, nil
	case "Invoke":
		if json.Unmarshal(f.Payload, &g.request) != nil {
			return containerwire.Frame{}, fmt.Errorf("payload")
		}
		prompt, _ := g.request["prompt"].(string)
		g.loop = prompt == "host-loop"
		switch prompt {
		case "probe":
			return g.reply(probe()), nil
		case "configuration":
			return g.reply(g.configuration), nil
		case "spin":
			for {
			}
		case "oom":
			var retained [][]byte
			for {
				b := make([]byte, 8<<20)
				for i := range b {
					b[i] = 42
				}
				retained = append(retained, b)
				if len(retained) > 10000 {
					panic("unreachable")
				}
			}
		case "exit":
			os.Exit(12)
		case "flood":
			os.Stdout.Write(make([]byte, 300<<10))
			select {}
		}
		capability := "community.thread.public.read"
		if strings.HasPrefix(prompt, "cap:") {
			capability = strings.TrimPrefix(prompt, "cap:")
		}
		return containerwire.Frame{Kind: "host", Capability: capability, Payload: []byte(`{"thread_id":"505003"}`)}, nil
	case "HostResult":
		if f.Failed {
			return containerwire.Frame{Kind: "result", Failed: true}, nil
		}
		if g.loop {
			return containerwire.Frame{Kind: "host", Capability: "community.thread.public.read", Payload: []byte(`{"thread_id":"505003"}`)}, nil
		}
		if strings.Contains(string(f.Payload), "PRIVATE-BODY") {
			return containerwire.Frame{}, fmt.Errorf("private resource")
		}
		return g.reply("real-container/" + os.Getenv("CAMPUSOS_CONTAINER_TRANSPORT")), nil
	}
	return containerwire.Frame{}, fmt.Errorf("method")
}

type pipeAddress struct{}

func (pipeAddress) Network() string { return "pipe" }
func (pipeAddress) String() string  { return "private" }

type stdioConn struct {
	sync.Mutex
	once sync.Once
}

func (c *stdioConn) Read(b []byte) (int, error) {
	n, e := os.Stdin.Read(b)
	if e != nil {
		os.Exit(0)
	}
	return n, e
}
func (c *stdioConn) Write(b []byte) (int, error)      { return os.Stdout.Write(b) }
func (c *stdioConn) Close() error                     { c.once.Do(func() { os.Stdin.Close() }); return nil }
func (c *stdioConn) LocalAddr() net.Addr              { return pipeAddress{} }
func (c *stdioConn) RemoteAddr() net.Addr             { return pipeAddress{} }
func (c *stdioConn) SetDeadline(time.Time) error      { return nil }
func (c *stdioConn) SetReadDeadline(time.Time) error  { return nil }
func (c *stdioConn) SetWriteDeadline(time.Time) error { return nil }

type single struct {
	conn     net.Conn
	accepted bool
}

func (l *single) Accept() (net.Conn, error) {
	if l.accepted {
		return nil, io.EOF
	}
	l.accepted = true
	return l.conn, nil
}
func (l *single) Addr() net.Addr { return pipeAddress{} }
func (l *single) Close() error   { return nil }
func main() {
	transport := os.Getenv("CAMPUSOS_CONTAINER_TRANSPORT")
	g := &guest{}
	h := containerwire.Handler(transport, g.handle)
	conn := &stdioConn{}
	if transport == "grpc" {
		(&http2.Server{MaxConcurrentStreams: 1, MaxReadFrameSize: 16384}).ServeConn(conn, &http2.ServeConnOpts{Context: context.Background(), Handler: h})
		return
	}
	s := &http.Server{Handler: h, MaxHeaderBytes: 8192}
	go s.Serve(&single{conn: conn})
	select {}
}
