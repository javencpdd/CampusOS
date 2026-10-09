package port

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const boardDelegationJSON = `{"contract":"campusos.board-delegation/v1","request_id":"request-1","facts_version":"facts-1","evaluated_at":1000,"principal":{"contract":"campusos.principal/v1","actor":{"kind":"admin","id":"admin-1"},"audience":"admin","authentication_strength":"mfa","credential_id":"credential-1"},"management":{"actor":{"kind":"admin","id":"admin-1"},"action":"identity.role.assign","status":"active","not_before":900,"expires_at":2000},"recipient":{"subject":{"kind":"user","id":"user-1"},"status":"active"},"boards":[{"kind":"community.board","id":"board-A","status":"active"},{"kind":"community.board","id":"board-B","status":"active"}],"bounds":[{"action":"community.thread.take_down","board_id":"board-A","not_before":900,"expires_at":2000,"required_strength":"mfa","id":"bound-1","actor":{"kind":"admin","id":"admin-1"},"status":"active","delegable":true}],"candidate":{"recipient":{"kind":"user","id":"user-1"},"grants":[{"action":"community.thread.take_down","board_id":"board-A","not_before":1100,"expires_at":1800,"required_strength":"mfa"}]}}`

func TestBoardDelegationCodecRoundTrip(t *testing.T) {
	for _, raw := range []string{
		boardDelegationJSON,
		strings.Replace(boardDelegationJSON, `"evaluated_at":1000`, `"evaluated_at":1e3`, 1),
		strings.Replace(boardDelegationJSON, `"evaluated_at":1000`, `"evaluated_at":1000.000`, 1),
		strings.Replace(boardDelegationJSON, `"evaluated_at":1000`, `"evaluated_at":253402300799`, 1),
		strings.Replace(boardDelegationJSON, `"expires_at":1800,"required_strength":"mfa"}]}`, `"expires_at":1800,"required_strength":"mfa"},{"action":"community.post.delete","board_id":"board-B","not_before":1000,"expires_at":1500,"required_strength":"password"}]}`, 1),
	} {
		r, err := ParseBoardDelegationRequestJSON([]byte(raw))
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
	// An empty bounds set is structurally legal; the evaluator must deny it.
	empty := strings.Replace(boardDelegationJSON, `"bounds":[{"action":"community.thread.take_down","board_id":"board-A","not_before":900,"expires_at":2000,"required_strength":"mfa","id":"bound-1","actor":{"kind":"admin","id":"admin-1"},"status":"active","delegable":true}]`, `"bounds":[]`, 1)
	r, err := ParseBoardDelegationRequestJSON([]byte(empty))
	if err != nil || len(r.Bounds) != 0 || r.Bounds == nil {
		t.Fatalf("empty bounds: %+v %v", r.Bounds, err)
	}
}

func TestBoardDelegationCodecRejectsAmbiguousInput(t *testing.T) {
	cases := map[string]string{
		"duplicate":             strings.Replace(boardDelegationJSON, `"request_id":"request-1"`, `"request_id":"other","request_id":"request-1"`, 1),
		"nested_duplicate":      strings.Replace(boardDelegationJSON, `"board_id":"board-A","not_before":900`, `"board_id":"board-B","board_id":"board-A","not_before":900`, 1),
		"escaped_duplicate":     strings.Replace(boardDelegationJSON, `"board_id":"board-A","not_before":1100`, `"board_id":"board-B","boar\u0064_id":"board-A","not_before":1100`, 1),
		"unknown_field":         strings.Replace(boardDelegationJSON, `"facts_version":"facts-1"`, `"facts_version":"facts-1","trusted":true`, 1),
		"case_alias":            strings.Replace(boardDelegationJSON, `"status":"active","not_before":900`, `"Status":"active","not_before":900`, 1),
		"missing_time":          strings.Replace(boardDelegationJSON, `"evaluated_at":1000,`, ``, 1),
		"null_time":             strings.Replace(boardDelegationJSON, `"evaluated_at":1000`, `"evaluated_at":null`, 1),
		"null_bounds":           strings.Replace(boardDelegationJSON, `"bounds":[`, `"bounds":null,"surplus":[`, 1),
		"numeric_board":         strings.Replace(boardDelegationJSON, `"board_id":"board-A","not_before":1100`, `"board_id":7,"not_before":1100`, 1),
		"empty_board_id":        strings.Replace(boardDelegationJSON, `"board_id":"board-A","not_before":1100`, `"board_id":"","not_before":1100`, 1),
		"unknown_action":        strings.Replace(boardDelegationJSON, `"action":"community.thread.take_down","board_id":"board-A","not_before":1100`, `"action":"community.thread.view_all","board_id":"board-A","not_before":1100`, 1),
		"unknown_strength":      strings.Replace(boardDelegationJSON, `"required_strength":"mfa"}]}`, `"required_strength":"workload"}]}`, 1),
		"delegable_string":      strings.Replace(boardDelegationJSON, `"delegable":true`, `"delegable":"true"`, 1),
		"delegable_missing":     strings.Replace(boardDelegationJSON, `,"delegable":true`, ``, 1),
		"bound_extra_field":     strings.Replace(boardDelegationJSON, `"delegable":true`, `"delegable":true,"scope":"global"`, 1),
		"management_action":     strings.Replace(boardDelegationJSON, `"action":"identity.role.assign"`, `"action":"community.thread.take_down"`, 1),
		"management_actor_user": strings.Replace(boardDelegationJSON, `"management":{"actor":{"kind":"admin"`, `"management":{"actor":{"kind":"user"`, 1),
		"recipient_admin":       strings.Replace(boardDelegationJSON, `"recipient":{"subject":{"kind":"user"`, `"recipient":{"subject":{"kind":"admin"`, 1),
		"recipient_status":      strings.Replace(boardDelegationJSON, `"recipient":{"subject":{"kind":"user","id":"user-1"},"status":"active"}`, `"recipient":{"subject":{"kind":"user","id":"user-1"},"status":"archived"}`, 1),
		"board_kind":            strings.Replace(boardDelegationJSON, `"kind":"community.board","id":"board-A"`, `"kind":"community.group","id":"board-A"`, 1),
		"board_status":          strings.Replace(boardDelegationJSON, `"id":"board-A","status":"active"`, `"id":"board-A","status":"deleted"`, 1),
		"duplicate_board_id":    strings.Replace(boardDelegationJSON, `"id":"board-B","status":"active"`, `"id":"board-A","status":"archived"`, 1),
		"reversed_management":   strings.Replace(boardDelegationJSON, `"not_before":900,"expires_at":2000}`, `"not_before":2000,"expires_at":900}`, 1),
		"reversed_bound":        strings.Replace(boardDelegationJSON, `"not_before":900,"expires_at":2000,"required_strength":"mfa","id":"bound-1"`, `"not_before":2000,"expires_at":900,"required_strength":"mfa","id":"bound-1"`, 1),
		"reversed_candidate":    strings.Replace(boardDelegationJSON, `"not_before":1100,"expires_at":1800`, `"not_before":1800,"expires_at":1100`, 1),
		"time_too_large":        strings.Replace(boardDelegationJSON, `"evaluated_at":1000`, `"evaluated_at":253402300800`, 1),
		"time_negative":         strings.Replace(boardDelegationJSON, `"evaluated_at":1000`, `"evaluated_at":-1`, 1),
		"time_fractional":       strings.Replace(boardDelegationJSON, `"evaluated_at":1000`, `"evaluated_at":1000.5`, 1),
		"time_overflow":         strings.Replace(boardDelegationJSON, `"evaluated_at":1000`, `"evaluated_at":1e999999999`, 1),
		"time_string":           strings.Replace(boardDelegationJSON, `"evaluated_at":1000`, `"evaluated_at":"1000"`, 1),
		"lone_high_surrogate":   strings.Replace(boardDelegationJSON, `"id":"board-A","status"`, `"id":"board-A","x":"\ud800","status"`, 1),
		"invalid_utf8":          strings.Replace(boardDelegationJSON, `"id":"board-A","status"`, "\"id\":\"board-A\xff\",\"status\"", 1),
		"trailing":              boardDelegationJSON + " {}",
		"oversized":             strings.Repeat(" ", 32768) + boardDelegationJSON,
		"duplicate_candidate":   strings.Replace(boardDelegationJSON, `"expires_at":1800,"required_strength":"mfa"}]}`, `"expires_at":1800,"required_strength":"mfa"},{"action":"community.thread.take_down","board_id":"board-A","not_before":1200,"expires_at":1700,"required_strength":"password"}]}`, 1),
		"empty_candidate":       strings.Replace(boardDelegationJSON, `"grants":[{"action":"community.thread.take_down","board_id":"board-A","not_before":1100,"expires_at":1800,"required_strength":"mfa"}]`, `"grants":[]`, 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := ParseBoardDelegationRequestJSON([]byte(raw))
			if !errors.Is(err, ErrDelegationInputInvalid) || !reflect.DeepEqual(r, BoardDelegationRequest{}) {
				t.Fatalf("invalid request escaped: %v", err)
			}
		})
	}
	// An unknown contract string wins over every other input error; a missing
	// or non-string contract is plain input_invalid.
	for _, raw := range []string{
		`{"contract":"campusos.board-delegation/v2"}`,
		strings.Replace(boardDelegationJSON, `"campusos.board-delegation/v1"`, `"campusos.board-delegation/v2"`, 1),
	} {
		if _, err := ParseBoardDelegationRequestJSON([]byte(raw)); !errors.Is(err, ErrDelegationContractUnsupported) {
			t.Fatalf("contract precedence: %v", err)
		}
	}
	for _, raw := range []string{
		`{}`,
		`{"contract":null}`,
		`{"contract":7}`,
	} {
		if _, err := ParseBoardDelegationRequestJSON([]byte(raw)); !errors.Is(err, ErrDelegationInputInvalid) {
			t.Fatalf("missing/non-string contract: %v", err)
		}
	}
	// Duplicate candidate action/board pairs are invalid even with disjoint windows.
	r, err := ParseBoardDelegationRequestJSON([]byte(boardDelegationJSON))
	if err != nil {
		t.Fatal(err)
	}
	r.Candidate.Grants = append(r.Candidate.Grants, r.Candidate.Grants[0])
	if err = r.Validate(); !errors.Is(err, ErrDelegationInputInvalid) {
		t.Fatal("duplicate candidate pair accepted")
	}
	r, err = ParseBoardDelegationRequestJSON([]byte(boardDelegationJSON))
	if err != nil {
		t.Fatal(err)
	}
	r.Bounds = append(r.Bounds, r.Bounds[0])
	if err = r.Validate(); !errors.Is(err, ErrDelegationInputInvalid) {
		t.Fatal("duplicate bound ID accepted")
	}
}

func TestBoardDelegationTimePrecision(t *testing.T) {
	for _, c := range []struct {
		token string
		want  int64
		valid bool
	}{
		{"0", 0, true}, {"1e3", 1000, true}, {"1000.000", 1000, true}, {"100000e-2", 1000, true},
		{"253402300799", 253402300799, true}, {"253402300800", 0, false}, {"9223372036854775807", 0, false},
		{"-1", 0, false}, {"1000.00000000000001", 0, false}, {"1e999999999", 0, false}, {`"1000"`, 0, false}, {"null", 0, false},
	} {
		t.Run(c.token, func(t *testing.T) {
			raw := strings.Replace(boardDelegationJSON, `"evaluated_at":1000`, `"evaluated_at":`+c.token, 1)
			r, err := ParseBoardDelegationRequestJSON([]byte(raw))
			if (err == nil) != c.valid || c.valid && r.EvaluatedAt != c.want {
				t.Fatalf("time got %d %v", r.EvaluatedAt, err)
			}
		})
	}
}

func TestBoardDelegationDecisionCodec(t *testing.T) {
	allow := `{"contract":"campusos.board-delegation/v1","request_id":"request-1","facts_version":"facts-1","effect":"allow","reason":"delegation.allowed","witnesses":[{"candidate_index":0,"bound_id":"bound-1"}],"obligations":["recheck_authority_in_transaction","required_audit"]}`
	d, err := ParseBoardDelegationDecisionJSON([]byte(allow))
	if err != nil || d.Validate() != nil || d.Witnesses[0].BoundID != "bound-1" {
		t.Fatalf("allow decision: %+v %v", d, err)
	}
	deny := `{"contract":"campusos.board-delegation/v1","request_id":"request-1","facts_version":"facts-1","effect":"deny","reason":"delegation.outside_bounds","witnesses":[],"obligations":[]}`
	if d, err = ParseBoardDelegationDecisionJSON([]byte(deny)); err != nil || d.Effect != "deny" {
		t.Fatalf("deny decision: %+v %v", d, err)
	}
	for name, raw := range map[string]string{
		"allow_without_witness":  strings.Replace(allow, `[{"candidate_index":0,"bound_id":"bound-1"}]`, `[]`, 1),
		"allow_missing_audit":    strings.Replace(allow, `,"required_audit"]`, `]`, 1),
		"allow_reordered_duties": strings.Replace(allow, `["recheck_authority_in_transaction","required_audit"]`, `["required_audit","recheck_authority_in_transaction"]`, 1),
		"allow_deny_reason":      strings.Replace(allow, `"delegation.allowed"`, `"delegation.outside_bounds"`, 1),
		"deny_with_witness":      strings.Replace(deny, `"witnesses":[]`, `"witnesses":[{"candidate_index":0,"bound_id":"bound-1"}]`, 1),
		"deny_with_obligations":  strings.Replace(deny, `"obligations":[]`, `"obligations":["required_audit"]`, 1),
		"deny_allow_reason":      strings.Replace(deny, `"delegation.outside_bounds"`, `"delegation.allowed"`, 1),
		"unknown_reason":         strings.Replace(deny, `"delegation.outside_bounds"`, `"delegation.super_admin"`, 1),
		"witness_duplicate":      strings.Replace(allow, `"bound_id":"bound-1"}]`, `"bound_id":"bound-1"},{"candidate_index":0,"bound_id":"bound-1"}]`, 1),
		"witness_index_32":       strings.Replace(allow, `"candidate_index":0`, `"candidate_index":32`, 1),
		"witness_index_negative": strings.Replace(allow, `"candidate_index":0`, `"candidate_index":-1`, 1),
		"witness_unknown_field":  strings.Replace(allow, `"bound_id":"bound-1"}`, `"bound_id":"bound-1","grant":{}}`, 1),
		"null_witnesses":         strings.Replace(allow, `"witnesses":[`, `"witnesses":null,"surplus":[`, 1),
		"null_obligations":       strings.Replace(allow, `"obligations":[`, `"obligations":null,"surplus":[`, 1),
		"unknown_field":          strings.Replace(allow, `"facts_version":"facts-1"`, `"facts_version":"facts-1","committed":true`, 1),
		"wrong_contract":         strings.Replace(allow, `"campusos.board-delegation/v1"`, `"campusos.board-delegation/v2"`, 1),
		"trailing":               allow + " {}",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseBoardDelegationDecisionJSON([]byte(raw)); !errors.Is(err, ErrDelegationInputInvalid) {
				t.Fatalf("invalid decision escaped: %v", err)
			}
		})
	}
}
