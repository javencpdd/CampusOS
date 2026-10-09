package port

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const scopeDelegationJSON = `{"contract":"campusos.scope-delegation/v1","actor":{"kind":"admin","id":"admin-1","authentication_strength":"mfa"},"management":{"action":"plugin.grant.manage","active":true,"expires_at_ms":5000},"bound":{"action":"knowledge.source.read","scope":{"kind":"public_collection","ids":["public-a","public-b"]},"not_before_ms":1000,"expires_at_ms":4000,"required_strength":"password","status":"active","delegable":true},"candidate":{"action":"knowledge.source.read","scope":{"kind":"public_collection","ids":["public-a"]},"not_before_ms":1500,"expires_at_ms":3000,"required_strength":"mfa","recipient":{"kind":"plugin","id":"plugin-1"}},"now_ms":1500,"facts_version":"rev-1"}`

func TestScopeDelegationCodecRoundTrip(t *testing.T) {
	for _, raw := range []string{
		scopeDelegationJSON,
		strings.Replace(scopeDelegationJSON, `"now_ms":1500`, `"now_ms":1.5e3`, 1),
		strings.Replace(scopeDelegationJSON, `"id":"plugin-1"`, `"id":"插件😀"`, 1),
		strings.Replace(scopeDelegationJSON, `"kind":"public_collection","ids":["public-a"]`, `"kind":"endpoint","ids":["https://hooks.example.test:8443"]`, 1),
	} {
		r, err := ParseScopeDelegationInputJSON([]byte(raw))
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
	// Ajv counts string length in UTF-16 code units; an astral character is 2.
	for _, n := range []int{64, 65, 128} {
		id := strings.Repeat("😀", n)
		raw := strings.Replace(scopeDelegationJSON, `"id":"plugin-1"`, `"id":"`+id+`"`, 1)
		_, err := ParseScopeDelegationInputJSON([]byte(raw))
		if (err == nil) != (2*n <= 128) {
			t.Fatalf("UTF-16 units %d: %v", 2*n, err)
		}
	}
}

func TestScopeDelegationCodecRejectsAmbiguousInput(t *testing.T) {
	cases := map[string]string{
		"duplicate":           strings.Replace(scopeDelegationJSON, `"facts_version":"rev-1"`, `"facts_version":"other","facts_version":"rev-1"`, 1),
		"nested_duplicate":    strings.Replace(scopeDelegationJSON, `"ids":["public-a"]`, `"ids":["public-c"],"ids":["public-a"]`, 1),
		"escaped_duplicate":   strings.Replace(scopeDelegationJSON, `"ids":["public-a"]`, `"i\u0064s":["public-c"],"ids":["public-a"]`, 1),
		"unknown_field":       strings.Replace(scopeDelegationJSON, `"facts_version":"rev-1"`, `"facts_version":"rev-1","secret":"token"`, 1),
		"case_alias":          strings.Replace(scopeDelegationJSON, `"active":true`, `"Active":true`, 1),
		"missing_now":         strings.Replace(scopeDelegationJSON, `"now_ms":1500,`, ``, 1),
		"null_now":            strings.Replace(scopeDelegationJSON, `"now_ms":1500`, `"now_ms":null`, 1),
		"null_scope":          strings.Replace(scopeDelegationJSON, `"scope":{"kind":"public_collection","ids":["public-a"]}`, `"scope":null`, 1),
		"numeric_id":          strings.Replace(scopeDelegationJSON, `"id":"plugin-1"`, `"id":7`, 1),
		"empty_id":            strings.Replace(scopeDelegationJSON, `"id":"plugin-1"`, `"id":""`, 1),
		"unknown_actor_kind":  strings.Replace(scopeDelegationJSON, `"actor":{"kind":"admin"`, `"actor":{"kind":"root"`, 1),
		"unknown_strength":    strings.Replace(scopeDelegationJSON, `"authentication_strength":"mfa"`, `"authentication_strength":"workload"`, 1),
		"unknown_management":  strings.Replace(scopeDelegationJSON, `"action":"plugin.grant.manage"`, `"action":"host.db.query"`, 1),
		"unknown_action":      strings.Replace(scopeDelegationJSON, `"action":"knowledge.source.read","scope":{"kind":"public_collection","ids":["public-a"]}`, `"action":"host.db.query","scope":{"kind":"public_collection","ids":["public-a"]}`, 1),
		"unknown_scope_kind":  strings.Replace(scopeDelegationJSON, `"kind":"public_collection","ids":["public-a"]`, `"kind":"all","ids":["public-a"]`, 1),
		"unknown_recipient":   strings.Replace(scopeDelegationJSON, `"recipient":{"kind":"plugin"`, `"recipient":{"kind":"anonymous"`, 1),
		"ids_empty":           strings.Replace(scopeDelegationJSON, `"ids":["public-a"]`, `"ids":[]`, 1),
		"ids_duplicate":       strings.Replace(scopeDelegationJSON, `"ids":["public-a"]`, `"ids":["public-a","public-a"]`, 1),
		"ids_null":            strings.Replace(scopeDelegationJSON, `"ids":["public-a"]`, `"ids":null`, 1),
		"ids_null_item":       strings.Replace(scopeDelegationJSON, `"ids":["public-a"]`, `"ids":[null]`, 1),
		"ids_33":              strings.Replace(scopeDelegationJSON, `"ids":["public-a"]`, `"ids":[`+strings.Repeat(`"x",`, 32)+`"x"]`, 1),
		"delegable_string":    strings.Replace(scopeDelegationJSON, `"delegable":true`, `"delegable":"true"`, 1),
		"active_string":       strings.Replace(scopeDelegationJSON, `"active":true`, `"active":"true"`, 1),
		"time_fractional":     strings.Replace(scopeDelegationJSON, `"now_ms":1500`, `"now_ms":1500.5`, 1),
		"time_negative":       strings.Replace(scopeDelegationJSON, `"now_ms":1500`, `"now_ms":-1`, 1),
		"time_zero_expiry":    strings.Replace(scopeDelegationJSON, `"expires_at_ms":5000`, `"expires_at_ms":0`, 1),
		"time_overflow":       strings.Replace(scopeDelegationJSON, `"now_ms":1500`, `"now_ms":1e999999999`, 1),
		"time_string":         strings.Replace(scopeDelegationJSON, `"now_ms":1500`, `"now_ms":"1500"`, 1),
		"lone_high_surrogate": strings.Replace(scopeDelegationJSON, `"id":"plugin-1"`, `"id":"\ud800"`, 1),
		"invalid_utf8":        strings.Replace(scopeDelegationJSON, `"id":"plugin-1"`, "\"id\":\"\xff\"", 1),
		"trailing":            scopeDelegationJSON + " {}",
		"oversized":           strings.Repeat(" ", 32768) + scopeDelegationJSON,
		"wrong_contract":      strings.Replace(scopeDelegationJSON, `"campusos.scope-delegation/v1"`, `"campusos.scope-delegation/v2"`, 1),
		"missing_contract":    strings.Replace(scopeDelegationJSON, `"contract":"campusos.scope-delegation/v1",`, ``, 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := ParseScopeDelegationInputJSON([]byte(raw))
			if !errors.Is(err, ErrScopeInputInvalid) || !reflect.DeepEqual(r, ScopeDelegationInput{}) {
				t.Fatalf("invalid input escaped: %v", err)
			}
		})
	}
}

func TestScopeDelegationTimePrecision(t *testing.T) {
	for _, c := range []struct {
		token string
		want  int64
		valid bool
	}{
		{"0", 0, true}, {"1e3", 1000, true}, {"1000.000", 1000, true}, {"9007199254740993", 9007199254740993, true},
		{"9223372036854775807", 9223372036854775807, true}, {"9223372036854775808", 0, false}, {"-1", 0, false},
		{"1000.00000000000001", 0, false}, {`"1000"`, 0, false}, {"null", 0, false},
	} {
		t.Run(c.token, func(t *testing.T) {
			raw := strings.Replace(scopeDelegationJSON, `"now_ms":1500`, `"now_ms":`+c.token, 1)
			r, err := ParseScopeDelegationInputJSON([]byte(raw))
			if (err == nil) != c.valid || c.valid && r.NowMS != c.want {
				t.Fatalf("now_ms got %d %v", r.NowMS, err)
			}
		})
	}
}
