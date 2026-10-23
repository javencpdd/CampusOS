package ai

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestManagedHTTPClosedBodyAndAdminCSRF(t *testing.T) {
	for _, tc := range []struct {
		name, raw                       string
		browser, cookie, matching, want bool
	}{
		{"CLI", `{"version":"1","enabled":true}`, false, false, false, true},
		{"browser_missing", `{"version":"1","enabled":true}`, true, false, false, false},
		{"browser_current_CSRF", `{"version":"1","enabled":true}`, true, true, true, true},
		{"browser_mismatch", `{"version":"1","enabled":true}`, true, true, false, false},
		{"cookie_only", `{"version":"1","enabled":true}`, false, true, false, false},
		{"unknown", `{"version":"1","enabled":true,"endpoint":"https://unsafe"}`, false, false, false, false},
		{"missing_boolean", `{"version":"1"}`, false, false, false, false},
		{"null_boolean", `{"version":"1","enabled":null}`, false, false, false, false},
		{"duplicate", `{"version":"1","enabled":true,"enabled":false}`, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/", strings.NewReader(tc.raw))
			if tc.browser {
				c.Request.Header.Set("Origin", "https://owned.test")
			}
			if tc.cookie {
				c.Request.Header.Set("Cookie", "campusos_admin_refresh=owned; campusos_admin_csrf=owned-current-csrf")
			}
			if tc.matching {
				c.Request.Header.Set("X-Admin-CSRF-Token", "owned-current-csrf")
			}
			var out struct {
				Version string `json:"version"`
				Enabled bool   `json:"enabled"`
			}
			if got := managedBody(c, &out, "version", "enabled"); got != tc.want {
				t.Fatalf("accepted=%v want=%v", got, tc.want)
			}
		})
	}
}
