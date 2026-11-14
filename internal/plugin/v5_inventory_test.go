package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestV5InventoryOwnerAndVisibilityBeforePagination(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	v.imported(t)
	// Hidden package releases and noninstalled grants cannot occupy a user page.
	for n := 0; n < 60; n++ {
		c := V5Coordinate{"aaa", "hidden", "1.0." + v5Counter(int64(n))}
		r := V5PackageRelease{V5Coordinate: c}
		v.store.save(ctx, v5Release, c.id(), r)
	}
	page, err := v.s.Inventory(ctx, false, "", 1, v5TestProof("user", "1001"))
	if err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
	v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "install"}, true, v.admin)
	v.mutate(t, V5Mutation{Kind: "grant", IdempotencyKey: "grant", Approved: true, Consumes: []PluginV5Consume{}}, true, v.admin)
	op := v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "user-one"}, false, v5TestProof("user", "1001"))
	v.mutate(t, V5Mutation{Kind: "configure", IdempotencyKey: "private-config", Values: json.RawMessage(`{"locale":"en-US"}`)}, false, v5TestProof("user", "1001"))
	page, err = v.s.Inventory(ctx, false, "", 1, v5TestProof("user", "1002"))
	if err != nil || len(page.Items) != 1 || page.Next != "" || page.Items[0].Installation != nil || page.Items[0].Configuration != nil {
		t.Fatal(page, err)
	}
	own, err := v.s.Detail(ctx, v.c, false, v5TestProof("user", "1001"))
	if err != nil || string(own.Configuration.Values) != `{"locale":"en-US"}` {
		t.Fatal(own, err)
	}
	other, err := v.s.Detail(ctx, v.c, false, v5TestProof("user", "1002"))
	if err != nil || string(other.Configuration.Values) == string(own.Configuration.Values) {
		t.Fatal("private config leaked", other, err)
	}
	if _, err := v.s.Operation(ctx, op.ID, false, v5TestProof("user", "1002")); err != ErrV5NotFound {
		t.Fatal("other operation disclosed", err)
	}
	if _, err := v.s.Operation(ctx, op.ID, false, v5TestProof("user", "1001")); err != nil {
		t.Fatal(err)
	}
}

func TestV5InventoryProjectsSiteStateAndOnlyOwnConsent(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	installed := v.approved(t)
	user := v5TestProof("user", "1001")
	page, err := v.s.Inventory(ctx, false, "", 25, user)
	if err != nil || len(page.Items) != 1 || page.Items[0].SiteState == nil {
		t.Fatal(page, err)
	}
	if state := page.Items[0].SiteState; !state.Installed || state.Enabled || state.Ready {
		t.Fatal("installed is not the same as enabled", state)
	}
	v.s.uiHostReady.Store(true)
	v.mutate(t, V5Mutation{Kind: "enable", IdempotencyKey: "site-enable", ExpectedGeneration: installed.Generation}, true, v.admin)
	v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "personal-install"}, false, user)
	v.mutate(t, V5Mutation{Kind: "consent", IdempotencyKey: "personal-consent", Accepted: true, PurposeHash: V5PurposeHash(v.manifest)}, false, user)
	global, _ := v.s.installation(ctx, v.c, "")
	global.ConfigDigest = "system-config-sentinel"
	global.Rollback.ID = "recovery-sentinel"
	if err := v.store.save(ctx, v5Global, v5StateID(v.c, ""), global); err != nil {
		t.Fatal(err)
	}
	page, err = v.s.Inventory(ctx, false, "", 25, user)
	if err != nil || len(page.Items) != 1 || page.Items[0].Consent == nil || !page.Items[0].Consent.Accepted || page.Items[0].Configuration != nil {
		t.Fatal("list must distinguish a saved own consent without configuration", page, err)
	}
	if state := page.Items[0].SiteState; !state.Installed || !state.Enabled || !state.Ready {
		t.Fatal("UI-only availability must not require a backend process", state)
	}
	other, err := v.s.Inventory(ctx, false, "", 25, v5TestProof("user", "1002"))
	if err != nil || len(other.Items) != 1 || other.Items[0].Consent != nil || other.Items[0].Installation != nil {
		t.Fatal("another owner's personal state must remain private", other, err)
	}
	raw, _ := json.Marshal(other)
	if strings.Contains(string(raw), "system-config-sentinel") || strings.Contains(string(raw), "recovery-sentinel") {
		t.Fatal("site summary exposed private system preparation data")
	}
	v.mutate(t, V5Mutation{Kind: "disable", IdempotencyKey: "site-disable", ExpectedGeneration: global.Generation}, true, v.admin)
	page, err = v.s.Inventory(ctx, false, "", 25, user)
	if err != nil || page.Items[0].SiteState.Enabled || page.Items[0].SiteState.Ready || page.Items[0].Consent == nil {
		t.Fatal("site stop must remain distinct from personal consent", page, err)
	}
}
