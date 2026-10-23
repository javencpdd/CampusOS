package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func v5CheckpointFixture(t *testing.T) (v5TestPlatform, V5Coordinate) {
	t.Helper()
	v := newV5TestPlatform(t)
	priv, trust := testV5Trust(t)
	if err := v.s.catalog.SetTrust(trust); err != nil {
		t.Fatal(err)
	}
	v.manifest.Data.PrivateKVBytes = 1024
	publish := func(version string) ([]byte, []byte) {
		m := *v.manifest
		m.Version = version
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
		return raw, release
	}
	v.raw, v.envelope = publish(v.c.Version)
	v.approved(t)
	cfg := v.mutate(t, V5Mutation{Kind: "configure", Values: json.RawMessage(`{"locale":"en-US"}`), IdempotencyKey: "a-config"}, true, v.admin)
	op := v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: cfg.Generation, IdempotencyKey: "a-enable"}, true, v.admin)
	v.ready(t, op)
	b := v.c
	b.Version = "8.2.0"
	raw, release := publish(b.Version)
	if _, err := v.s.Import(context.Background(), raw, release, "b-import", v.admin); err != nil {
		t.Fatal(err)
	}
	if _, err := v.s.Mutate(context.Background(), b.Publisher, b.Key, V5Mutation{Kind: "grant", Version: b.Version, Approved: true, Consumes: []PluginV5Consume{}, IdempotencyKey: "b-grant"}, true, v.admin); err != nil {
		t.Fatal(err)
	}
	return v, b
}
func v5CheckpointCurrent(t *testing.T, v v5TestPlatform) V5Installation {
	t.Helper()
	i, err := v.s.installation(context.Background(), v.c, "")
	if err != nil {
		t.Fatal(err)
	}
	return i
}
func v5CheckpointWrite(t *testing.T, v v5TestPlatform, i V5Installation, name, value string) {
	t.Helper()
	view, err := v.s.files.dataView(i, i.DataGeneration)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	f, err := view.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(value); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestV5CheckpointRestoreAndAuthorizationFence(t *testing.T) {
	v, b := v5CheckpointFixture(t)
	ctx := context.Background()
	a := v.c
	i := v5CheckpointCurrent(t, v)
	v5CheckpointWrite(t, v, i, "state", "original-private")
	upgrade, err := v.s.Mutate(ctx, b.Publisher, b.Key, V5Mutation{Kind: "upgrade", Version: b.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "upgrade"}, true, v.admin)
	if err != nil {
		t.Fatal(err)
	}
	if upgrade.Checkpoint.ID == "" {
		t.Fatal("upgrade missing durable checkpoint")
	}
	v.ready(t, upgrade)
	i = v5CheckpointCurrent(t, v)
	if i.Version != b.Version || i.Rollback != upgrade.Checkpoint {
		t.Fatal("checkpoint not bound to actual switch")
	}
	v5CheckpointWrite(t, v, i, "state", "candidate-private")
	// Staging old config is not permission to destroy its pre-upgrade checkpoint.
	if _, err := v.s.Mutate(ctx, a.Publisher, a.Key, V5Mutation{Kind: "configure", Version: a.Version, ExpectedRevision: mustV5ConfigRevision(t, v, a), Values: json.RawMessage(`{"locale":"zh-CN"}`), IdempotencyKey: "edit-old"}, true, v.admin); err != nil {
		t.Fatal(err)
	}
	rollback, err := v.s.Mutate(ctx, a.Publisher, a.Key, V5Mutation{Kind: "upgrade", Version: a.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "rollback"}, true, v.admin)
	if err != nil {
		t.Fatal(err)
	}
	if rollback.Restore != upgrade.Checkpoint {
		t.Fatal("rollback chose wrong checkpoint")
	}
	v.process(t)
	pending := v5CheckpointCurrent(t, v)
	if pending.Version != b.Version || pending.DataGeneration != 0 || pending.PendingDataGeneration == 0 {
		t.Fatal("data switched before ready", pending)
	}
	currentView, err := v.s.files.dataView(pending, pending.DataGeneration)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := v5ReadFile(currentView, "state", 1024)
	currentView.Close()
	if err != nil || string(raw) != "candidate-private" {
		t.Fatal("pending restore overwrote active data", err)
	}
	v.ready(t, rollback)
	i = v5CheckpointCurrent(t, v)
	view, err := v.s.files.dataView(i, i.DataGeneration)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	raw, err = v5ReadFile(view, "state", 1024)
	if err != nil || string(raw) != "original-private" {
		t.Fatal("private snapshot not selected", err)
	}
	cfg, err := v.s.configuration(ctx, a, "", v.manifest)
	if err != nil || string(cfg.Values) != `{"locale":"en-US"}` {
		t.Fatal("old config not restored", err)
	}
	var grant V5Grant
	if err := v.store.load(ctx, v5Grant, a.id(), &grant); err != nil {
		t.Fatal(err)
	}
	v.mutate(t, V5Mutation{Kind: "revoke", ExpectedRevision: grant.Revision, IdempotencyKey: "revoke-after-restore"}, true, v.admin)
	i = v5CheckpointCurrent(t, v)
	if _, err := v.s.Mutate(ctx, a.Publisher, a.Key, V5Mutation{Kind: "enable", Version: a.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "no-privilege-restore"}, true, v.admin); err != ErrV5Denied {
		t.Fatal("snapshot restored authority", err)
	}
}
func TestV5CheckpointTamperingAndOwnerBoundaries(t *testing.T) {
	for _, test := range []string{"symlink", "oversize", "tamper", "owner", "audit"} {
		t.Run(test, func(t *testing.T) {
			v, b := v5CheckpointFixture(t)
			ctx := context.Background()
			i := v5CheckpointCurrent(t, v)
			view, err := v.s.files.dataView(i, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			switch test {
			case "symlink":
				if err := view.Symlink("../../../../outside", "link"); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				v5CheckpointWrite(t, v, i, "state", string(make([]byte, 1025)))
			case "audit":
				v.fault.fail = "audit"
			}
			op, err := v.s.Mutate(ctx, b.Publisher, b.Key, V5Mutation{Kind: "upgrade", Version: b.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "up"}, true, v.admin)
			if test == "symlink" || test == "oversize" || test == "audit" {
				if err == nil {
					t.Fatal("unsafe/unaudited upgrade accepted")
				}
				if !reflect.DeepEqual(v5CheckpointCurrent(t, v), i) {
					t.Fatal("rejected upgrade changed installation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			v.ready(t, op)
			i = v5CheckpointCurrent(t, v)
			if test == "tamper" {
				d, err := v.s.files.directory("checkpoints/" + i.Publisher + "/" + i.Key + "/system")
				if err != nil {
					t.Fatal(err)
				}
				defer d.Close()
				f, err := d.OpenFile(op.Checkpoint.ID+".json", os.O_WRONLY|os.O_TRUNC, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = f.WriteString(`{}`)
				f.Close()
			}
			if test == "owner" {
				wrong := i
				wrong.Owner = "1002"
				if _, err := v.s.checkpoint(ctx, wrong, op.Checkpoint); err != ErrV5Rollback {
					t.Fatal("cross owner checkpoint read", err)
				}
				return
			}
			if _, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "upgrade", Version: v.c.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "back"}, true, v.admin); err != ErrV5Rollback {
				t.Fatal("tampered rollback admitted", err)
			}
		})
	}
}

func TestV5UpgradeStopPathsDoNotPromoteCandidate(t *testing.T) {
	for _, kind := range []string{"disable", "uninstall", "revoke"} {
		t.Run(kind, func(t *testing.T) {
			v, b := v5CheckpointFixture(t)
			i := v5CheckpointCurrent(t, v)
			op, err := v.s.Mutate(context.Background(), b.Publisher, b.Key, V5Mutation{Kind: "upgrade", Version: b.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "up-stop"}, true, v.admin)
			if err != nil {
				t.Fatal(err)
			}
			r := V5Mutation{Kind: kind, ExpectedGeneration: op.Generation, IdempotencyKey: "stop"}
			if kind == "revoke" {
				g, err := v.s.grant(context.Background(), v.c)
				if err != nil {
					t.Fatal(err)
				}
				r.ExpectedRevision = g.Revision
			}
			v.mutate(t, r, true, v.admin)
			v.process(t)
			i = v5CheckpointCurrent(t, v)
			if i.Version != v.c.Version || i.DesiredVersion != v.c.Version || i.ActiveGeneration != 0 || i.Observed != "stopped" {
				t.Fatal("stop promoted candidate", i)
			}
		})
	}
}
func TestV5VersionOrdering(t *testing.T) {
	versions := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.100000000000000000000.0", "2.0.0"}
	for a := range versions {
		for b := range versions {
			if v5VersionBefore(versions[a], versions[b]) != (a < b) {
				t.Fatal("invalid version order", versions[a], versions[b])
			}
		}
	}
}

func TestV5CheckpointObserveAtomicity(t *testing.T) {
	t.Run("missing-current-view", func(t *testing.T) {
		v, b := v5CheckpointFixture(t)
		ctx := context.Background()
		i := v5CheckpointCurrent(t, v)
		v5CheckpointWrite(t, v, i, "state", "a")
		up, err := v.s.Mutate(ctx, b.Publisher, b.Key, V5Mutation{Kind: "upgrade", Version: b.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "missing-view-up"}, true, v.admin)
		if err != nil {
			t.Fatal(err)
		}
		v.ready(t, up)
		i = v5CheckpointCurrent(t, v)
		back, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "upgrade", Version: v.c.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "missing-view-back"}, true, v.admin)
		if err != nil {
			t.Fatal(err)
		}
		v.ready(t, back)
		i = v5CheckpointCurrent(t, v)
		name := "restored-data/" + i.Publisher + "/" + i.Key + "/system/" + v5Counter(i.DataGeneration)
		if err := v.s.files.root.RemoveAll(name); err != nil {
			t.Fatal(err)
		}
		if _, err := v.s.ExecutionFacts(ctx, v.c, ""); err != ErrV5Rollback {
			t.Fatal("missing active data view admitted", err)
		}
		if _, err := v.s.Mutate(ctx, b.Publisher, b.Key, V5Mutation{Kind: "upgrade", Version: b.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "missing-view-next"}, true, v.admin); err != ErrV5Rollback {
			t.Fatal("missing active view captured as empty", err)
		}
		if _, err := v.s.files.root.Lstat(name); !os.IsNotExist(err) {
			t.Fatal("missing active view silently recreated", err)
		}
	})
	for _, kind := range []string{"audit", "restored-view-tamper", "missing-checkpoint"} {
		t.Run(kind, func(t *testing.T) {
			v, b := v5CheckpointFixture(t)
			ctx := context.Background()
			i := v5CheckpointCurrent(t, v)
			v5CheckpointWrite(t, v, i, "state", "a")
			up, err := v.s.Mutate(ctx, b.Publisher, b.Key, V5Mutation{Kind: "upgrade", Version: b.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "atomic-up"}, true, v.admin)
			if err != nil {
				t.Fatal(err)
			}
			v.ready(t, up)
			i = v5CheckpointCurrent(t, v)
			v5CheckpointWrite(t, v, i, "state", "b")
			back, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "upgrade", Version: v.c.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "atomic-back"}, true, v.admin)
			if err != nil {
				t.Fatal(err)
			}
			v.process(t)
			before := v5CheckpointCurrent(t, v)
			op := v.operation(t, back.ID)
			switch kind {
			case "audit":
				v.fault.fail = "audit"
			case "restored-view-tamper":
				view, err := v.s.files.dataView(before, before.PendingDataGeneration)
				if err != nil {
					t.Fatal(err)
				}
				if err := view.WriteFile("state", []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
				view.Close()
			case "missing-checkpoint":
				d, err := v.s.files.directory("checkpoints/" + i.Publisher + "/" + i.Key + "/system")
				if err != nil {
					t.Fatal(err)
				}
				if err := d.Remove(up.Checkpoint.ID + ".json"); err != nil {
					t.Fatal(err)
				}
				d.Close()
			}
			observation := V5Observation{op.ID, op.Lease, op.Generation, op.Preparation.ConfigDigest, true}
			if err := v.s.Observe(ctx, observation); err == nil {
				t.Fatal("unaudited/unverified pointer switch accepted")
			}
			v.fault.fail = ""
			after := v5CheckpointCurrent(t, v)
			if after != before || after.Version != b.Version || after.DataGeneration != 0 {
				t.Fatal("failed readiness changed committed version/data pointer")
			}
			view, err := v.s.files.dataView(after, after.DataGeneration)
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			raw, err := v5ReadFile(view, "state", 1024)
			if err != nil || string(raw) != "b" {
				t.Fatal("failed restore overwrote current bytes", err)
			}
			if kind == "audit" {
				if err := v.s.Observe(ctx, observation); err != nil {
					t.Fatal("verified retry after audit recovery", err)
				}
			}
		})
	}
}

func TestV5UpgradeStoppedRequiresExplicitReadiness(t *testing.T) {
	v, b := v5CheckpointFixture(t)
	ctx := context.Background()
	i := v5CheckpointCurrent(t, v)
	v.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: i.Generation, IdempotencyKey: "off-before-upgrade"}, true, v.admin)
	v.process(t)
	i = v5CheckpointCurrent(t, v)
	staged, err := v.s.Mutate(ctx, b.Publisher, b.Key, V5Mutation{Kind: "upgrade", Version: b.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "stopped-upgrade"}, true, v.admin)
	if err != nil {
		t.Fatal(err)
	}
	v.process(t)
	i = v5CheckpointCurrent(t, v)
	if i.Version != v.c.Version || i.DesiredVersion != b.Version || i.Desired != "disabled" || i.Observed != "stopped" || i.ActiveGeneration != 0 {
		t.Fatal("stopped upgrade auto-enabled or promoted candidate", i)
	}
	op, err := v.s.Mutate(ctx, b.Publisher, b.Key, V5Mutation{Kind: "enable", Version: b.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "explicit-staged-enable"}, true, v.admin)
	if err != nil {
		t.Fatal(err)
	}
	if op.Checkpoint != staged.Checkpoint {
		t.Fatal("explicit enable lost staged checkpoint")
	}
	v.ready(t, op)
	i = v5CheckpointCurrent(t, v)
	if i.Version != b.Version || i.Observed != "ready" || i.Rollback != staged.Checkpoint {
		t.Fatal("staged upgrade did not commit after readiness")
	}
}
