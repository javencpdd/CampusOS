package httpapi

import (
	"strings"
	"testing"
)

func TestRuntimeConfigRoutesRequireIndependentAdminAuthority(t *testing.T) {
	routes := Build(Dependencies{}).RouteDescriptors()
	found := 0
	for _, route := range routes {
		if !strings.HasPrefix(route.Path, "/api/v1/platform/runtime-config") {
			continue
		}
		found++
		if route.Owner != "core.platform-api" || route.Audience != "admin" || !strings.HasPrefix(route.Auth, "jwt+current-admin-session+permission") {
			t.Fatal(route)
		}
		if route.Method != "GET" && (route.Audit != "required-intent-audit+outbox" || route.PermissionCode != "platform.runtime_config.manage" || !strings.HasSuffix(route.Auth, "recent-mfa-or-current-password+browser-csrf")) {
			t.Fatal(route)
		}
	}
	if found != 3 {
		t.Fatal("runtime configuration routes missing", found)
	}
}
