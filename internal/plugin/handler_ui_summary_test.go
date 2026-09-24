package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPluginPayloadSummarizesUISurfacesWithoutSchema(t *testing.T) {
	h := NewHandler(NewManager())
	p := &Plugin{ID: "summary-test", Manifest: &Manifest{Name: "summary-test", Version: "1.1", UI: UIContribution{ContractVersion: CurrentUIContract, Surfaces: []UISurface{{ID: "summary-test.preview", Version: "v1", Type: "record-preview", Presentations: []string{"modal"}, Schema: map[string]interface{}{"private_fixture": "never-return-schema"}}}}}}
	payload := h.pluginPayload(p)
	if payload["ui_contract_version"] != CurrentUIContract {
		t.Fatal("missing UI contract version")
	}
	raw, err := json.Marshal(payload["ui_surfaces"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "summary-test.preview") || !strings.Contains(string(raw), "modal") || strings.Contains(string(raw), "never-return-schema") {
		t.Fatalf("unsafe/incomplete UI summary: %s", raw)
	}
}
