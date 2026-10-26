package plugin

import (
	"encoding/json"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
	"os"
	"testing"
)

func TestV5DispatchDecisionAndStrictEnvelope(t *testing.T) {
	c := campusossdk.V5DispatchCall{RequestID: "r1", Contract: "campusos.plugin-dispatch/v1", Target: campusossdk.V5DispatchTarget{Kind: "contribution", ID: "chat"}, InstanceGeneration: 4, DeadlineMS: 1500, RequestBytes: 2, ResponseLimitBytes: 4}
	f := v5DispatchFacts{NowMS: 1000, CurrentGeneration: 4, Desired: "enabled", Observed: "ready", GlobalInstalled: true, Grant: true, Declared: true, Published: true, Feature: true, Audience: "host-api", CredentialExpiry: 2000, Selected: "chat", MaxRequest: 4, MaxResponse: 8, MaxDeadline: 2000}
	if err := decideV5Dispatch("host_to_plugin", c, f); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*v5DispatchFacts)
		want   string
	}{
		{"generation", func(f *v5DispatchFacts) { f.CurrentGeneration++ }, "plugin.instance_stale"},
		{"grant", func(f *v5DispatchFacts) { f.Grant = false }, "plugin.grant_revoked"},
		{"consent", func(f *v5DispatchFacts) { f.ConsentRequired = true }, "plugin.consent_required"},
		{"publication", func(f *v5DispatchFacts) { f.Published = false }, "plugin.target_unpublished"},
		{"selection", func(f *v5DispatchFacts) { f.Selected = "other" }, "plugin.contribution_selection_conflict"},
		{"feature", func(f *v5DispatchFacts) { f.Feature = false }, "plugin.feature_disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := f
			tc.change(&x)
			e := decideV5Dispatch("host_to_plugin", c, x)
			if e == nil || e.Error() != tc.want {
				t.Fatal(e)
			}
		})
	}
	wire, _ := json.Marshal(c)
	if _, err := ParseV5DispatchCall(wire); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"trusted":true}`, `null`, string(wire) + `{}`, `{"request_id":"a","request_id":"b"}`} {
		if _, err := ParseV5DispatchCall([]byte(raw)); err != ErrV5CallInvalid {
			t.Fatal(err)
		}
	}
}

func TestV5DispatchFrozenFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../sdk/typescript/tests/plugin-v5-dispatch-native.fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name  string `json:"name"`
		Input struct {
			Direction string          `json:"direction"`
			Call      json.RawMessage `json:"call"`
			Trusted   v5DispatchFacts `json:"trusted"`
		} `json:"input"`
		Expected string `json:"expected"`
	}
	if json.Unmarshal(raw, &fixtures) != nil || len(fixtures) != 30 {
		t.Fatal("missing frozen fixtures")
	}
	results := []map[string]string{}
	for _, f := range fixtures {
		actual := "accepted"
		t.Run(f.Name, func(t *testing.T) {
			c, err := ParseV5DispatchCall(f.Input.Call)
			if err == nil {
				err = decideV5Dispatch(f.Input.Direction, c, f.Input.Trusted)
			}
			if err != nil {
				actual = err.Error()
			}
			if actual != f.Expected {
				t.Fatalf("got %s want %s", actual, f.Expected)
			}
		})
		results = append(results, map[string]string{"name": f.Name, "actual": actual})
	}
	if name := os.Getenv("CAMPUSOS_V5_DISPATCH_DECISIONS"); name != "" {
		raw, _ := json.Marshal(results)
		if os.WriteFile(name, raw, 0600) != nil {
			t.Fatal("cannot write owned decisions")
		}
	}
}
