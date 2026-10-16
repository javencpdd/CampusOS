package port

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func integrationAccessFixture() IntegrationAccessInput {
	return IntegrationAccessInput{
		Contract: IntegrationAccessContract, RequestID: "request.1", NowMS: 1000,
		Principal: PrincipalContext{Contract: PrincipalContract, Actor: PrincipalRef{Kind: "integration", ID: "same-42"}, Audience: "integration_api", AuthenticationStrength: "credential", CredentialID: "credential.1"},
		Target:    IntegrationAccessTarget{Channel: "integration_http", Endpoint: "public.sources", Action: "knowledge.source.read", Resource: PolicyResourceRef{Kind: "public_collection", ID: "collection.1"}},
		Facts: IntegrationAccessFacts{
			IntegrationID: "same-42", CredentialID: "credential.1", PrincipalStatus: "active", CredentialStatus: "active", CredentialAudience: "integration_api", NotBeforeMS: 100, ExpiresAtMS: 2000, FactsVersion: "facts.1",
			Endpoint: IntegrationAccessEndpoint{Channel: "integration_http", ID: "public.sources", Action: "knowledge.source.read", ResourceKind: "public_collection", Enabled: true, IntegrationAllowed: true},
			Rules:    []IntegrationAccessRule{{ID: "rule.1", Channel: "integration_http", Endpoint: "public.sources", Action: "knowledge.source.read", ResourceKind: "public_collection", ResourceIDs: []string{"collection.1"}, Status: "active", NotBeforeMS: 100, ExpiresAtMS: 1800}},
		},
	}
}

func integrationAccessWire(t *testing.T, r IntegrationAccessInput) string {
	t.Helper()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestIntegrationAccessCodecExactSchemaAndSafeIntegers(t *testing.T) {
	r := integrationAccessFixture()
	wire := integrationAccessWire(t, r)
	for _, raw := range []string{wire, strings.Replace(wire, `"now_ms":1000`, `"now_ms":1e3`, 1), strings.Replace(wire, `"not_before_ms":100`, `"not_before_ms":100.0`, 1)} {
		got, err := ParseIntegrationAccessInputJSON([]byte(raw))
		if err != nil || !reflect.DeepEqual(got, r) {
			t.Fatal("exact round trip", got, err)
		}
	}
	r.NowMS, r.Facts.NotBeforeMS, r.Facts.ExpiresAtMS = 9007199254740990, 9007199254740989, 9007199254740991
	got, err := ParseIntegrationAccessInputJSON([]byte(integrationAccessWire(t, r)))
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatal("safe integer boundary", err)
	}
	r.Facts.Rules = []IntegrationAccessRule{}
	if _, err := ParseIntegrationAccessInputJSON([]byte(integrationAccessWire(t, r))); err != nil {
		t.Fatal("an explicit empty inventory is a valid deny input", err)
	}
}

func TestIntegrationAccessCodecRejectsAmbiguousInput(t *testing.T) {
	wire := integrationAccessWire(t, integrationAccessFixture())
	cases := map[string]string{
		"root_duplicate":         strings.Replace(wire, `"request_id":"request.1"`, `"request_id":"request.1","request_id":"other"`, 1),
		"escaped_duplicate":      strings.Replace(wire, `"request_id":"request.1"`, `"request_id":"request.1","request_\u0069d":"other"`, 1),
		"nested_duplicate":       strings.Replace(wire, `"enabled":true`, `"enabled":true,"enabled":false`, 1),
		"extra_root":             strings.TrimSuffix(wire, "}") + `,"token":"sentinel-secret"}`,
		"extra_target":           strings.Replace(wire, `"resource":{`, `"untrusted_owner":"sentinel-secret","resource":{`, 1),
		"extra_rule":             strings.Replace(wire, `"resource_ids":[`, `"allow":true,"resource_ids":[`, 1),
		"extra_resource":         strings.Replace(wire, `"kind":"public_collection"`, `"kind":"public_collection","owner":"caller"`, 1),
		"null_principal":         strings.Replace(wire, `"principal":{`, `"principal":null,"unknown":{`, 1),
		"null_rules":             strings.Replace(wire, `"rules":[{`, `"rules":null,"unknown":[{`, 1),
		"null_flag":              strings.Replace(wire, `"enabled":true`, `"enabled":null`, 1),
		"missing_flag":           strings.Replace(wire, `"enabled":true,`, ``, 1),
		"wrong_flag":             strings.Replace(wire, `"enabled":true`, `"enabled":"true"`, 1),
		"null_resource_ids":      strings.Replace(wire, `"resource_ids":["collection.1"]`, `"resource_ids":null`, 1),
		"duplicate_ids":          strings.Replace(wire, `"resource_ids":["collection.1"]`, `"resource_ids":["collection.1","collection.1"]`, 1),
		"null_id":                strings.Replace(wire, `"resource_ids":["collection.1"]`, `"resource_ids":[null]`, 1),
		"empty_ids":              strings.Replace(wire, `"resource_ids":["collection.1"]`, `"resource_ids":[]`, 1),
		"wildcard":               strings.Replace(wire, `"resource_ids":["collection.1"]`, `"resource_ids":["*"]`, 1),
		"unknown_contract":       strings.Replace(wire, IntegrationAccessContract, "campusos.integration-access/v2", 1),
		"unknown_status":         strings.Replace(wire, `"principal_status":"active"`, `"principal_status":"unknown"`, 1),
		"fractional":             strings.Replace(wire, `"now_ms":1000`, `"now_ms":1000.25`, 1),
		"negative":               strings.Replace(wire, `"now_ms":1000`, `"now_ms":-1`, 1),
		"unsafe_integer":         strings.Replace(wire, `"now_ms":1000`, `"now_ms":9007199254740992`, 1),
		"rounded_fraction":       strings.Replace(wire, `"now_ms":1000`, `"now_ms":9007199254740990.5`, 1),
		"huge_exponent":          strings.Replace(wire, `"now_ms":1000`, `"now_ms":1e99999999`, 1),
		"wrong_principal_domain": strings.Replace(wire, `"audience":"integration_api"`, `"audience":"admin"`, 1),
		"unpaired_surrogate":     strings.Replace(wire, `"request.1"`, `"\ud800"`, 1),
		"invalid_utf8":           strings.Replace(wire, `request.1`, "bad\xff", 1),
		"trailing_value":         wire + `{}`,
		"too_large":              wire + strings.Repeat(" ", 32769),
		"too_deep":               `{"unknown":` + strings.Repeat("[", 70) + "0" + strings.Repeat("]", 70) + "}",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseIntegrationAccessInputJSON([]byte(raw))
			if !errors.Is(err, ErrIntegrationInputInvalid) || err.Error() != "integration.input_invalid" {
				t.Fatal("not a safe exact rejection", err)
			}
		})
	}
}

func TestIntegrationAccessTypedValidationBounds(t *testing.T) {
	mutations := map[string]func(*IntegrationAccessInput){
		"nil_rules":                func(r *IntegrationAccessInput) { r.Facts.Rules = nil },
		"duplicate_rules":          func(r *IntegrationAccessInput) { r.Facts.Rules = append(r.Facts.Rules, r.Facts.Rules[0]) },
		"too_many_rules":           func(r *IntegrationAccessInput) { r.Facts.Rules = make([]IntegrationAccessRule, 65) },
		"too_many_ids":             func(r *IntegrationAccessInput) { r.Facts.Rules[0].ResourceIDs = make([]string, 33) },
		"long_id":                  func(r *IntegrationAccessInput) { r.Target.Resource.ID = strings.Repeat("x", 129) },
		"missing_version":          func(r *IntegrationAccessInput) { r.Facts.FactsVersion = "" },
		"invalid_window":           func(r *IntegrationAccessInput) { r.Facts.ExpiresAtMS = r.Facts.NotBeforeMS },
		"negative_window":          func(r *IntegrationAccessInput) { r.Facts.NotBeforeMS = -1 },
		"unsafe_expiry":            func(r *IntegrationAccessInput) { r.Facts.ExpiresAtMS = 9007199254740992 },
		"invalid_credential_state": func(r *IntegrationAccessInput) { r.Facts.CredentialStatus = "suspended" },
		"invalid_subject": func(r *IntegrationAccessInput) {
			r.Principal.EffectiveSubject = &PrincipalRef{Kind: "user", ID: "same-42"}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			r := integrationAccessFixture()
			mutate(&r)
			if !errors.Is(r.Validate(), ErrIntegrationInputInvalid) {
				t.Fatal("typed invalid input accepted")
			}
			if _, err := ParseIntegrationAccessInputJSON([]byte(integrationAccessWire(t, r))); err == nil {
				t.Fatal("wire invalid input accepted")
			}
		})
	}
}

func TestIntegrationAccessCloneDetachedInventories(t *testing.T) {
	r := integrationAccessFixture()
	r.Principal.EffectiveSubject = &PrincipalRef{Kind: "user", ID: "owner.1"}
	c := r.Clone()
	r.Facts.Rules[0].ResourceIDs[0] = "other"
	r.Facts.Rules[0].ID = "other"
	r.Principal.EffectiveSubject.ID = "other"
	if c.Facts.Rules[0].ResourceIDs[0] != "collection.1" || c.Facts.Rules[0].ID != "rule.1" || c.Principal.EffectiveSubject.ID != "owner.1" {
		t.Fatal("private snapshot aliases mutable input")
	}
}
