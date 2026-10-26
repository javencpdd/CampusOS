package httpapi

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKnowledgePolicyFailuresNeverCache(t *testing.T) {
	for _, path := range []string{"/api/v1/knowledge/collections", "/api/v1/admin/knowledge/collections/id/index", "/api/v1/integration/knowledge/collections/id/search", "/api/v1/knowledge-unrelated", "/api/v1/admin/knowledge-unrelated"} {
		t.Run(path, func(t *testing.T) {
			router := gin.New()
			router.Use(knowledgeNoStore, func(c *gin.Context) { c.AbortWithStatus(403) })
			router.GET(path, func(c *gin.Context) { t.Fatal("denied handler ran") })
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 403 {
				t.Fatal(w.Code)
			}
			expected := !strings.Contains(path, "knowledge-unrelated")
			if got := strings.Contains(w.Header().Get("Cache-Control"), "no-store"); got != expected {
				t.Fatal(path, w.Header())
			}
		})
	}
}
