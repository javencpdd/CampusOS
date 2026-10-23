package plugin

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestV5HTTPStrictIntentAndCSRF(t *testing.T) {
	v := newV5TestPlatform(t)
	h := &V5HTTPHandler{Platform: v.s}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/:publisher/:key", h.Mutate(true))
	for _, body := range []string{`{"kind":"enable","kind":"disable"}`, `{"Kind":"enable","version":"1.0.0","idempotency_key":"case"}`, `{"kind":"enable","owner_user_id":"1002"}`, `{"kind":null}`, `{"kind":"enable","version":"1.0.0","idempotency_key":"x","expected_generation":"01"}`, `{"kind":"enable","version":"1.0.0","idempotency_key":"x","expected_revision":"+1"}`, `{"kind":"enable","observed":"ready"}`, `{}` + `{}`, strings.Repeat(" ", PluginV5ManifestMaxBytes+1)} {
		req := httptest.NewRequest("POST", "/campusos/sample", strings.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal("untrusted shape accepted", w.Code)
		}
	}
	for _, admin := range []bool{false, true} {
		c, w := gin.CreateTestContext(httptest.NewRecorder())
		_ = w
		c.Request = httptest.NewRequest("POST", "/", nil)
		c.Request.Header.Set("Origin", "http://localhost")
		if v5CSRF(c, admin) {
			t.Fatal("missing browser CSRF")
		}
		cookie, header := "campusos_csrf", "X-CSRF-Token"
		if admin {
			cookie, header = "campusos_admin_csrf", "X-Admin-CSRF-Token"
		}
		c.Request.AddCookie(&http.Cookie{Name: cookie, Value: "owned-browser-token-123456"})
		c.Request.Header.Set(header, "owned-browser-token-123456")
		if !v5CSRF(c, admin) {
			t.Fatal("bound token rejected")
		}
	}
}
