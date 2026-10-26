package plugin

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	campusossdk "github.com/campusos/CampusOS/sdk/go"
)

func newV5AuthorizedManagedFixture(t *testing.T, timeout ...int64) (v5TestPlatform, *V5ManagedRuntime, V5RuntimeCredential) {
	t.Helper()
	v, priv := newV5ContributionTestPlatform(t)
	if len(timeout) > 0 {
		v.manifest.HostResources.TimeoutMS = timeout[0]
	}
	v.manifest.Consumes = []PluginV5Consume{}
	for _, code := range []string{"community.thread.public.read", "plugin.lifecycle.subscribe", "community.thread.public.subscribe", "knowledge.source.public.subscribe"} {
		v.manifest.Consumes = append(v.manifest.Consumes, PluginV5Consume{Capability: code, Scope: PluginV5ConsumeScope{Kind: "public"}, Purpose: "Bounded managed target", Required: true})
	}
	if err := os.WriteFile(filepath.Join(v.dir, "plugin.json"), mustV5JSON(v.manifest), 0600); err != nil {
		t.Fatal(err)
	}
	raw, p, err := BuildPluginV5Package(v.dir)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := SignPluginV5Package(raw, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	v.raw, v.envelope, v.manifest = raw, envelope, p.Manifest
	v.installed(t)
	v.mutate(t, V5Mutation{Kind: "grant", IdempotencyKey: "approve", Approved: true, Consumes: v.manifest.Consumes, Resources: v.manifest.HostResources}, true, v.admin)
	i, _ := v.s.installation(context.Background(), v.c, "")
	v.ready(t, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin))
	i, _ = v.s.installation(context.Background(), v.c, "")
	h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
	lease, err := h.Publish(context.Background(), v.c, "", i.Generation, time.Minute, v5ManagedTestAdapter{invoke: v5ManagedReply, event: func(context.Context, json.RawMessage) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	return v, h, lease
}

func TestV5ManagedHostCredentialTypedGrantAndRecheck(t *testing.T) {
	ctx := context.Background()
	v, h, lease := newV5AuthorizedManagedFixture(t)
	features := v5ChatPGFeatures(t)
	allowed := true
	e := V5HostEndpoint{Feature: features, FeatureID: "contract-chat-consumer", Authorize: func(context.Context, string, PluginV5Consume, json.RawMessage) (string, error) {
		if !allowed {
			return "", ErrV5Denied
		}
		return "rev42", nil
	}, Call: func(_ context.Context, _ string, s PluginV5Consume, raw json.RawMessage, w io.Writer) error {
		if s.Scope.Kind != "public" {
			t.Fatal("scope forged")
		}
		_, err := w.Write([]byte(`{"public":true}`))
		return err
	}}
	if err := h.BindHost("community.thread.public.read", e); err != nil {
		t.Fatal(err)
	}
	if err := h.BindHost("community.thread.public.read", e); err != ErrV5ContributionConflict {
		t.Fatal("endpoint silently replaced", err)
	}
	i, _ := v.s.installation(ctx, v.c, "")
	payload := []byte(`{"thread_id":"1"}`)
	call := campusossdk.V5DispatchCall{RequestID: "host-1", Contract: "campusos.plugin-dispatch/v1", Target: campusossdk.V5DispatchTarget{Kind: "host_api", ID: "community.thread.public.read"}, InstanceGeneration: i.Generation, DeadlineMS: time.Now().Add(900 * time.Millisecond).UnixMilli(), RequestBytes: int64(len(payload)), ResponseLimitBytes: 4096}
	if _, err := h.HostCall(ctx, lease.Token, mustV5JSON(call), payload); err != nil {
		t.Fatal(err)
	}
	if _, err := h.HostCall(ctx, "forged", mustV5JSON(call), payload); err == nil {
		t.Fatal("unverified instance")
	}
	allowed = false
	if out, err := h.HostCall(ctx, lease.Token, mustV5JSON(call), payload); err == nil || len(out) > 0 {
		t.Fatal("resource authorization bypass")
	}
	allowed = true
	g, _ := v.s.grant(ctx, v.c)
	v.mutate(t, V5Mutation{Kind: "revoke", ExpectedRevision: g.Revision, IdempotencyKey: "revoke"}, true, v.admin)
	if out, err := h.HostCall(ctx, lease.Token, mustV5JSON(call), payload); err == nil || len(out) > 0 {
		t.Fatal("revocation did not fence Host call")
	}
}

func TestV5ManagedCurrentCatalogMatchesContract(t *testing.T) {
	raw, err := os.ReadFile("../../docs/api/plugin-v5-host-catalog-current.json")
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := CurrentPluginV5Catalogs()
	var want, got any
	if json.Unmarshal(raw, &want) != nil || json.Unmarshal(actual, &got) != nil || !reflect.DeepEqual(want, got) {
		t.Fatal("published Host contract differs from runtime factory")
	}
}

func TestV5ManagedHostGracefulDrain(t *testing.T) {
	ctx := context.Background()
	_, h, lease := newV5AuthorizedManagedFixture(t, 5000)
	features := v5ChatPGFeatures(t)
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	endpoint := V5HostEndpoint{Feature: features, FeatureID: "contract-chat-consumer", Authorize: func(context.Context, string, PluginV5Consume, json.RawMessage) (string, error) { return "rev42", nil }, Call: func(c context.Context, _ string, _ PluginV5Consume, _ json.RawMessage, w io.Writer) error {
		close(started)
		select {
		case <-c.Done():
			return c.Err()
		case <-release:
			_, err := w.Write([]byte(`{"public":true}`))
			return err
		}
	}}
	if err := h.BindHost("community.thread.public.read", endpoint); err != nil {
		t.Fatal(err)
	}
	i, err := h.credential(ctx, lease.Token)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"thread_id":"1"}`)
	call := campusossdk.V5DispatchCall{RequestID: "drain-host", Contract: "campusos.plugin-dispatch/v1", Target: campusossdk.V5DispatchTarget{Kind: "host_api", ID: "community.thread.public.read"}, InstanceGeneration: i.generation, DeadlineMS: time.Now().Add(3 * time.Second).UnixMilli(), RequestBytes: int64(len(payload)), ResponseLimitBytes: 4096}
	go func() {
		raw, err := h.HostCall(ctx, lease.Token, mustV5JSON(call), payload)
		if len(raw) == 0 && err == nil {
			err = ErrV5CallInvalid
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("Host call not started")
	}
	short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if err := h.Withdraw(short, lease.ID, false); err == nil {
		t.Fatal("drain finished before callback")
	}
	if _, err := h.HostCall(ctx, lease.Token, mustV5JSON(call), payload); err == nil {
		t.Fatal("new Host call admitted during drain")
	}
	select {
	case err := <-done:
		t.Fatal("drain cancelled by current-credential guard", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal("authorized in-flight Host result lost", err)
	}
}
