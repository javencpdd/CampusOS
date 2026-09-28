package policy

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// The Linux drill supplies an independently generated frozen G1 corpus and
// validates these actual native Go decisions again with Ajv and the G1 oracle.
func TestResourcePolicyG1WireCorpus(t *testing.T) {
	input, output := os.Getenv("CAMPUSOS_RESOURCE_CORPUS_IN"), os.Getenv("CAMPUSOS_RESOURCE_CORPUS_OUT")
	if input == "" && output == "" {
		t.Skip("native G1 interoperability corpus is supplied by v12-02a-resource-policy-drill.sh")
	}
	if input == "" || output == "" {
		t.Fatal("both corpus paths required")
	}
	raw, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string          `json:"name"`
		Input    json.RawMessage `json:"input"`
		Expected string          `json:"expected"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 397 {
		t.Fatalf("expected complete 397-case native corpus, got %d", len(cases))
	}
	type result struct {
		Name     string                       `json:"name"`
		Input    json.RawMessage              `json:"input"`
		Decision *port.ResourcePolicyDecision `json:"decision,omitempty"`
		Error    string                       `json:"error,omitempty"`
	}
	results := make([]result, 0, len(cases))
	for _, c := range cases {
		entry := result{Name: c.Name, Input: c.Input}
		request, err := port.ParseResourcePolicyRequestJSON(c.Input)
		if err == nil {
			var decision port.ResourcePolicyDecision
			decision, err = (ResourcePolicy{}).DecideResource(request)
			if err == nil {
				entry.Decision = &decision
			}
		}
		outcome := ""
		if err != nil {
			entry.Error = err.Error()
			outcome = entry.Error
		} else if entry.Decision.Effect == "allow" {
			outcome = "allow"
		} else {
			outcome = entry.Decision.Reason
		}
		if outcome != c.Expected {
			t.Fatalf("%s: got %s want %s", c.Name, outcome, c.Expected)
		}
		results = append(results, entry)
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, encoded, 0600); err != nil {
		t.Fatal(err)
	}
}
