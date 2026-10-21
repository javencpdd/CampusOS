package projectaudit

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeRoutesCurrentAuthorityAndClosedDTOs(t *testing.T) {
	root, e := FindRepositoryRoot(".")
	if e != nil {
		t.Fatal(e)
	}
	routes, e := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if e != nil {
		t.Fatal(e)
	}
	count, writes := 0, 0
	for _, r := range routes {
		if !strings.HasPrefix(r.OperationCode, "http.knowledge.") {
			continue
		}
		count++
		if r.ModuleOwner != "feature.knowledge" {
			t.Fatal("Knowledge route not owned", r)
		}
		profile := profileFor(r)
		if profile.ResponseSchema == "" || profile.ResponseSchema == "GenericObject" {
			t.Fatal("response has no narrow schema", r.OperationCode)
		}
		if strings.Contains(r.Path, "/admin/knowledge/") {
			if !strings.Contains(r.Auth, "current-admin-session") || r.Scope != "knowledge.collection_inventory/all" {
				t.Fatal("missing current domain proof", r)
			}
			expected := "knowledge.collection.read"
			if r.Method != "GET" {
				writes++
				expected = "knowledge.collection.manage"
				if !strings.Contains(r.Auth, "recent-mfa+browser-csrf") || r.Audit != "required-command+outbox" || profile.RequestSchema == "GenericObject" || profile.RequestSchema == "" {
					t.Fatal("write weakened", r)
				}
			}
			if r.PermissionCode != expected {
				t.Fatal("wrong exact permission", r)
			}
		} else if r.Auth != "none+current-origin-policy" || r.Scope != "public-collection+current-origin" {
			t.Fatal("public source must recheck origin", r)
		}
	}
	if count != 15 || writes != 8 {
		t.Fatal("Knowledge route inventory incomplete", count, writes)
	}
}
