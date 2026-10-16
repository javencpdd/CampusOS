// Package integrationmodel contains detached Identity-owned whitelist values.
// It has no repository, authenticator, transport or policy dependencies.
package integrationmodel

import (
	"encoding/json"
	"errors"
)

type Rule struct {
	ID           string   `json:"id"`
	Channel      string   `json:"channel"`
	Endpoint     string   `json:"endpoint"`
	Action       string   `json:"action"`
	ResourceKind string   `json:"resource_kind"`
	ResourceIDs  []string `json:"resource_ids"`
	Status       string   `json:"status"`
	NotBeforeMS  int64    `json:"not_before_ms"`
	ExpiresAtMS  int64    `json:"expires_at_ms"`
}

func OpaqueID(v string) bool {
	if len(v) < 1 || len(v) > 128 {
		return false
	}
	for i, c := range []byte(v) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || i > 0 && (c == '.' || c == '_' || c == ':' || c == '-') {
			continue
		}
		return false
	}
	return true
}

var ErrRulesInvalid = errors.New("invalid integration whitelist")

func ValidateRules(rules []Rule) error {
	if rules == nil || len(rules) > 64 {
		return ErrRulesInvalid
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if !OpaqueID(r.ID) || !OpaqueID(r.Channel) || !OpaqueID(r.Endpoint) || !OpaqueID(r.Action) || !OpaqueID(r.ResourceKind) || seen[r.ID] || r.NotBeforeMS < 0 || r.ExpiresAtMS <= r.NotBeforeMS || r.ExpiresAtMS > 9007199254740991 || (r.Status != "active" && r.Status != "suspended" && r.Status != "revoked") || len(r.ResourceIDs) < 1 || len(r.ResourceIDs) > 32 {
			return ErrRulesInvalid
		}
		seen[r.ID] = true
		ids := map[string]bool{}
		for _, id := range r.ResourceIDs {
			if !OpaqueID(id) || ids[id] {
				return ErrRulesInvalid
			}
			ids[id] = true
		}
	}
	raw, err := json.Marshal(rules)
	if err != nil || len(raw) > 24576 {
		return ErrRulesInvalid
	}
	return nil
}
