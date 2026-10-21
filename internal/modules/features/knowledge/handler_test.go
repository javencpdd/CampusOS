package knowledge

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestKnowledgeHTTPClosedBodyAndBrowserCSRF(t *testing.T) {
	f := fixtureCollections(t)
	h := NewHandler(f.s, noAdminSession{})
	for _, tc := range []struct {
		name, raw           string
		browser, csrf, want bool
	}{
		{"complete64KiB", `{"title":"x","content":"` + strings.Repeat("x", MaxSourceBodyBytes) + `","format":"text","status":"draft"}`, false, false, true},
		{"duplicate", `{"title":"x","title":"y","content":"x","format":"text","status":"draft"}`, false, false, false},
		{"escapedDuplicate", `{"title":"x","\u0074itle":"y","content":"x","format":"text","status":"draft"}`, false, false, false},
		{"null", `{"title":"x","content":null,"format":"text","status":"draft"}`, false, false, false},
		{"badUnicode", `{"title":"\ud800","content":"x","format":"text","status":"draft"}`, false, false, false},
		{"principal", `{"title":"x","content":"x","format":"text","status":"draft","principal":{"kind":"admin"}}`, false, false, false},
		{"browserNoCSRF", `{"title":"x","content":"x","format":"text","status":"draft"}`, true, false, false},
		{"browserCSRF", `{"title":"x","content":"x","format":"text","status":"draft"}`, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/", strings.NewReader(tc.raw))
			if tc.browser {
				c.Request.Header.Set("Origin", "http://localhost")
			}
			if tc.csrf {
				c.Request.Header.Set("Cookie", "campusos_admin_csrf=matching")
				c.Request.Header.Set("X-Admin-CSRF-Token", "matching")
			}
			var out Official
			if got := h.body(c, &out, "title", "content", "format", "status"); got != tc.want {
				t.Fatalf("body accepted=%t want=%t", got, tc.want)
			}
		})
	}
	for _, raw := range []string{`{"version":"01"}`, `{"version":"0"}`, `{"version":1}`, `{"version":"9223372036854775808"}`, `{"version":"+1"}`} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(raw))
		var v map[string]json.RawMessage
		if h.body(c, &v, "version") {
			t.Fatal("noncanonical version", raw)
		}
	}
}
