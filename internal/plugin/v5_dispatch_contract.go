package plugin

import (
	"encoding/json"
	"errors"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
	"math"
)

type v5DispatchFacts struct {
	NowMS             int64  `json:"now_ms"`
	CurrentGeneration int64  `json:"current_generation"`
	Desired           string `json:"desired_state"`
	Observed          string `json:"observed_state"`
	GlobalInstalled   bool   `json:"global_installed"`
	UserRequired      bool   `json:"user_install_required"`
	UserInstalled     bool   `json:"user_installed"`
	Grant             bool   `json:"grant_active"`
	ConsentRequired   bool   `json:"consent_required"`
	Consent           bool   `json:"consent_active"`
	Declared          bool   `json:"declared"`
	Published         bool   `json:"published"`
	Feature           bool   `json:"feature_enabled"`
	Audience          string `json:"instance_audience"`
	CredentialExpiry  int64  `json:"credential_expires_at_ms"`
	Selected          string `json:"selected_id"`
	MaxRequest        int64  `json:"max_request_bytes"`
	MaxResponse       int64  `json:"max_response_bytes"`
	MaxDeadline       int64  `json:"max_deadline_ms"`
}

func ParseV5DispatchCall(raw []byte) (campusossdk.V5DispatchCall, error) {
	var c campusossdk.V5DispatchCall
	v, ok := v5JSON(raw)
	if !ok || !v5Object("request_id contract target instance_generation deadline_ms request_bytes response_limit_bytes", map[string]v5Check{
		"request_id": v5String(1, 128, `(?s)^.*$`), "contract": v5Enum("campusos.plugin-dispatch/v1"),
		"target":              v5Object("kind id", map[string]v5Check{"kind": v5Enum("host_api", "contribution"), "id": v5String(1, 128, `(?s)^.*$`)}),
		"instance_generation": v5Integer(1, math.MaxInt64), "deadline_ms": v5Integer(1, math.MaxInt64),
		"request_bytes": v5Integer(0, math.MaxInt64), "response_limit_bytes": v5Integer(1, math.MaxInt64),
	})(v) {
		return c, ErrV5CallInvalid
	}
	wire, _ := json.Marshal(v5CanonicalNumbers(v))
	if json.Unmarshal(wire, &c) != nil {
		return c, ErrV5CallInvalid
	}
	return c, nil
}

// Pure G1 decision kernel. Only managed host adapters construct these facts;
// no ingress decodes this struct as authority.
func decideV5Dispatch(direction string, c campusossdk.V5DispatchCall, f v5DispatchFacts) error {
	fail := func(s string) error { return errors.New("plugin." + s) }
	if direction != "host_to_plugin" && direction != "plugin_to_host" {
		return ErrV5CallInvalid
	}
	if (direction == "host_to_plugin") != (c.Target.Kind == "contribution") {
		return ErrV5CallTarget
	}
	if c.InstanceGeneration != f.CurrentGeneration {
		return ErrV5ContributionStale
	}
	if c.DeadlineMS <= f.NowMS || c.DeadlineMS > f.MaxDeadline {
		return ErrV5CallDeadline
	}
	if c.RequestBytes > f.MaxRequest || c.ResponseLimitBytes > f.MaxResponse {
		return ErrV5ContributionBudget
	}
	if !f.GlobalInstalled {
		return fail("global_install_required")
	}
	if f.Desired != "enabled" || f.Observed != "ready" {
		return ErrV5RuntimeMissing
	}
	if !f.Grant {
		return fail("grant_revoked")
	}
	if f.UserRequired && !f.UserInstalled {
		return fail("user_install_required")
	}
	if f.ConsentRequired && !f.Consent {
		return fail("consent_required")
	}
	if !f.Feature {
		return ErrV5FeatureDisabled
	}
	if !f.Declared {
		return fail("target_undeclared")
	}
	if !f.Published {
		return ErrV5ContributionUnpublished
	}
	if direction == "host_to_plugin" && f.Selected != c.Target.ID {
		return ErrV5ContributionConflict
	}
	if direction == "plugin_to_host" && (f.Audience != "host-api" || f.CredentialExpiry <= f.NowMS) {
		return fail("instance_credential_invalid")
	}
	return nil
}
