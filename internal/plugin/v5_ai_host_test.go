package plugin

import (
	"context"
	"encoding/json"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"strings"
	"testing"
)

func TestAIHostClosedRequestAndInvocation(t *testing.T) {
	good := `{"profile_id":"local-chat","binding_revision":"1","profile_revision":"2","run_id":"ar_` + strings.Repeat("a", 32) + `","prompt":"explicit"}`
	if q, e := ParseV5AIRequest([]byte(good), false); e != nil || q.ProfileID != "local-chat" {
		t.Fatal(q, e)
	}
	for _, bad := range []string{strings.Replace(good, `"1"`, `"01"`, 1), strings.Replace(good, `"1"`, `"9999999999999999999"`, 1), strings.Replace(good, `"prompt":"explicit"`, `"prompt":"explicit","owner":"1"`, 1), strings.Replace(good, `"prompt":"explicit"`, `"prompt":"explicit","profile_id":"remote-chat"`, 1), strings.Replace(good, "local-chat", "foreign", 1)} {
		if _, e := ParseV5AIRequest([]byte(bad), false); e == nil {
			t.Fatal("accepted override", bad)
		}
	}
	if _, e := ParseV5AIRequest([]byte(good), true); e == nil {
		t.Fatal("RAG without collection")
	}
	p := identity.PrincipalContext{Contract: identity.PrincipalContract, Actor: identity.PrincipalRef{Kind: "user", ID: "1001"}, Audience: "user", CredentialID: "session-a", AuthenticationStrength: "password"}
	r := func(context.Context) (identity.PrincipalContext, error) { return p, nil }
	ctx := v5BindInvocation(t.Context(), &v5ManagedInstance{id: "i", owner: "1001", generation: 2}, r, p)
	ctx = context.WithValue(ctx, v5NestedCallKey{}, "i")
	if _, e := V5CurrentInvocation(ctx, "1001"); e == nil {
		t.Fatal("not a Host call")
	}
	v := ctx.Value(v5InvocationKey{}).(v5Invocation)
	v.require = func(context.Context, string) (PluginV5Consume, error) { return PluginV5Consume{}, nil }
	ctx = context.WithValue(ctx, v5InvocationKey{}, v)
	if _, e := V5CurrentInvocation(ctx, "1001"); e != nil {
		t.Fatal(e)
	}
	if _, e := V5CurrentInvocation(ctx, "1002"); e == nil {
		t.Fatal("cross owner")
	}
	p.CredentialID = "session-b"
	if _, e := V5CurrentInvocation(ctx, "1001"); e == nil {
		t.Fatal("cross session")
	}
	if _, e := V5CurrentInvocation(t.Context(), "1001"); e == nil {
		t.Fatal("background instance borrowed user")
	}
	raw, _ := json.Marshal(map[string]string{"collection_id": "kc_" + strings.Repeat("b", 32)})
	_ = raw
}

func TestModelRuntimeTimeoutRequiresExactApprovedConsumption(t *testing.T) {
	d := PluginV5Consume{Capability: V5AIChatLocal, Scope: PluginV5ConsumeScope{Kind: "self"}, Purpose: "Explicit model"}
	m := &PluginV5Manifest{Runtime: "container", Consumes: []PluginV5Consume{d}}
	for _, g := range []V5Grant{{}, {Approved: true}, {Approved: true, Consumes: []PluginV5Consume{{Capability: V5AIChatRemote, Scope: d.Scope, Purpose: d.Purpose}}}} {
		if v5RuntimeTimeoutCeiling(m, g) != 5000 {
			t.Fatal("model declaration alone expanded deployment ceiling")
		}
	}
	if v5RuntimeTimeoutCeiling(m, V5Grant{Approved: true, Consumes: m.Consumes}) != 30000 {
		t.Fatal("approved model ceiling")
	}
	m.Runtime = "wasm"
	if v5RuntimeTimeoutCeiling(m, V5Grant{Approved: true, Consumes: m.Consumes}) != 5000 {
		t.Fatal("Wasm deployment ceiling changed")
	}
}
