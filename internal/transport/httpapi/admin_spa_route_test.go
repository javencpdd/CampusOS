package httpapi

import (
	"github.com/gin-gonic/gin"
	"strings"
	"testing"
)

func TestAdminSPAActualRuntimeDescriptorsKeepSelfSeparateFromManagement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := Build(Dependencies{})
	found := 0
	management := false
	for _, r := range router.RouteDescriptors() {
		if strings.HasPrefix(r.OperationCode, "http.identity.admin_self.") {
			found++
			expected := "jwt+current-admin-session+browser-csrf"
			audit := "required-command+outbox"
			if r.OperationCode == "http.identity.admin_self.read" {
				expected = "jwt+current-admin-session"
				audit = "none"
			}
			if r.Auth != expected || r.Audit != audit || r.PermissionCode != "" {
				t.Fatal(r)
			}
		}
		if r.Path == "/api/v1/admin/identity/accounts" {
			management = true
			if r.Auth != "jwt+admin-account+permission" || r.PermissionCode == "" {
				t.Fatal("management permission weakened", r)
			}
		}
	}
	if found != 5 || !management {
		t.Fatal("self/management actual registry incomplete", found, management)
	}
}
