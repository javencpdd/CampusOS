package port

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestBoardGovernanceAuthorityBoundSnapshot(t *testing.T) {
	p := PrincipalContext{Contract: PrincipalContract, Actor: PrincipalRef{Kind: "admin", ID: "admin.current"}, Audience: "admin", CredentialID: "session.current", AuthenticationStrength: "mfa"}
	for _, mode := range []string{"same", "grant", "private_version", "principal", "missing", "typed_nil", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			initial := BoardGovernanceAuthority{Principal: p, Grants: []ResourcePolicyGrant{{SubjectKind: "admin", SubjectID: p.Actor.ID, Action: "community.thread.take_down", BoardID: "board.actual", Status: "active", RequiredStrength: "mfa", ExpiresAtMS: time.Now().Add(time.Minute).UnixMilli()}}, Binding: struct{ Version int }{1}}
			current := initial
			current.Grants = append([]ResourcePolicyGrant{}, initial.Grants...)
			switch mode {
			case "grant":
				current.Grants[0].BoardID = "board.other"
			case "private_version":
				current.Binding = struct{ Version int }{2}
			case "principal":
				current.Principal.CredentialID = "session.other"
			case "missing":
				initial.Binding, current.Binding = nil, nil
			case "typed_nil":
				var v *int
				initial.Binding, current.Binding = v, v
			case "invalid":
				initial.Principal.Actor.Kind = "anonymous"
				current = initial
			}
			err := CheckBoardGovernanceAuthority(initial, current)
			if mode == "same" && err != nil || mode != "same" && !errors.Is(err, ErrPolicyFactsChanged) {
				t.Fatal(err)
			}
			raw, err := json.Marshal(initial)
			if err != nil || string(raw) != "{}" {
				t.Fatal("host witness exposed on wire", string(raw), err)
			}
		})
	}
	if err := (BoardGovernanceQuery{BoardID: "board.actual", Action: "community.thread.pin"}).Validate(); !errors.Is(err, ErrResourcePolicyUnknownAction) {
		t.Fatal(err)
	}
}
