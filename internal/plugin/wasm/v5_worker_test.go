package wasm

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// Exercise the real worker protocol and wazero kernel independently of the
// Docker launcher. This does not establish operating-system isolation.
func TestV5WorkerProtocolRoundTrip(t *testing.T) {
	input, parentWrite := io.Pipe()
	parentRead, output := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer input.Close()
	defer parentWrite.Close()
	defer parentRead.Close()
	defer output.Close()
	// Closing both pipes also bounds the synchronous IPC if a test fails early.
	stop := context.AfterFunc(ctx, func() { input.Close(); parentRead.Close(); output.Close(); parentWrite.Close() })
	defer stop()
	done := make(chan error, 1)
	go func() { done <- RunV5Worker(ctx, input, output); output.Close() }()
	init := v5WorkerFrame{Kind: "init", Code: v5Module(v5WasmFixture{}), Limits: V5SandboxLimits{1, 1, 500}, ExpiresAtMS: time.Now().Add(time.Minute).UnixMilli()}
	if err := writeV5Frame(parentWrite, init, v5InitLimit); err != nil {
		t.Fatal(err)
	}
	ready, err := readV5Frame(parentRead, v5FrameLimit)
	if err != nil || ready.Kind != "ready" {
		t.Fatal(ready.Kind, err)
	}
	payload := json.RawMessage(`{"worker_protocol":true}`)
	if err := writeV5Frame(parentWrite, v5WorkerFrame{Kind: "invoke", Payload: payload, DeadlineMS: time.Now().Add(time.Second).UnixMilli()}, v5FrameLimit); err != nil {
		t.Fatal(err)
	}
	result, err := readV5Frame(parentRead, v5FrameLimit)
	if err != nil || result.Kind != "result" || result.Failed || string(result.Payload) != string(payload) {
		t.Fatal(result, err)
	}
	parentWrite.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("worker did not stop on EOF")
	}
}

func TestV5WorkerPartialInitParentLoss(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	input, writer := io.Pipe()
	defer writer.Close()
	done := make(chan error, 1)
	go func() { done <- RunV5Worker(ctx, input, io.Discard) }()
	// A valid header followed by a missing body must not outlive the parent.
	if _, err := writer.Write([]byte{0, 0, 0, 32}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("partial init accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("partial init orphan")
	}
}
