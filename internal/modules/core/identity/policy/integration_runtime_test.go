package policy

import (
	"encoding/json"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIntegrationRuntimeIndependentWireCorpus(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("source path unavailable")
	}
	path := filepath.Join(filepath.Dir(file), "../../../../../sdk/typescript/tests/integration-access-v2.fixtures.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string          `json:"name"`
		Input    json.RawMessage `json:"input"`
		Expected string          `json:"expected"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil || len(cases) != 192 {
		t.Fatal("complete independent corpus required", err)
	}
	results := make([]map[string]any, 0, len(cases))
	for _, tc := range cases {
		r, err := port.ParseIntegrationRuntimeInputJSON(tc.Input)
		result := ""
		var decision *port.IntegrationAccessDecision
		if err == nil {
			d, e := (IntegrationRuntimePolicy{}).DecideIntegrationAccess(r)
			err = e
			if e == nil {
				result = d.Reason
				decision = &d
			}
		}
		if err != nil {
			result = err.Error()
		}
		if result != tc.Expected {
			t.Fatalf("%s: got %s want %s", tc.Name, result, tc.Expected)
		}
		results = append(results, map[string]any{"name": tc.Name, "input": tc.Input, "result": result, "decision": decision})
	}
	if output := os.Getenv("CAMPUSOS_INTEGRATION_RUNTIME_OUTPUT"); output != "" {
		encoded, err := json.Marshal(results)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
