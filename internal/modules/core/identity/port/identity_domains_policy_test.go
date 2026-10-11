package port

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const identityDomainsJSON = `{"contract":"campusos.identity-domains/v1","domains":{"user":{"issuer":"campusos-user","audience":"user-api","key_id":"user-key","credential_store":"user-credentials","session_store":"user-sessions","recovery_store":"user-recovery","role_store":"user-roles"},"admin":{"issuer":"campusos-admin","audience":"admin-api","key_id":"admin-key","credential_store":"admin-credentials","session_store":"admin-sessions","recovery_store":"admin-recovery","role_store":"admin-roles"}},"request":{"kind":"admin","account_id":"same-42","issuer":"campusos-admin","audience":"admin-api","key_id":"admin-key","credential_store":"admin-credentials","session_store":"admin-sessions","challenge_kind":"access","challenge_domain":"admin","target_account_id":"same-42","status":"active","activation_source":"direct"}}`

func TestIdentityDomainCodecRoundTrip(t *testing.T) {
	for _, raw := range []string{
		identityDomainsJSON,
		strings.Replace(identityDomainsJSON, `"session_store":"admin-sessions","challenge_kind"`, `"session_store":"admin-sessions","recovery_store":"admin-recovery","role_store":"admin-roles","challenge_kind"`, 1),
		strings.Replace(identityDomainsJSON, `"request":{`, `"operation":{"kind":"create","actor_kind":"admin","actor_mfa":true,"target_admin_id":"same-42","target_is_security_admin":false,"active_security_admins":2,"bootstrap_enabled":false,"audit_reason":"required audit"},"request":{`, 1),
	} {
		r, err := ParseIdentityDomainInputJSON([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var original, roundtrip any
		if json.Unmarshal([]byte(raw), &original) != nil || json.Unmarshal(encoded, &roundtrip) != nil || !reflect.DeepEqual(original, roundtrip) {
			t.Fatal("wire values changed")
		}
	}
}

func TestIdentityDomainCodecRejectsAmbiguousInput(t *testing.T) {
	cases := map[string]string{
		"duplicate":            strings.Replace(identityDomainsJSON, `"status":"active"`, `"status":"revoked","status":"active"`, 1),
		"nested_duplicate":     strings.Replace(identityDomainsJSON, `"issuer":"campusos-admin","audience":"admin-api","key_id":"admin-key","credential_store":"admin-credentials","session_store":"admin-sessions","challenge_kind"`, `"issuer":"campusos-admin","issuer":"campusos-user","audience":"admin-api","key_id":"admin-key","credential_store":"admin-credentials","session_store":"admin-sessions","challenge_kind"`, 1),
		"escaped_duplicate":    strings.Replace(identityDomainsJSON, `"status":"active"`, `"status":"active","statu\u0073":"revoked"`, 1),
		"unknown_field":        strings.Replace(identityDomainsJSON, `"status":"active"`, `"status":"active","super":true`, 1),
		"missing_request":      strings.Replace(identityDomainsJSON, `"request":{`, `"request2":{`, 1),
		"null_operation":       strings.Replace(identityDomainsJSON, `"status":"active"`, `"status":"active","operation":null`, 1),
		"null_session_store":   strings.Replace(identityDomainsJSON, `"session_store":"admin-sessions","challenge_kind"`, `"session_store":null,"challenge_kind"`, 1),
		"empty_recovery_store": strings.Replace(identityDomainsJSON, `"session_store":"admin-sessions","challenge_kind"`, `"session_store":"admin-sessions","recovery_store":"","challenge_kind"`, 1),
		"unknown_kind":         strings.Replace(identityDomainsJSON, `"kind":"admin"`, `"kind":"integration"`, 1),
		"unknown_challenge":    strings.Replace(identityDomainsJSON, `"challenge_kind":"access"`, `"challenge_kind":"password_reset_any"`, 1),
		"unknown_status":       strings.Replace(identityDomainsJSON, `"status":"active"`, `"status":"legacy"`, 1),
		"unknown_activation":   strings.Replace(identityDomainsJSON, `"activation_source":"direct"`, `"activation_source":"user_admin_role"`, 1),
		"unknown_operation":    strings.Replace(identityDomainsJSON, `"request":{`, `"operation":{"kind":"assign_user_role","actor_kind":"admin","actor_mfa":true,"target_admin_id":"same-42","target_is_security_admin":false,"active_security_admins":2,"bootstrap_enabled":false,"audit_reason":"x"},"request":{`, 1),
		"negative_admins":      strings.Replace(identityDomainsJSON, `"request":{`, `"operation":{"kind":"create","actor_kind":"admin","actor_mfa":true,"target_admin_id":"same-42","target_is_security_admin":false,"active_security_admins":-1,"bootstrap_enabled":false,"audit_reason":"x"},"request":{`, 1),
		"mfa_string":           strings.Replace(identityDomainsJSON, `"request":{`, `"operation":{"kind":"create","actor_kind":"admin","actor_mfa":"true","target_admin_id":"same-42","target_is_security_admin":false,"active_security_admins":2,"bootstrap_enabled":false,"audit_reason":"x"},"request":{`, 1),
		"wrong_contract":       strings.Replace(identityDomainsJSON, `"campusos.identity-domains/v1"`, `"campusos.identity-domains/v2"`, 1),
		"missing_contract":     strings.Replace(identityDomainsJSON, `"contract":"campusos.identity-domains/v1",`, ``, 1),
		"trailing":             identityDomainsJSON + " {}",
		"oversized":            strings.Repeat(" ", 32768) + identityDomainsJSON,
		"invalid_utf8":         strings.Replace(identityDomainsJSON, `"account_id":"same-42"`, "\"account_id\":\"\xff\"", 1),
		"lone_low_surrogate":   strings.Replace(identityDomainsJSON, `"account_id":"same-42"`, `"account_id":"\udfff"`, 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := ParseIdentityDomainInputJSON([]byte(raw))
			if !errors.Is(err, ErrIdentityDomainInvalid) || !reflect.DeepEqual(r, IdentityDomainInput{}) {
				t.Fatalf("invalid input escaped: %v", err)
			}
		})
	}
}
