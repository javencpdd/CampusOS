package plugin

import "testing"

func TestSecretBindingScopeRequiresExactGrant(t *testing.T) {
	approved := map[string]interface{}{
		"secret_name": "mail.password",
		"secret_ref":  "secret-ref:mail-old",
		"profile_id":  "mail-primary",
		"target_url":  "https://mailer.example.edu/v1/send",
		"purpose":     "notify-course",
	}
	requested := map[string]interface{}{"scope": "system", "secret_binding": approved}
	grant := map[string]interface{}{"scope": "system", "secret_bindings": []interface{}{approved}}
	if !scopeContains(grant, requested) {
		t.Fatal("exact approved binding denied")
	}
	for field, changed := range map[string]interface{}{
		"secret_name": "other.password",
		"secret_ref":  "secret-ref:other",
		"profile_id":  "other-profile",
		"target_url":  "https://mailer.example.edu/v1/echo",
		"purpose":     "export-private-data",
	} {
		mismatched := map[string]interface{}{}
		for key, value := range approved {
			mismatched[key] = value
		}
		mismatched[field] = changed
		if scopeContains(grant, map[string]interface{}{"scope": "system", "secret_binding": mismatched}) {
			t.Fatalf("binding with changed %s was allowed", field)
		}
	}
	for _, malformed := range []map[string]interface{}{
		nil,
		{"scope": "system"},
		{"secret_bindings": approved},
		{"secret_bindings": []interface{}{}},
		{"secret_bindings": []interface{}{map[string]interface{}{"secret_ref": "secret-ref:mail-old"}}},
	} {
		if scopeContains(malformed, requested) {
			t.Fatalf("malformed grant was allowed: %+v", malformed)
		}
	}
}
