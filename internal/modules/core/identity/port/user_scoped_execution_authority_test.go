package port

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestUserScopedExecutionQueryAndBoundAuthority(t *testing.T) {
	q := UserScopedExecutionQuery{PermissionCode: "community.thread.pin", Scope: PolicyResourceRef{Kind: "community.board", ID: "42"}}
	p := PrincipalContext{Contract: PrincipalContract, Actor: PrincipalRef{Kind: "user", ID: "22001"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "session.current"}
	a := UserScopedExecutionAuthority{Principal: p, Query: q, Allowed: true, ExpiresAtMS: 1000, Binding: struct{ Version int64 }{1}}
	if q.Validate() != nil || CheckUserScopedExecutionAuthority(a, a) != nil {
		t.Fatal("valid same-call authority rejected")
	}
	for _, code := range []string{"community.thread.lock", "community.thread.take_down", "community.thread.edit", "community.post.delete", "platform.reliability.read", ""} {
		changed := q
		changed.PermissionCode = code
		if (changed.Validate() == nil) != (code == "community.thread.lock") {
			t.Fatal("expanded query", code)
		}
	}
	for _, id := range []string{"0", "-1", "042", "42.0", "board", "9223372036854775808", ""} {
		changed := q
		changed.Scope.ID = id
		if changed.Validate() == nil {
			t.Fatal("invalid actual board", id)
		}
	}
	for _, mutate := range []func(*UserScopedExecutionAuthority){
		func(a *UserScopedExecutionAuthority) { a.Query.Scope.ID = "43" },
		func(a *UserScopedExecutionAuthority) { a.Query.PermissionCode = "community.thread.lock" },
		func(a *UserScopedExecutionAuthority) { a.Principal.CredentialID = "another" },
		func(a *UserScopedExecutionAuthority) { a.Allowed = false },
		func(a *UserScopedExecutionAuthority) { a.ExpiresAtMS++ },
		func(a *UserScopedExecutionAuthority) { a.Binding = (*int)(nil) },
	} {
		changed := a
		mutate(&changed)
		if !errors.Is(CheckUserScopedExecutionAuthority(a, changed), ErrPolicyFactsChanged) {
			t.Fatal("authority replay")
		}
	}
	if raw, err := json.Marshal(a); err != nil || string(raw) != "{}" {
		t.Fatal("private authority serialized", string(raw), err)
	}
	if (BoardGovernanceQuery{BoardID: "42", Action: q.PermissionCode}).Validate() == nil {
		t.Fatal("frozen board delegation expanded")
	}
}
