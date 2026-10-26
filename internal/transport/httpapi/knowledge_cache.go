package httpapi

import (
	"github.com/gin-gonic/gin"
	"strings"
)

// Apply before Feature/auth/permission middleware: policy failures also belong
// to the no-store contract and may never retain a previous Knowledge response.
func knowledgeNoStore(c *gin.Context) {
	for _, prefix := range []string{"/api/v1/knowledge", "/api/v1/admin/knowledge", "/api/v1/integration/knowledge", "/api/v1/ai"} {
		if c.Request.URL.Path == prefix || strings.HasPrefix(c.Request.URL.Path, prefix+"/") {
			c.Header("Cache-Control", "private, no-store")
			break
		}
	}
	c.Next()
}
