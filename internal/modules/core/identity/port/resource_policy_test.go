package port

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

const resourcePolicyJSON = `{"contract":"campusos.resource-policy/v1","request_id":"req1","principal":{"contract":"campusos.principal/v1","actor":{"kind":"user","id":"u1"},"audience":"user","authentication_strength":"mfa","credential_id":"c1"},"action":"community.thread.take_down","facts":{"kind":"thread","id":"t1","owner_id":"u1","board_id":"b1","status":"published","publication_status":"published","version":"r1"},"grants":[{"subject_kind":"user","subject_id":"u1","action":"community.thread.take_down","board_id":"b1","expires_at_ms":2000,"status":"active","required_strength":"mfa"}],"evaluated_at_ms":1000,"policy_version":"policy-1"}`

func TestResourcePolicyCodecRoundTrip(t *testing.T) {
	for _, raw := range []string{resourcePolicyJSON,
		strings.Replace(resourcePolicyJSON, `"id":"t1"`, `"id":"校园😀"`, 1),
		strings.Replace(resourcePolicyJSON, `"id":"t1"`, `"id":"\ud83d\ude00"`, 1),
		strings.Replace(resourcePolicyJSON, `"id":"t1"`, `"id":"\\ud800"`, 1),
		strings.Replace(resourcePolicyJSON, `"version":"r1"`, `"origin_visible":false,"version":"r1"`, 1),
	} {
		r, err := ParseResourcePolicyRequestJSON([]byte(raw))
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
	// Missing resource owner is structurally legal; the evaluator must deny it.
	raw := strings.Replace(resourcePolicyJSON, `"owner_id":"u1",`, "", 1)
	if _, err := ParseResourcePolicyRequestJSON([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	// The resource contract measures Unicode characters rather than UTF-8 bytes.
	for _, n := range []int{128, 129} {
		raw := strings.Replace(resourcePolicyJSON, `"id":"t1"`, `"id":"`+strings.Repeat("校", n)+`"`, 1)
		_, err := ParseResourcePolicyRequestJSON([]byte(raw))
		if (err == nil) != (n == 128) {
			t.Fatalf("Unicode length %d: %v", n, err)
		}
	}
}

func TestResourcePolicyCodecRejectsAmbiguousInput(t *testing.T) {
	cases := map[string]string{
		"duplicate":              strings.Replace(resourcePolicyJSON, `"request_id":"req1"`, `"request_id":"other","request_id":"req1"`, 1),
		"nested_duplicate":       strings.Replace(resourcePolicyJSON, `"owner_id":"u1"`, `"owner_id":"u2","owner_id":"u1"`, 1),
		"escaped_duplicate":      strings.Replace(resourcePolicyJSON, `"owner_id":"u1"`, `"owner_id":"u2","owner_\u0069d":"u1"`, 1),
		"unknown":                strings.Replace(resourcePolicyJSON, `"policy_version":"policy-1"`, `"allowed":true,"policy_version":"policy-1"`, 1),
		"case_alias":             strings.Replace(resourcePolicyJSON, `"status":"published"`, `"Status":"published"`, 1),
		"missing_time":           strings.Replace(resourcePolicyJSON, `"evaluated_at_ms":1000,`, "", 1),
		"null_time":              strings.Replace(resourcePolicyJSON, `"evaluated_at_ms":1000`, `"evaluated_at_ms":null`, 1),
		"null_grants":            strings.Replace(resourcePolicyJSON, `"grants":[`, `"grants":null,"surplus":[`, 1),
		"null_origin":            strings.Replace(resourcePolicyJSON, `"version":"r1"`, `"origin_visible":null,"version":"r1"`, 1),
		"empty_optional":         strings.Replace(resourcePolicyJSON, `"owner_id":"u1"`, `"owner_id":""`, 1),
		"null_optional":          strings.Replace(resourcePolicyJSON, `"owner_id":"u1"`, `"owner_id":null`, 1),
		"numeric_id":             strings.Replace(resourcePolicyJSON, `"id":"t1"`, `"id":1`, 1),
		"unknown_status":         strings.Replace(resourcePolicyJSON, `"status":"published"`, `"status":"visible"`, 1),
		"grant_missing_strength": strings.Replace(resourcePolicyJSON, `,"required_strength":"mfa"`, "", 1),
		"grant_extra_scope":      strings.Replace(resourcePolicyJSON, `"required_strength":"mfa"`, `"required_strength":"mfa","global":true`, 1),
		"grant_wrong_type":       strings.Replace(resourcePolicyJSON, `"expires_at_ms":2000`, `"expires_at_ms":"2000"`, 1),
		"unknown_contract":       strings.Replace(resourcePolicyJSON, ResourcePolicyContract, "future", 1),
		"lone_high_surrogate":    strings.Replace(resourcePolicyJSON, `"id":"t1"`, `"id":"\ud800"`, 1),
		"lone_low_surrogate":     strings.Replace(resourcePolicyJSON, `"id":"t1"`, `"id":"\udfff"`, 1),
		"invalid_utf8":           strings.Replace(resourcePolicyJSON, `"id":"t1"`, "\"id\":\"\xff\"", 1),
		"trailing":               resourcePolicyJSON + " {}",
		"oversized":              strings.Repeat(" ", 32768) + resourcePolicyJSON,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := ParseResourcePolicyRequestJSON([]byte(raw))
			if !errors.Is(err, ErrResourcePolicyInvalid) || !reflect.DeepEqual(r, ResourcePolicyRequest{}) {
				t.Fatalf("invalid request escaped: %v", err)
			}
		})
	}
	for _, raw := range []string{`{}`, `{"action":"unknown"}`, strings.Replace(resourcePolicyJSON, `"action":"community.thread.take_down"`, `"action":"unknown"`, 1)} {
		if _, err := ParseResourcePolicyRequestJSON([]byte(raw)); !errors.Is(err, ErrResourcePolicyUnknownAction) {
			t.Fatalf("action precedence: %v", err)
		}
	}
	r, err := ParseResourcePolicyRequestJSON([]byte(resourcePolicyJSON))
	if err != nil {
		t.Fatal(err)
	}
	r.Grants = make([]ResourcePolicyGrant, 65)
	for i := range r.Grants {
		r.Grants[i] = ResourcePolicyGrant{SubjectKind: "user", SubjectID: "u1", Action: r.Action, BoardID: "b1", ExpiresAtMS: 2000, Status: "active", RequiredStrength: "mfa"}
	}
	raw, _ := json.Marshal(r)
	if _, err := ParseResourcePolicyRequestJSON(raw); !errors.Is(err, ErrResourcePolicyInvalid) {
		t.Fatal("65 grants accepted")
	}
	r.Grants = r.Grants[:64]
	raw, _ = json.Marshal(r)
	if _, err := ParseResourcePolicyRequestJSON(raw); err != nil {
		t.Fatal("64 grants rejected", err)
	}
}

func TestResourcePolicyTimePrecision(t *testing.T) {
	for _, c := range []struct {
		token string
		want  int64
		valid bool
	}{
		{"0", 0, true}, {"1e3", 1000, true}, {"1000.000", 1000, true}, {"100000e-2", 1000, true},
		{"9007199254740993", 9007199254740993, true}, {"9223372036854775807", math.MaxInt64, true},
		{"9223372036854775808", 0, false}, {"1000.00000000000001", 0, false}, {"-1", 0, false},
		{"1e999999999", 0, false}, {"1e-999999999", 0, false}, {`"1000"`, 0, false}, {"null", 0, false},
	} {
		t.Run(c.token, func(t *testing.T) {
			raw := strings.Replace(resourcePolicyJSON, `"evaluated_at_ms":1000`, `"evaluated_at_ms":`+c.token, 1)
			r, err := ParseResourcePolicyRequestJSON([]byte(raw))
			if (err == nil) != c.valid || c.valid && r.EvaluatedAtMS != c.want {
				t.Fatalf("time got %d %v", r.EvaluatedAtMS, err)
			}
		})
	}
	for _, token := range []string{"0", "-1", "1.1", "9223372036854775808"} {
		raw := strings.Replace(resourcePolicyJSON, `"expires_at_ms":2000`, `"expires_at_ms":`+token, 1)
		if _, err := ParseResourcePolicyRequestJSON([]byte(raw)); !errors.Is(err, ErrResourcePolicyInvalid) {
			t.Fatalf("invalid grant expiry %s", token)
		}
	}
}

func TestResourcePolicyDecisionCodec(t *testing.T) {
	base := `{"request_id":"req1","effect":"allow","reason":"ALLOW","facts_version":"r1","policy_version":"policy-1","obligations":["recheck_facts","required_audit"]}`
	if _, err := ParseResourcePolicyDecisionJSON([]byte(base)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		strings.Replace(base, `"ALLOW"`, `"policy.scope_denied"`, 1),
		strings.Replace(base, `"allow"`, `"deny"`, 1),
		strings.Replace(base, `["recheck_facts","required_audit"]`, `[]`, 1),
		strings.Replace(base, `["recheck_facts","required_audit"]`, `null`, 1),
		strings.Replace(base, `"required_audit"`, `"recheck_facts"`, 1),
		strings.Replace(base, `"required_audit"`, `"optional_audit"`, 1),
		strings.Replace(base, `"request_id":"req1"`, `"request_id":"req1","Request_ID":"other"`, 1),
		strings.Replace(base, `"facts_version":"r1"`, `"facts_version":"\ud800"`, 1),
		base + " {}",
	} {
		if _, err := ParseResourcePolicyDecisionJSON([]byte(raw)); !errors.Is(err, ErrResourcePolicyInvalid) {
			t.Fatalf("invalid decision: %v", err)
		}
	}
}
