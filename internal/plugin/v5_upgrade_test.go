package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestV5UpgradePrepareSwitchAndVerifiableRollback(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	priv, trust := testV5Trust(t)
	if err := v.s.catalog.SetTrust(trust); err != nil {
		t.Fatal(err)
	}
	v.envelope, _ = SignPluginV5Package(v.raw, "test-key", priv)
	i := v.approved(t)
	cfg := v.mutate(t, V5Mutation{Kind: "configure", Values: json.RawMessage(`{"locale":"en-US"}`), IdempotencyKey: "v1-config"}, true, v.admin)
	_ = i
	active := v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: cfg.Generation, IdempotencyKey: "v1-enable"}, true, v.admin)
	v.ready(t, active)
	original := v.c
	candidate := original
	candidate.Version = "8.2.0"
	publish := func(schema int64) {
		t.Helper()
		m := *v.manifest
		m.Version = candidate.Version
		m.Data.Upgrade.SchemaVersion = schema
		m.Data.Upgrade.Mode = "preserve"
		wire, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(v.dir, "plugin.json"), wire, 0600); err != nil {
			t.Fatal(err)
		}
		raw, _, err := BuildPluginV5Package(v.dir)
		if err != nil {
			t.Fatal(err)
		}
		release, err := SignPluginV5Package(raw, "test-key", priv)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.s.Import(ctx, raw, release, "import-"+candidate.Version, v.admin); err != nil {
			t.Fatal(err)
		}
		if _, err := v.s.Mutate(ctx, candidate.Publisher, candidate.Key, V5Mutation{Kind: "grant", Version: candidate.Version, Approved: true, Consumes: []PluginV5Consume{}, IdempotencyKey: "grant-" + candidate.Version}, true, v.admin); err != nil {
			t.Fatal(err)
		}
	}
	publish(v.manifest.Data.Upgrade.SchemaVersion)
	i, _ = v.s.installation(ctx, original, "")
	staged, err := v.s.Mutate(ctx, candidate.Publisher, candidate.Key, V5Mutation{Kind: "configure", Version: candidate.Version, Values: json.RawMessage(`{"locale":"en-US"}`), IdempotencyKey: "stage-before-upgrade"}, true, v.admin)
	if err != nil || staged.Status != "done" || staged.Generation != 0 {
		t.Fatal("target config needs premature upgrade", staged, err)
	}
	unchanged, _ := v.s.installation(ctx, original, "")
	if unchanged != i {
		t.Fatal("staged config disturbed current instance")
	}
	up, err := v.s.Mutate(ctx, candidate.Publisher, candidate.Key, V5Mutation{Kind: "upgrade", Version: candidate.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "upgrade"}, true, v.admin)
	if err != nil {
		t.Fatal(err)
	}
	i, _ = v.s.installation(ctx, original, "")
	if i.Version != original.Version || i.DesiredVersion != candidate.Version || i.ActiveGeneration == i.Generation {
		t.Fatal("switched before ready", i)
	}
	if _, err := v.s.Mutate(ctx, candidate.Publisher, candidate.Key, V5Mutation{Kind: "configure", Version: candidate.Version, ExpectedRevision: mustV5ConfigRevision(t, v, candidate), Values: json.RawMessage(`{"locale":"zh-CN"}`), IdempotencyKey: "candidate-config"}, true, v.admin); err != nil {
		t.Fatal("pending candidate config", err)
	}
	v.process(t)
	up = v.operation(t, up.ID)
	if up.Status != "superseded" {
		t.Fatal("old candidate generation resurrected", up)
	}
	rows, _ := v.store.list(ctx, v5Operation, v5Query{Limit: 50})
	for _, raw := range rows {
		var op V5Operation
		json.Unmarshal(raw, &op)
		if op.Kind == "configure" && op.Version == candidate.Version && op.Generation > 0 {
			v.ready(t, op)
		}
	}
	i, _ = v.s.installation(ctx, candidate, "")
	if i.Version != candidate.Version || i.Observed != "ready" {
		t.Fatal("ready switch", i)
	}
	rollback, err := v.s.Mutate(ctx, original.Publisher, original.Key, V5Mutation{Kind: "upgrade", Version: original.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "rollback"}, true, v.admin)
	if err != nil {
		t.Fatal(err)
	}
	v.ready(t, rollback)
	oldConfig, _ := v.s.configuration(ctx, original, "", v.manifest)
	if string(oldConfig.Values) != `{"locale":"en-US"}` {
		t.Fatal("old version config not retained")
	}
	candidate.Version = "9.0.0"
	publish(v.manifest.Data.Upgrade.SchemaVersion + 1)
	i, _ = v.s.installation(ctx, original, "")
	if _, err := v.s.Mutate(ctx, candidate.Publisher, candidate.Key, V5Mutation{Kind: "upgrade", Version: candidate.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "irreversible"}, true, v.admin); err != ErrV5Rollback {
		t.Fatal("unverified data migration admitted", err)
	}
	after, _ := v.s.installation(ctx, original, "")
	if after != i {
		t.Fatal("rejected migration changed active state")
	}
}

func mustV5ConfigRevision(t *testing.T, v v5TestPlatform, c V5Coordinate) int64 {
	t.Helper()
	var cfg V5Configuration
	if err := v.store.load(context.Background(), v5Config, v5VersionOwnerID(c, ""), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Revision
}
