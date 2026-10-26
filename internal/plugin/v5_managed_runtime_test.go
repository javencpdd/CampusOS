package plugin

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

type v5ManagedTestAdapter struct {
	invoke func(context.Context, V5ChatTarget, json.RawMessage, io.Writer) error
	event  func(context.Context, json.RawMessage) error
}

func (a v5ManagedTestAdapter) Invoke(ctx context.Context, t V5ChatTarget, r json.RawMessage, w io.Writer) error {
	if a.invoke == nil {
		return ErrV5RuntimeMissing
	}
	return a.invoke(ctx, t, r, w)
}
func (a v5ManagedTestAdapter) Event(ctx context.Context, r json.RawMessage) error {
	if a.event == nil {
		return ErrV5RuntimeMissing
	}
	return a.event(ctx, r)
}

func TestV5ManagedPublicationWithdrawalAndRestart(t *testing.T) {
	ctx := context.Background()
	v, _ := newV5ContributionTestPlatform(t)
	i := v5ContributionApproved(t, v)
	h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
	if _, err := h.Publish(ctx, v.c, "", i.Generation, time.Minute, v5ManagedTestAdapter{}); err != ErrV5ContributionStale {
		t.Fatal("unready publication", err)
	}
	v.ready(t, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin))
	i, _ = v.s.installation(ctx, v.c, "")
	lease, err := h.Publish(ctx, v.c, "", i.Generation, time.Minute, v5ManagedTestAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Publish(ctx, v.c, "", i.Generation, time.Minute, v5ManagedTestAdapter{}); err != ErrV5ContributionConflict {
		t.Fatal("duplicate replaced runtime", err)
	}
	b, err := h.Select(ctx, "", v5ContributionQueries()[1], nil)
	if err != nil || b.Descriptor().InstanceGeneration != i.Generation {
		t.Fatal(err)
	}
	if _, err := h.credential(ctx, lease.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := h.credential(ctx, "invalid"); err == nil {
		t.Fatal("forged credential")
	}
	v.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: i.Generation, IdempotencyKey: "disable"}, true, v.admin)
	if _, err := h.Select(ctx, "", v5ContributionQueries()[1], nil); err == nil {
		t.Fatal("publication survived stop")
	}
	if _, err := h.credential(ctx, lease.Token); err == nil {
		t.Fatal("credential survived withdrawal")
	}
	if err := h.Withdraw(ctx, lease.ID, false); err != nil {
		t.Fatal(err)
	}
	restarted := NewV5Platform(v.store, v.s.files, v.s.catalog, v.s.reliable, v.s.admins)
	other := NewV5ManagedRuntime(NewV5ContributionDirectory(restarted))
	if _, err := other.credential(ctx, lease.Token); err == nil {
		t.Fatal("restart revived credential")
	}
}

func TestV5ManagedConflictSelectionAndCredentialExpiry(t *testing.T) {
	ctx := context.Background()
	v, priv := newV5ContributionTestPlatform(t)
	i := v5ContributionApproved(t, v)
	v.ready(t, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin))
	h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
	i, _ = v.s.installation(ctx, v.c, "")
	lease, err := h.Publish(ctx, v.c, "", i.ActiveGeneration, time.Minute, v5ManagedTestAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	auto, err := h.Select(ctx, "", v5ContributionQueries()[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	ref := auto.Descriptor().V5ContributionRef
	explicit, err := h.Select(ctx, "", v5ContributionQueries()[1], &ref)
	if err != nil {
		t.Fatal(err)
	}
	second := v5ContributionRelease(t, v, priv, "second-managed")
	i = v5ContributionApproved(t, second)
	second.ready(t, second.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, second.admin))
	i, _ = second.s.installation(ctx, second.c, "")
	if _, err := h.Publish(ctx, second.c, "", i.ActiveGeneration, time.Minute, v5ManagedTestAdapter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Select(ctx, "", v5ContributionQueries()[1], nil); err != ErrV5ContributionConflict {
		t.Fatal("new provider silently took over", err)
	}
	if _, err := h.checkBinding(ctx, auto); err != ErrV5ContributionConflict {
		t.Fatal("old automatic binding ignored conflict", err)
	}
	if _, err := h.checkBinding(ctx, explicit); err != nil {
		t.Fatal("explicit binding switched", err)
	}
	h.mu.Lock()
	h.instances[lease.ID].expires = time.Now().Add(-time.Second)
	h.mu.Unlock()
	if _, err := h.credential(ctx, lease.Token); err == nil {
		t.Fatal("expired credential accepted")
	}
	if _, err := h.checkBinding(ctx, explicit); err == nil {
		t.Fatal("expired publication remained bound")
	}
}
