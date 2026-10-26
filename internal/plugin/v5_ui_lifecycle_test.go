package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestV5UIOnlyUpgradeActivatesAssetsWithoutRunner(t *testing.T) {
	v, user := newV5UIFixture(t)
	v.s.Users = &v5TestUsers{active: true}
	ctx := context.Background()
	priv, trust := testV5Trust(t)
	if err := v.s.catalog.SetTrust(trust); err != nil {
		t.Fatal(err)
	}
	release, err := SignPluginV5Package(v.raw, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.s.Import(ctx, v.raw, release, "ui-current-trust", v.admin); err != nil {
		t.Fatal(err)
	}
	if _, err := NewV5UIHost(v.s, "http://{isolation}.plugin-ui.test", []string{"http://parent.test"}); err != nil {
		t.Fatal(err)
	}
	v.process(t)
	a := v.c
	b := a
	b.Version = "8.2.0"
	m := *v.manifest
	m.Version = b.Version
	m.Data.Upgrade.Mode = "preserve"
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(v.dir, "plugin.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	archive, _, err := BuildPluginV5Package(v.dir)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := SignPluginV5Package(archive, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.s.Import(ctx, archive, signature, "ui-candidate", v.admin); err != nil {
		t.Fatal(err)
	}
	cmd := func(c V5Coordinate, admin bool, r V5Mutation) V5Operation {
		t.Helper()
		proof := user
		owner := "1001"
		if admin {
			proof = v.admin
			owner = ""
		}
		r.Version = c.Version
		r.IdempotencyKey = v5RandomID()
		if r.Kind == "upgrade" {
			i, err := v.s.installation(ctx, c, owner)
			if err != nil {
				t.Fatal(err)
			}
			r.ExpectedGeneration = i.Generation
		}
		op, err := v.s.Mutate(ctx, c.Publisher, c.Key, r, admin, proof)
		if err != nil {
			t.Fatal(r.Kind, err)
		}
		return op
	}
	cmd(b, true, V5Mutation{Kind: "grant", Approved: true, Consumes: m.Consumes})
	cmd(b, true, V5Mutation{Kind: "upgrade"})
	v.process(t)
	i, err := v.s.installation(ctx, b, "")
	if err != nil || i.Version != b.Version || i.Observed == "ready" || i.ActiveGeneration != 0 {
		t.Fatal("UI upgrade waited for/fabricated a runner", i, err)
	}
	cmd(b, false, V5Mutation{Kind: "consent", Accepted: true, PurposeHash: V5PurposeHash(&m)})
	cmd(b, false, V5Mutation{Kind: "upgrade"})
	v.process(t)
	if err := v.s.withUI(ctx, b, false, user, func(f V5UIFacts, p *PluginV5Package) error { return nil }); err != nil {
		t.Fatal("new user UI unavailable", err)
	}
	cmd(a, true, V5Mutation{Kind: "upgrade"})
	v.process(t)
	cmd(a, false, V5Mutation{Kind: "upgrade"})
	v.process(t)
	if err := v.s.withUI(ctx, a, false, user, func(f V5UIFacts, p *PluginV5Package) error { return nil }); err != nil {
		t.Fatal("restored UI unavailable", err)
	}
	if i, err := v.s.installation(ctx, a, "1001"); err != nil || i.Version != a.Version || i.Observed == "ready" || i.ActiveGeneration != 0 {
		t.Fatal("UI rollback fabricated backend health", i, err)
	}
}
