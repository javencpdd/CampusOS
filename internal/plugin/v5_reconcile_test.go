package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type v5TestUsers struct{ active bool }

func (u *v5TestUsers) GetUser(_ context.Context, id string) (identity.User, error) {
	return identity.User{ID: id, Status: map[bool]string{true: "active", false: "disabled"}[u.active]}, nil
}
func (u *v5TestUsers) GetUserByUsername(ctx context.Context, id string) (identity.User, error) {
	return u.GetUser(ctx, id)
}
func (u *v5TestUsers) GetUserForUpdate(ctx context.Context, id string) (identity.User, error) {
	return u.GetUser(ctx, id)
}
func (v v5TestPlatform) process(t *testing.T) {
	t.Helper()
	if _, err := v.s.ProcessOnce(context.Background(), 50); err != nil {
		t.Fatal(err)
	}
}
func (v v5TestPlatform) operation(t *testing.T, id string) V5Operation {
	t.Helper()
	var op V5Operation
	if err := v.store.load(context.Background(), v5Operation, id, &op); err != nil {
		t.Fatal(err)
	}
	return op
}
func (v v5TestPlatform) ready(t *testing.T, op V5Operation) {
	t.Helper()
	v.process(t)
	op = v.operation(t, op.ID)
	if op.Status != "waiting" || op.Preparation == nil {
		t.Fatal("not prepared", op)
	}
	if err := v.s.Observe(context.Background(), V5Observation{op.ID, op.Lease, op.Generation, op.Preparation.ConfigDigest, true}); err != nil {
		t.Fatal("host observation", err)
	}
}

func TestV5ReconcileLeaseRestartAndStaleObservation(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	i := v.approved(t)
	op := v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin)
	v.process(t)
	first := v.operation(t, op.ID)
	if first.Status != "waiting" {
		t.Fatal(first)
	}
	facts, err := v.s.ExecutionFacts(ctx, v.c, "")
	if err != nil || facts.ReadyForDispatch {
		t.Fatal("pending falsely ready", facts, err)
	}
	// A second host instance shares persistent facts/files but has no volatile lease.
	restarted := NewV5Platform(v.store, v.s.files, v.s.catalog, v.s.reliable, v.s.admins)
	restarted.now = func() time.Time { return time.Now().Add(time.Minute) }
	if _, err := restarted.ProcessOnce(ctx, 50); err != nil {
		t.Fatal(err)
	}
	second := v.operation(t, op.ID)
	if second.Lease <= first.Lease || second.Attempts != 2 {
		t.Fatal("lease reused", first, second)
	}
	if err := v.s.Observe(ctx, V5Observation{first.ID, first.Lease, first.Generation, first.Preparation.ConfigDigest, true}); err != ErrV5Conflict {
		t.Fatal("old lease admitted", err)
	}
	v.ready(t, second)
	facts, err = v.s.ExecutionFacts(ctx, v.c, "")
	if err != nil || !facts.ReadyForDispatch {
		t.Fatal("bounded host contract observation", facts, err)
	}
	i, _ = v.s.installation(ctx, v.c, "")
	v.mutate(t, V5Mutation{Kind: "configure", Values: json.RawMessage(`{"locale":"en-US"}`), IdempotencyKey: "config"}, true, v.admin)
	if err := v.s.Observe(ctx, V5Observation{second.ID, second.Lease, second.Generation, second.Preparation.ConfigDigest, true}); err != ErrV5Conflict {
		t.Fatal("stale generation admitted", err)
	}
	facts, err = v.s.ExecutionFacts(ctx, v.c, "")
	if err != nil || facts.ReadyForDispatch {
		t.Fatal("changed config left old instance current", facts, err)
	}
}

func TestV5ReconcileTamperFaultAndRetry(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	i := v.approved(t)
	op := v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin)
	v.process(t)
	op = v.operation(t, op.ID)
	name := filepath.Join(v.s.files.root.Name(), "prepared", v.c.Publisher, v.c.Key, "system", v5Counter(op.Generation)+".json")
	if err := os.WriteFile(name, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := v.s.Observe(ctx, V5Observation{op.ID, op.Lease, op.Generation, op.Preparation.ConfigDigest, true}); err != ErrPluginV5PackageDigest {
		t.Fatal("prepared tamper", err)
	}
	// A repair never overwrites the generation capsule; retry exposes a failure.
	v.s.now = func() time.Time { return time.Now().Add(time.Minute) }
	v.process(t)
	bad := v.operation(t, op.ID)
	if bad.Status != "failed" {
		t.Fatal(bad)
	}
	v.mutate(t, V5Mutation{Kind: "retry", OperationID: op.ID, IdempotencyKey: "retry"}, true, v.admin)
	v.process(t)
	if v.operation(t, op.ID).Status != "failed" {
		t.Fatal("retry hid corrupt capsule")
	}
	// New config means a new generation and capsule, not reuse of broken state.
	next := v.mutate(t, V5Mutation{Kind: "configure", Values: json.RawMessage(`{"locale":"en-US"}`), IdempotencyKey: "recover"}, true, v.admin)
	v.ready(t, next)
	facts, err := v.s.ExecutionFacts(ctx, v.c, "")
	if err != nil || !facts.ReadyForDispatch {
		t.Fatal(facts, err)
	}
	_, trust := testV5Trust(t)
	trust = bytes.Replace(trust, []byte(`"active"`), []byte(`"revoked"`), 1)
	if err := v.s.catalog.SetTrust(trust); err != nil {
		t.Fatal(err)
	}
	if _, err := v.s.ExecutionFacts(ctx, v.c, ""); err == nil {
		t.Fatal("current trust bypassed")
	}
}

func TestV5ReconcileOwnerRetentionPurgeAndPackageGC(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	v.s.Users = &v5TestUsers{true}
	i := v.approved(t)
	global := v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin)
	v.ready(t, global)
	for _, owner := range []string{"1001", "1002"} {
		proof := v5TestProof("user", owner)
		op := v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "install-" + owner}, false, proof)
		op = v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: op.Generation, IdempotencyKey: "enable-" + owner}, false, proof)
		v.ready(t, op)
	}
	root := v.s.files.root.Name()
	for _, owner := range []string{"1001", "1002"} {
		name := filepath.Join(root, "data", v.c.Publisher, v.c.Key, "user-"+owner)
		if err := os.MkdirAll(name, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(name, "private"), []byte(owner), 0600); err != nil {
			t.Fatal(err)
		}
	}
	i, _ = v.s.installation(ctx, v.c, "1001")
	v.mutate(t, V5Mutation{Kind: "uninstall", ExpectedGeneration: i.Generation, IdempotencyKey: "retain"}, false, v5TestProof("user", "1001"))
	v.process(t)
	path := filepath.Join(root, "data", v.c.Publisher, v.c.Key, "user-1001", "private")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("uninstall lost retained data", err)
	}
	i, _ = v.s.installation(ctx, v.c, "1001")
	op := v.mutate(t, V5Mutation{Kind: "install", ExpectedGeneration: i.Generation, IdempotencyKey: "reinstall"}, false, v5TestProof("user", "1001"))
	v.mutate(t, V5Mutation{Kind: "uninstall", ExpectedGeneration: op.Generation, Purge: true, IdempotencyKey: "purge-owner"}, false, v5TestProof("user", "1001"))
	v.process(t)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("owner data remained", err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", v.c.Publisher, v.c.Key, "user-1002", "private")); err != nil {
		t.Fatal("other owner purged", err)
	}
	if _, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "purge_release", Version: v.c.Version, IdempotencyKey: "gc-active"}, true, v.admin); err != ErrV5Conflict {
		t.Fatal("active package gc", err)
	}
	i, _ = v.s.installation(ctx, v.c, "1002")
	v.mutate(t, V5Mutation{Kind: "uninstall", ExpectedGeneration: i.Generation, IdempotencyKey: "stop-two"}, false, v5TestProof("user", "1002"))
	i, _ = v.s.installation(ctx, v.c, "")
	v.mutate(t, V5Mutation{Kind: "uninstall", ExpectedGeneration: i.Generation, IdempotencyKey: "stop-global"}, true, v.admin)
	gc := v.mutate(t, V5Mutation{Kind: "purge_release", IdempotencyKey: "gc"}, true, v.admin)
	v.process(t)
	if v.operation(t, gc.ID).Status != "done" {
		t.Fatal("gc failed")
	}
	r, _ := v.s.release(ctx, v.c)
	if r.Storage != "purged" {
		t.Fatal(r)
	}
	if _, err := v.s.files.Package(r.Digest); err != ErrV5NotFound {
		t.Fatal("bytes retained after explicit gc", err)
	}
	// Known identity survives GC, and same signed bytes can be reimported.
	if _, err := v.s.Import(ctx, v.raw, v.envelope, "reimport", v.admin); err != nil {
		t.Fatal(err)
	}
}

func TestV5ReconcileQueueFairnessAndMissingPackage(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	i := v.approved(t)
	v.process(t)
	for n := 0; n < 40; n++ {
		id := fmt.Sprintf("%032x", n)
		op := V5Operation{V5Coordinate: v.c, ID: id, Status: "waiting", LeaseUntil: 1}
		if err := v.store.save(ctx, v5Operation, id, op); err != nil {
			t.Fatal(err)
		}
	}
	op := v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "new-work"}, true, v.admin)
	if _, err := v.s.ProcessOnce(ctx, 1); err != nil {
		t.Fatal(err)
	}
	current := v.operation(t, op.ID)
	if current.Status != "waiting" || current.Attempts != 1 {
		t.Fatal("waiting jobs starved fresh command", current)
	}
	if err := v.s.files.RemovePackage(v5BytesDigest(v.raw)); err != nil {
		t.Fatal(err)
	}
	if err := v.s.reconcilePackages(ctx); err != nil {
		t.Fatal(err)
	}
	r, _ := v.s.release(ctx, v.c)
	if r.Storage != "missing" {
		t.Fatal("missing bytes not reconciled", r.Storage)
	}
	if _, err := v.s.ExecutionFacts(ctx, v.c, ""); err == nil {
		t.Fatal("missing package still admitted")
	}
	if _, err := v.s.Import(ctx, v.raw, v.envelope, "repair-same-bytes", v.admin); err != nil {
		t.Fatal(err)
	}
	r, _ = v.s.release(ctx, v.c)
	if r.Storage != "available" {
		t.Fatal("repair did not restore catalog")
	}
}
