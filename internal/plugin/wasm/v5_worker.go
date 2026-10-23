package wasm

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
)

const v5FrameLimit = 256 << 10
const v5InitLimit = 48 << 20

type v5WorkerFrame struct {
	Kind          string            `json:"kind"`
	Code          []byte            `json:"code,omitempty"`
	Limits        V5SandboxLimits   `json:"limits,omitempty"`
	ExpiresAtMS   int64             `json:"expires_at_ms,omitempty"`
	DeadlineMS    int64             `json:"deadline_ms,omitempty"`
	Declared      []string          `json:"declared,omitempty"`
	EventRequired bool              `json:"event_required,omitempty"`
	Capability    string            `json:"capability,omitempty"`
	Payload       json.RawMessage   `json:"payload,omitempty"`
	Failed        bool              `json:"failed,omitempty"`
	Controls      map[string]string `json:"controls,omitempty"`
	Configuration json.RawMessage   `json:"configuration,omitempty"`
}

func writeV5Frame(w io.Writer, frame v5WorkerFrame, max int) error {
	raw, err := json.Marshal(frame)
	if err != nil || len(raw) > max {
		return ErrV5SandboxBudget
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(raw)))
	if _, err := io.Copy(w, bytes.NewReader(append(size[:], raw...))); err != nil {
		return err
	}
	return nil
}
func readV5Frame(r io.Reader, max int) (v5WorkerFrame, error) {
	var out v5WorkerFrame
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return out, err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n == 0 || uint64(n) > uint64(max) {
		return out, ErrV5SandboxBudget
	}
	raw := make([]byte, int(n))
	if _, err := io.ReadFull(r, raw); err != nil {
		return out, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&out); err != nil {
		return out, ErrV5SandboxABI
	}
	if d.Decode(new(any)) != io.EOF {
		return out, ErrV5SandboxABI
	}
	return out, nil
}

// RunV5Worker is a private stdio protocol for the fixed host-owned executable.
// Parent authentication, current authority and native OS limits are mandatory
// in the launcher. There is no listener, shell, arbitrary binary or HTTP mode.
func RunV5Worker(ctx context.Context, input io.Reader, output io.Writer) error {
	// Bound even a partial init frame after a parent crash. Docker attach can
	// keep stdin open; EOF alone is insufficient to reap that orphan.
	initCtx, initCancel := context.WithTimeout(ctx, 5*time.Second)
	stopInit := context.AfterFunc(initCtx, func() {
		if c, ok := input.(io.Closer); ok {
			_ = c.Close()
		}
	})
	defer initCancel()
	defer stopInit()
	init, err := readV5Frame(input, v5InitLimit)
	if err != nil || init.Kind != "init" || len(init.Declared) > 64 {
		return ErrV5SandboxABI
	}
	declared := map[string]bool{}
	for _, code := range init.Declared {
		declared[code] = true
	}
	host := func(ctx context.Context, code string, raw json.RawMessage) (json.RawMessage, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err := writeV5Frame(output, v5WorkerFrame{Kind: "host", Capability: code, Payload: raw}, v5FrameLimit); err != nil {
			return nil, err
		}
		reply, err := readV5Frame(input, v5FrameLimit)
		if err != nil || reply.Kind != "host_result" || reply.Failed || ctx.Err() != nil {
			return nil, ErrV5SandboxCall
		}
		return reply.Payload, nil
	}
	s, err := newV5Sandbox(ctx, init.Code, init.Limits, time.UnixMilli(init.ExpiresAtMS), declared, host)
	if err != nil {
		return err
	}
	defer s.Close(context.Background())
	if len(init.Configuration) > V5SandboxMaxJSONBytes || len(init.Configuration) > 0 && !json.Valid(init.Configuration) {
		return ErrV5SandboxABI
	}
	s.configuration = bytes.Clone(init.Configuration)
	if init.EventRequired && !s.SupportsEvents() {
		return ErrV5SandboxABI
	}
	if err := s.Check(ctx); err != nil {
		return err
	}
	controls := map[string]string{}
	for _, name := range []string{"memory.max", "memory.swap.max", "cpu.max", "pids.max"} {
		raw, _ := os.ReadFile("/sys/fs/cgroup/" + name)
		controls[name] = strings.TrimSpace(string(raw))
	}
	if err := writeV5Frame(output, v5WorkerFrame{Kind: "ready", Controls: controls}, v5FrameLimit); err != nil {
		return err
	}
	stopInit()
	initCancel()
	for {
		idleCtx, idleCancel := context.WithTimeout(ctx, 30*time.Second)
		stopIdle := context.AfterFunc(idleCtx, func() {
			if c, ok := input.(io.Closer); ok {
				_ = c.Close()
			}
		})
		request, err := readV5Frame(input, v5FrameLimit)
		stopIdle()
		idleCancel()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil || request.DeadlineMS < 1 {
			return ErrV5SandboxABI
		}
		callCtx, cancel := context.WithDeadline(ctx, time.UnixMilli(request.DeadlineMS))
		stopRead := context.AfterFunc(callCtx, func() {
			if c, ok := input.(io.Closer); ok {
				_ = c.Close()
			}
		})
		var out []byte
		switch request.Kind {
		case "ping":
			if time.Now().UnixMilli() >= init.ExpiresAtMS {
				err = ErrV5SandboxClosed
			}
		case "invoke":
			out, err = s.Invoke(callCtx, request.Payload, V5SandboxMaxJSONBytes)
		case "event":
			err = s.Event(callCtx, request.Payload)
		default:
			stopRead()
			cancel()
			return ErrV5SandboxABI
		}
		stopRead()
		cancel()
		if err := writeV5Frame(output, v5WorkerFrame{Kind: "result", Payload: out, Failed: err != nil}, v5FrameLimit); err != nil {
			return err
		}
	}
}

var _ plugin.V5RuntimeFactory = (*V5IsolatedFactory)(nil)
