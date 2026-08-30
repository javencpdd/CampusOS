package academicterm

import (
	"strings"
	"testing"
)

// This guards the PostgreSQL parse-time regression where $5 was inferred as
// both text and varchar. The full create lifecycle is covered by the runtime
// PostgreSQL smoke check in the Docker development verification workflow.
func TestCreateTermSQLUsesOneExplicitStatusType(t *testing.T) {
	if count := strings.Count(createTermSQL, "$5::varchar"); count != 2 {
		t.Fatalf("status placeholder must be explicitly varchar in INSERT and CASE, got %d occurrences: %s", count, createTermSQL)
	}
	if strings.Contains(createTermSQL, "CASE WHEN $5='") {
		t.Fatalf("status CASE must not reintroduce an untyped $5 placeholder: %s", createTermSQL)
	}
}
