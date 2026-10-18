package plugin

import (
	"context"
	"encoding/json"
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
