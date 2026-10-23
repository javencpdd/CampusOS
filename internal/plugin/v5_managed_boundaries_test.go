package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestV5ManagedTypedResponseAndResourceFences(t *testing.T) {
	v, _, _, proof, features := newV5ChatTestDispatcher(t, v5ChatTestTransport(v5ChatReply))
	ctx := context.Background()
	h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
	i, _ := v.s.installation(ctx, v.c, "1001")
	lease, err := h.Publish(ctx, v.c, "1001", i.Generation, time.Minute, v5ManagedTestAdapter{invoke: v5ManagedReply})
	if err != nil {
		t.Fatal(err)
	}
	var revision atomic.Int64
	revision.Store(42)
	consumer := V5Consumer{Feature: features, FeatureID: "contract-chat-consumer", Authorize: func(context.Context, identity.PrincipalContext, V5ContributionDescriptor, json.RawMessage) (string, error) {
		if revision.Load() != 42 {
			return "changed", nil
		}
		return "rev42", nil
	}}
	for _, q := range v5ContributionQueries() {
		t.Run(string(q.Point), func(t *testing.T) {
			b, err := h.Select(ctx, "1001", q, nil)
			if err != nil {
				t.Fatal(err)
			}
			raw := v5ManagedPayload(b)
			unknown := map[string]any{}
			_ = json.Unmarshal(raw, &unknown)
			unknown["actor"] = "forged"
			forged := mustV5JSON(unknown)
			if out, err := h.Invoke(ctx, consumer, b, v5ManagedEnvelope(b, forged), forged, proof); err != ErrV5CallInvalid || len(out) > 0 {
				t.Fatal("unknown request accepted", err)
			}
			for _, mode := range []string{"unknown-response", "wrong-id", "oversize", "changed-resource"} {
				t.Run(mode, func(t *testing.T) {
					revision.Store(42)
					h.instances[lease.ID].adapter = v5ManagedTestAdapter{invoke: func(c context.Context, target V5ChatTarget, r json.RawMessage, w io.Writer) error {
						var buf bytes.Buffer
						if err := v5ManagedReply(c, target, r, &buf); err != nil {
							return err
						}
						var fields map[string]any
						_ = json.Unmarshal(buf.Bytes(), &fields)
						switch mode {
						case "unknown-response":
							fields["secret"] = "PRIVATE"
						case "wrong-id":
							fields["request_id"] = "other"
						case "oversize":
							_, _ = w.Write(bytes.Repeat([]byte("x"), 4097))
							return nil
						case "changed-resource":
							revision.Store(43)
						}
						_, err := w.Write(mustV5JSON(fields))
						return err
					}}
					if out, err := h.Invoke(ctx, consumer, b, v5ManagedEnvelope(b, raw), raw, proof); err == nil || len(out) > 0 {
						t.Fatal("unsafe output released", mode, err)
					}
				})
			}
			h.instances[lease.ID].adapter = v5ManagedTestAdapter{invoke: v5ManagedReply}
			revision.Store(42)
		})
	}
}

func TestV5ManagedDrainConcurrencyAndDeadline(t *testing.T) {
	for _, mode := range []string{"drain", "feature-cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			v, _, _, proof, features := newV5ChatTestDispatcher(t, v5ChatTestTransport(v5ChatReply))
			ctx := context.Background()
			h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
			i, _ := v.s.installation(ctx, v.c, "1001")
			started, release := make(chan struct{}), make(chan struct{})
			finished := make(chan error, 1)
			adapter := v5ManagedTestAdapter{invoke: func(c context.Context, target V5ChatTarget, raw json.RawMessage, w io.Writer) error {
				close(started)
				select {
				case <-release:
					return v5ManagedReply(c, target, raw, w)
				case <-c.Done():
					return c.Err()
				}
			}}
			lease, err := h.Publish(ctx, v.c, "1001", i.Generation, time.Minute, adapter)
			if err != nil {
				t.Fatal(err)
			}
			b, err := h.Select(ctx, "1001", v5ContributionQueries()[1], nil)
			if err != nil {
				t.Fatal(err)
			}
			raw := v5ManagedPayload(b)
			consumer := V5Consumer{Feature: features, FeatureID: "contract-chat-consumer"}
			go func() {
				out, err := h.Invoke(ctx, consumer, b, v5ManagedEnvelope(b, raw), raw, proof)
				if len(out) > 0 && mode != "drain" {
					err = ErrV5CallInvalid
				}
				if len(out) == 0 && mode == "drain" && err == nil {
					err = ErrV5CallInvalid
				}
				finished <- err
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("call not started")
			}
			if _, err := h.Invoke(ctx, consumer, b, v5ManagedEnvelope(b, raw), raw, proof); err != ErrV5ContributionBudget {
				t.Fatal("instance concurrency exceeded", err)
			}
			switch mode {
			case "drain":
				drainCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
				defer cancel()
				if err := h.Withdraw(drainCtx, lease.ID, false); err == nil || err.Error() != "plugin.lifecycle_drain_incomplete" {
					t.Fatal("drain prematurely succeeded", err)
				}
				if _, err := h.credential(ctx, lease.Token); err == nil {
					t.Fatal("drain did not close admission first")
				}
				select {
				case err := <-finished:
					t.Fatal("graceful drain was cancelled by guard", err)
				case <-time.After(150 * time.Millisecond):
				}
				close(release)
			case "feature-cancel":
				if _, err := features.Request("contract-chat-consumer", false); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-finished:
				if mode == "drain" && err != nil {
					t.Fatal("graceful drain discarded current authorized output", err)
				}
				if mode != "drain" && err == nil {
					t.Fatal("stale output released")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cooperative call did not cancel/drain")
			}
			if err := h.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestV5ManagedOutputCannotEscapeBudget(t *testing.T) {
	v, _, _, proof, features := newV5ChatTestDispatcher(t, v5ChatTestTransport(v5ChatReply))
	ctx := context.Background()
	h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
	i, _ := v.s.installation(ctx, v.c, "1001")
	var retained io.Writer
	_, err := h.Publish(ctx, v.c, "1001", i.Generation, time.Minute, v5ManagedTestAdapter{invoke: func(c context.Context, target V5ChatTarget, r json.RawMessage, w io.Writer) error {
		retained = w
		return v5ManagedReply(c, target, r, w)
	}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Select(ctx, "1001", v5ContributionQueries()[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := v5ManagedPayload(b)
	if _, err := h.Invoke(ctx, V5Consumer{Feature: features, FeatureID: "contract-chat-consumer"}, b, v5ManagedEnvelope(b, raw), raw, proof); err != nil {
		t.Fatal(err)
	}
	if _, err := retained.Write([]byte("late PRIVATE data")); err == nil {
		t.Fatal("late writer remained open")
	}
	var call campusossdk.V5DispatchCall
	_ = json.Unmarshal(v5ManagedEnvelope(b, raw), &call)
	call.RequestBytes++
	if _, err := h.Invoke(ctx, V5Consumer{Feature: features, FeatureID: "contract-chat-consumer"}, b, mustV5JSON(call), raw, proof); err != ErrV5ContributionBudget {
		t.Fatal("untrusted request size", err)
	}
}
