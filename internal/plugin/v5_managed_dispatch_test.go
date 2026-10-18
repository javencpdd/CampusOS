package plugin

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
)

func v5ManagedPayload(b V5ManagedBinding) []byte {
	d := b.Descriptor()
	m := map[string]any{"request_id": "managed-1", "contribution_id": d.ID, "instance_generation": d.InstanceGeneration, "deadline_ms": 1000}
	switch d.Point {
	case V5PreviewRenderer:
		m["resource_ref"] = "object:owned"
		m["mime_type"] = "application/pdf"
	case V5AIChat:
		m["prompt"] = "explicit prompt"
		m["context_class"] = "none"
	case V5KnowledgeSource:
		m["binding_id"] = "public-source"
		m["limit"] = 1
	}
	return mustV5JSON(m)
}
func v5ManagedEnvelope(b V5ManagedBinding, payload []byte) []byte {
	return mustV5JSON(campusossdk.V5DispatchCall{RequestID: "managed-1", Contract: "campusos.plugin-dispatch/v1", Target: campusossdk.V5DispatchTarget{Kind: "contribution", ID: b.Descriptor().ID}, InstanceGeneration: b.Descriptor().InstanceGeneration, DeadlineMS: time.Now().Add(900 * time.Millisecond).UnixMilli(), RequestBytes: int64(len(payload)), ResponseLimitBytes: 4096})
}

func v5ManagedReply(_ context.Context, target V5ChatTarget, raw json.RawMessage, w io.Writer) error {
	request, err := parseV5Extension(target.Contribution.Point, raw, false)
	if err != nil {
		return err
	}
	r := map[string]any{"request_id": request["request_id"], "contribution_id": request["contribution_id"], "instance_generation": request["instance_generation"]}
	switch target.Contribution.Point {
	case V5PreviewRenderer:
		r["ui_slot_id"] = "viewer"
		r["resource_revision"] = "rev42"
	case V5AIChat:
		r["text"] = "managed reply"
		r["input_tokens"] = 1
		r["output_tokens"] = 1
		r["finish_reason"] = "completed"
	case V5KnowledgeSource:
		r["items"] = []any{map[string]any{"external_id": "public1", "revision": "rev42", "title": "public", "text": "public text"}}
	}
	return json.NewEncoder(w).Encode(r)
}

func TestV5ManagedThreeTypedContributions(t *testing.T) {
	v, _, _, proof, features := newV5ChatTestDispatcher(t, v5ChatTestTransport(v5ChatReply))
	ctx := context.Background()
	h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
	i, _ := v.s.installation(ctx, v.c, "1001")
	lease, err := h.Publish(ctx, v.c, "1001", i.Generation, time.Minute, v5ManagedTestAdapter{invoke: v5ManagedReply})
	if err != nil {
		t.Fatal(err)
	}
	consumer := V5Consumer{Feature: features, FeatureID: "contract-chat-consumer", Authorize: func(context.Context, identity.PrincipalContext, V5ContributionDescriptor, json.RawMessage) (string, error) {
		return "rev42", nil
	}}
	for _, q := range v5ContributionQueries() {
		t.Run(string(q.Point), func(t *testing.T) {
			b, err := h.Select(ctx, "1001", q, nil)
			if err != nil {
				t.Fatal(err)
			}
			raw := v5ManagedPayload(b)
			if _, err := h.Invoke(ctx, consumer, b, v5ManagedEnvelope(b, raw), raw, proof); err != nil {
				t.Fatal(err)
			}
			if q.Point != V5AIChat {
				unbound := consumer
				unbound.Authorize = nil
				if _, err := h.Invoke(ctx, unbound, b, v5ManagedEnvelope(b, raw), raw, proof); err != ErrV5Denied {
					t.Fatal("missing resource policy", err)
				}
			}
			if _, err := h.Invoke(ctx, consumer, b, v5ManagedEnvelope(b, raw), raw, v5TestProof("user", "1002")); err != ErrV5Denied {
				t.Fatal("cross-owner", err)
			}
		})
	}
	if err := h.Withdraw(ctx, lease.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.credential(ctx, lease.Token); err == nil {
		t.Fatal("drained credential remained live")
	}
}

func TestV5ManagedStopCancelsAndClosesAdmission(t *testing.T) {
	v, _, _, proof, features := newV5ChatTestDispatcher(t, v5ChatTestTransport(v5ChatReply))
	ctx := context.Background()
	h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
	i, _ := v.s.installation(ctx, v.c, "1001")
	started := make(chan struct{})
	cancelled := make(chan struct{})
	done := make(chan error, 1)
	adapter := v5ManagedTestAdapter{invoke: func(ctx context.Context, _ V5ChatTarget, _ json.RawMessage, _ io.Writer) error {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return ctx.Err()
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
	go func() {
		out, err := h.Invoke(ctx, V5Consumer{Feature: features, FeatureID: "contract-chat-consumer"}, b, v5ManagedEnvelope(b, raw), raw, proof)
		if len(out) != 0 {
			done <- ErrV5CallInvalid
		} else {
			done <- err
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("call not started")
	}
	v.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: i.Generation, IdempotencyKey: "stop-user"}, false, proof)
	if _, err := h.credential(ctx, lease.Token); err == nil {
		t.Fatal("stop did not fence credential")
	}
	select {
	case <-cancelled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("stop did not cancel promptly")
	}
	if err := <-done; err == nil {
		t.Fatal("cancelled output released")
	}
	if _, err := h.Invoke(ctx, V5Consumer{Feature: features, FeatureID: "contract-chat-consumer"}, b, v5ManagedEnvelope(b, raw), raw, proof); err == nil {
		t.Fatal("new call after withdrawal")
	}
}
