package httpapi

import (
	"net/http"
	"strings"

	platformversion "github.com/campusos/CampusOS/internal/platform/version"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// APIIndex is a small, stable discovery response for both humans and API
// clients. It deliberately lists public contracts only and never leaks server
// configuration or installed-plugin details.
func APIIndex(c *gin.Context) {
	if acceptsHTML(c.GetHeader("Accept")) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>CampusOS API v1</title></head><body><main><h1>CampusOS API v1</h1><p>此地址提供 CampusOS HTTP API。</p><ul><li><a href="/api/v1/health">健康检查</a></li><li><a href="/docs/api/openapi-v0.6-current.yaml">OpenAPI 合同</a></li><li><a href="/docs/README.md">项目文档</a></li></ul></main></body></html>`))
		return
	}
	response.Success(c, gin.H{
		"name":                "CampusOS API",
		"version":             "v1",
		"application_version": platformversion.Display,
		"links": gin.H{
			"health":  "/api/v1/health",
			"openapi": "/docs/api/openapi-v0.6-current.yaml",
			"docs":    "/docs/README.md",
			"threads": "/api/v1/threads",
		},
	})
}

// APINoRoute keeps unknown API paths inside the existing error envelope while
// letting an upstream web server handle non-API frontend fallbacks separately.
func APINoRoute(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		response.Error(c, http.StatusNotFound, 40401, "API endpoint not found")
		return
	}
	c.Status(http.StatusNotFound)
}

func acceptsHTML(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "text/html") && !strings.Contains(value, "application/json")
}
