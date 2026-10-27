package port

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdminMFARequirementFactIsHostOnlyAndCannotRoundTrip(t *testing.T) {
	p := PrincipalContext{Contract: PrincipalContract, Actor: PrincipalRef{Kind: "admin", ID: "adm.test"}, Audience: "admin", AuthenticationStrength: "password", CredentialID: "adms.test", AdminMFAOptional: true}
	if !p.SatisfiesAdminMFARequirement() || p.AuthenticationStrength != "password" {
		t.Fatal("host fact fabricated authentication strength")
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "mfa_optional") {
		t.Fatal("host policy fact leaked onto the wire")
	}
	if q, err := ParsePrincipalJSON(encoded); err != nil || q.AdminMFAOptional || q.SatisfiesAdminMFARequirement() {
		t.Fatal(q, err)
	}
	p.Actor.Kind = "user"
	if p.Validate() == nil {
		t.Fatal("Admin deployment policy crossed into User domain")
	}
	p.Actor.Kind = "admin"
	p.AdminMFAOptional = false
	if p.SatisfiesAdminMFARequirement() {
		t.Fatal("default password principal bypassed MFA")
	}
	for _, marker := range []string{"true", "false", "null", `"true"`} {
		invalid := strings.TrimSuffix(string(encoded), "}") + `,"admin_mfa_optional":` + marker + "}"
		if _, err := ParsePrincipalJSON([]byte(invalid)); err == nil {
			t.Fatal("client policy marker accepted")
		}
	}
}
