package policy

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// The Linux drill supplies an independently generated frozen G1 corpus and
// validates these actual native Go outcomes again with the G1 oracle.
func TestScopeDelegationG1WireCorpus(t *testing.T) {
	input, output := os.Getenv("CAMPUSOS_SCOPE_DELEGATION_CORPUS_IN"), os.Getenv("CAMPUSOS_SCOPE_DELEGATION_CORPUS_OUT")
	if input == "" && output == "" {
		t.Skip("native G1 interoperability corpus is supplied by v12-02a-scope-delegation-drill.sh")
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
	if len(cases) != 70 {
		t.Fatalf("expected complete 70-case native corpus, got %d", len(cases))
	}
	type result struct {
		Name   string          `json:"name"`
		Input  json.RawMessage `json:"input"`
		Result string          `json:"result"`
	}
	results := make([]result, 0, len(cases))
	for _, c := range cases {
		entry := result{Name: c.Name, Input: c.Input}
		parsed, err := port.ParseScopeDelegationInputJSON(c.Input)
		if err == nil {
			err = (ScopeDelegationPolicy{}).CheckScopeDelegation(parsed)
		}
		entry.Result = scopeDelegationOutcome(err)
		if entry.Result != c.Expected {
			t.Fatalf("%s: got %s want %s", c.Name, entry.Result, c.Expected)
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
