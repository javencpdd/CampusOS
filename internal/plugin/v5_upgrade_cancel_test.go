package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestV5UpgradeDisableCancelsPendingVersion(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	key, trust := testV5Trust(t)
	if err := v.s.catalog.SetTrust(trust); err != nil {
		t.Fatal(err)
	}
	v.envelope, _ = SignPluginV5Package(v.raw, "test-key", key)
	v.approved(t)
	configured := v.mutate(t, V5Mutation{Kind: "configure", Values: json.RawMessage(`{"locale":"en-US"}`), IdempotencyKey: "cancel-original-config"}, true, v.admin)
	active := v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: configured.Generation, IdempotencyKey: "cancel-original-enable"}, true, v.admin)
	v.ready(t, active)

	candidate := v.c
	candidate.Version = "8.2.0"
	m := *v.manifest
	m.Version = candidate.Version
	m.Data.Upgrade.Mode = "preserve"
	wire, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(v.dir, "plugin.json"), wire, 0600); err != nil {
		t.Fatal(err)
	}
	raw, _, err := BuildPluginV5Package(v.dir)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := SignPluginV5Package(raw, "test-key", key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.s.Import(ctx, raw, signature, "cancel-import-candidate", v.admin); err != nil {
		t.Fatal(err)
	}
	mutateCandidate := func(r V5Mutation) V5Operation {
		t.Helper()
		r.Version = candidate.Version
		op, err := v.s.Mutate(ctx, candidate.Publisher, candidate.Key, r, true, v.admin)
		if err != nil {
			t.Fatal(r.Kind, err)
		}
		return op
	}
	mutateCandidate(V5Mutation{Kind: "grant", Approved: true, Consumes: []PluginV5Consume{}, IdempotencyKey: "cancel-candidate-grant"})
	mutateCandidate(V5Mutation{Kind: "configure", Values: json.RawMessage(`{"locale":"zh-CN"}`), IdempotencyKey: "cancel-candidate-config"})
	i, _ := v.s.installation(ctx, v.c, "")
	upgrade := mutateCandidate(V5Mutation{Kind: "upgrade", ExpectedGeneration: i.Generation, IdempotencyKey: "cancel-candidate-upgrade"})
	v.process(t)
	prepared := v.operation(t, upgrade.ID)
	if prepared.Status != "waiting" || prepared.Preparation == nil {
		t.Fatal("candidate was not prepared", prepared)
	}
	disabled := v.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: upgrade.Generation, IdempotencyKey: "cancel-pending-upgrade"}, true, v.admin)
	v.process(t)
	i, _ = v.s.installation(ctx, v.c, "")
	if i.Version != v.c.Version || i.DesiredVersion != v.c.Version || i.Observed != "stopped" || i.ActiveGeneration != 0 {
		t.Fatal("disable promoted an unready candidate", i)
	}
	if err := v.s.Observe(ctx, V5Observation{OperationID: prepared.ID, Lease: prepared.Lease, Generation: prepared.Generation, ConfigDigest: prepared.Preparation.ConfigDigest, Ready: true}); err != ErrV5Conflict {
		t.Fatal("cancelled candidate observation admitted", err)
	}
	replay := v.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: upgrade.Generation, IdempotencyKey: "cancel-pending-upgrade"}, true, v.admin)
	if replay.ID != disabled.ID || replay.Generation != disabled.Generation {
		t.Fatal("disable replay created another generation")
	}
	oldConfig, err := v.s.configuration(ctx, v.c, "", v.manifest)
	if err != nil || string(oldConfig.Values) != `{"locale":"en-US"}` {
		t.Fatal("original configuration changed", err)
	}
	newConfig, err := v.s.configuration(ctx, candidate, "", &m)
	if err != nil || string(newConfig.Values) != `{"locale":"zh-CN"}` {
		t.Fatal("candidate configuration lost", err)
	}
	enabled := v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: disabled.Generation, IdempotencyKey: "cancel-reenable-original"}, true, v.admin)
	v.ready(t, enabled)
	i, _ = v.s.installation(ctx, v.c, "")
	if i.Version != v.c.Version || i.Observed != "ready" || i.Generation <= upgrade.Generation {
		t.Fatal("original did not recover in a new generation", i)
	}
}
