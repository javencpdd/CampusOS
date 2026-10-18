package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func v5EventSource(t *testing.T, v v5TestPlatform, kind, aggregate, id string) reliability.Event {
	t.Helper()
	e, err := reliability.NewEvent(kind, aggregate, id, map[string]string{"body": "PRIVATE-BODY-MARKER", "secret": "PRIVATE-SECRET-MARKER", "prompt": "PRIVATE-PROMPT-MARKER"})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.s.reliable.Execute(context.Background(), reliability.Command{Code: "owned.event-source", ActorType: "system", ActorID: "owned-target", ResourceType: aggregate, ResourceID: id, Event: &e}, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	return e
}
func v5LifecycleOp(t *testing.T, v v5TestPlatform) V5Operation {
	t.Helper()
	proof, _ := v.admin(context.Background())
	op, err := v.store.operation(context.Background(), "admin", proof.Actor.ID, "enable")
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func TestV5MetadataCommittedSafeIndependentDelivery(t *testing.T) {
	ctx := context.Background()
	v, h, lease := newV5AuthorizedManagedFixture(t)
	queue := NewMemoryV5EventStore()
	service := NewV5EventService(h, queue, nil)
	seen := 0
	h.instances[lease.ID].adapter = v5ManagedTestAdapter{event: func(_ context.Context, raw json.RawMessage) error {
		seen++
		if bytes.Contains(raw, []byte("PRIVATE")) {
			t.Fatal("private source copied")
		}
		var e map[string]any
		if json.Unmarshal(raw, &e) != nil || len(e) != 7 || len(e["payload"].(map[string]any)) != 2 {
			t.Fatal("not exact safe event")
		}
		return nil
	}}
	source := v5EventSource(t, v, "plugin.lifecycle.changed", "plugin_v5_operation", v5LifecycleOp(t, v).ID)
	if err := v.s.reliable.CheckedRead(ctx, func(tx context.Context) error { return service.CaptureCommitted(tx, source.ID) }); err == nil || err.Error() != "plugin.event_uncommitted" {
		t.Fatal("pre-commit boolean accepted", err)
	}
	if err := service.CaptureCommitted(ctx, "missing"); err == nil {
		t.Fatal("missing commit receipt")
	}
	for n := 0; n < 2; n++ {
		if err := service.CaptureCommitted(ctx, source.ID); err != nil {
			t.Fatal(err)
		}
	}
	if seen != 0 || len(queue.rows) != 1 {
		t.Fatal("core consumer invoked plugin or lost idempotency", seen, len(queue.rows))
	}
	for _, d := range queue.rows {
		raw, _ := json.Marshal(d)
		if bytes.Contains(raw, []byte("PRIVATE")) {
			t.Fatal("queue retained private body")
		}
	}
	if _, err := service.ProcessOnce(ctx, 16); err != nil {
		t.Fatal(err)
	}
	if seen != 1 {
		t.Fatal(seen)
	}
	if _, err := service.ProcessOnce(ctx, 16); err != nil || seen != 1 {
		t.Fatal("ack repeated", err, seen)
	}
}

func TestV5MetadataRetriesDeadLettersRevocationAndLeases(t *testing.T) {
	for _, action := range []string{"retry", "dead", "revoke", "source-private"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			v, h, lease := newV5AuthorizedManagedFixture(t)
			queue := NewMemoryV5EventStore()
			public := true
			service := NewV5EventService(h, queue, func(context.Context, string, string) (bool, error) { return public, nil })
			now := time.Now()
			service.now = func() time.Time { return now }
			attempts := 0
			h.instances[lease.ID].adapter = v5ManagedTestAdapter{event: func(context.Context, json.RawMessage) error {
				attempts++
				if action == "dead" || attempts < 3 {
					return errors.New("PRIVATE-transport-error")
				}
				return nil
			}}
			source := v5EventSource(t, v, "thread.updated", "thread", "1")
			now = time.Now()
			if err := service.CaptureCommitted(ctx, source.ID); err != nil {
				t.Fatal(err)
			}
			if action == "revoke" {
				g, _ := v.s.grant(ctx, v.c)
				v.mutate(t, V5Mutation{Kind: "revoke", ExpectedRevision: g.Revision, IdempotencyKey: "revoke-events"}, true, v.admin)
			}
			if action == "source-private" {
				public = false
			}
			for n := 0; n < 6; n++ {
				now = now.Add(10 * time.Second)
				if _, err := service.ProcessOnce(ctx, 16); err != nil {
					t.Fatal(err)
				}
			}
			for _, d := range queue.rows {
				if strings.Contains(d.Error, "PRIVATE") {
					t.Fatal("internal transport error persisted")
				}
				want := "done"
				if action != "retry" {
					want = "dead"
				}
				if d.Status != want {
					t.Fatal(d.Status, want)
				}
			}
			if action == "retry" && attempts != 3 || action == "dead" && attempts != 5 || (action == "revoke" || action == "source-private") && attempts != 0 {
				t.Fatal("wrong retry/admission", attempts)
			}
		})
	}
	ctx := context.Background()
	v, h, _ := newV5AuthorizedManagedFixture(t)
	queue := NewMemoryV5EventStore()
	s := NewV5EventService(h, queue, nil)
	source := v5EventSource(t, v, "plugin.lifecycle.changed", "plugin_v5_operation", v5LifecycleOp(t, v).ID)
	if err := s.CaptureCommitted(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	first, err := queue.Claim(ctx, now, 1)
	if err != nil || len(first) != 1 {
		t.Fatal(err)
	}
	second, err := queue.Claim(ctx, now+31000, 1)
	if err != nil || len(second) != 1 || second[0].Lease <= first[0].Lease {
		t.Fatal("lease epoch reused", err)
	}
	if err := queue.Finish(ctx, first[0], now+31000, "done", "", 0); err != ErrV5Conflict {
		t.Fatal("stale delivery ack accepted", err)
	}
}

func TestV5MetadataPrivateLifecycleOwnerFence(t *testing.T) {
	ctx := context.Background()
	v, h, global := newV5AuthorizedManagedFixture(t)
	v.s.Users = &v5TestUsers{active: true}
	owners := map[string]string{global.ID: ""}
	var ownOperation V5Operation
	for _, owner := range []string{"1001", "1002"} {
		proof := v5TestProof("user", owner)
		op := v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "install-" + owner}, false, proof)
		ownOperation = v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: op.Generation, IdempotencyKey: "enable-" + owner}, false, proof)
		v.ready(t, ownOperation)
		i, _ := v.s.installation(ctx, v.c, owner)
		lease, err := h.Publish(ctx, v.c, owner, i.ActiveGeneration, time.Minute, v5ManagedTestAdapter{})
		if err != nil {
			t.Fatal(err)
		}
		owners[lease.ID] = owner
	}
	queue := NewMemoryV5EventStore()
	events := NewV5EventService(h, queue, nil)
	source := v5EventSource(t, v, "plugin.lifecycle.changed", "plugin_v5_operation", ownOperation.ID)
	if err := events.CaptureCommitted(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	if len(queue.rows) != 1 {
		t.Fatal("private lifecycle fanout escaped owner", len(queue.rows))
	}
	for _, d := range queue.rows {
		if d.Owner != "1002" || owners[d.Instance] != "1002" {
			t.Fatal("private operation revealed cross owner", d.Owner)
		}
	}
}

func TestV5MetadataRegisteredPostCommitConsumer(t *testing.T) {
	ctx := context.Background()
	v, h, lease := newV5AuthorizedManagedFixture(t)
	queue := NewMemoryV5EventStore()
	events := NewV5EventService(h, queue, nil)
	calls := 0
	h.instances[lease.ID].adapter = v5ManagedTestAdapter{event: func(context.Context, json.RawMessage) error { calls++; return nil }}
	events.Register()
	source := v5EventSource(t, v, "plugin.lifecycle.changed", "plugin_v5_operation", v5LifecycleOp(t, v).ID)
	if n, err := v.s.reliable.ProcessOnce(ctx); err != nil || n < 1 {
		t.Fatal("registered Core consumer failed", n, err)
	}
	if calls != 0 {
		t.Fatal("Core Outbox worker ran plugin callback")
	}
	found := false
	for _, d := range queue.rows {
		if d.Source == source.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("registered consumer omitted committed source")
	}
	if _, err := events.ProcessOnce(ctx, 16); err != nil || calls < 1 {
		t.Fatal("independent worker failed", calls, err)
	}
}

type v5MetadataAuditBarrier struct {
	reliability.Store
	entered chan struct{}
	release chan struct{}
}

func (f v5MetadataAuditBarrier) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	if a.CommandCode == "plugin.v5.metadata.capture" {
		close(f.entered)
		select {
		case <-f.release:
		case <-ctx.Done():
		}
		return errors.New("owned required audit failure")
	}
	return f.Store.RecordCommandAudit(ctx, a)
}

func TestV5MetadataMemoryRollbackAndUncommittedClaim(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v, h, lease := newV5AuthorizedManagedFixture(t)
	source := v5EventSource(t, v, "plugin.lifecycle.changed", "plugin_v5_operation", v5LifecycleOp(t, v).ID)
	original := v.fault.Store.(*reliability.MemoryStore)
	barrier := v5MetadataAuditBarrier{Store: original, entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	defer once.Do(func() { close(barrier.release) })
	v.s.reliable = reliability.NewService(transaction.NewMemory(v.store, original), barrier)
	queue := NewMemoryV5EventStore()
	events := NewV5EventService(h, queue, nil)
	var calls atomic.Int32
	h.instances[lease.ID].adapter = v5ManagedTestAdapter{event: func(context.Context, json.RawMessage) error { calls.Add(1); return nil }}
	capture := make(chan error, 1)
	go func() { capture <- events.CaptureCommitted(ctx, source.ID) }()
	select {
	case <-barrier.entered:
	case <-ctx.Done():
		t.Fatal("audit barrier not reached")
	}
	finished := make(chan error, 1)
	go func() { _, err := events.ProcessOnce(ctx, 16); finished <- err }()
	select {
	case err := <-finished:
		t.Fatal("worker observed uncommitted enqueue", err)
	case <-time.After(20 * time.Millisecond):
	}
	queue.mu.Lock()
	for _, d := range queue.rows {
		if d.Attempts != 0 {
			t.Error("claimed before capture commit")
		}
	}
	queue.mu.Unlock()
	once.Do(func() { close(barrier.release) })
	if err := <-capture; err == nil {
		t.Fatal("required audit failure ignored")
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if len(queue.rows) != 0 || calls.Load() != 0 {
		t.Fatal("rolled-back queue escaped or plugin ran", len(queue.rows), calls.Load())
	}
	if err := v.s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		_, err := events.ProcessOnce(tx, 1)
		if err == nil {
			return errors.New("callback worker accepted Core transaction")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
