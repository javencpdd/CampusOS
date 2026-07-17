package feature

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPathGateAllowsOnlyDeclaredStatusPathWhenFeatureIsDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := NewRegistry(nil)
	if err := registry.Register(Definition{ID: "secondhand", Mode: HotGated, DefaultEnabled: false}); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(PathGate(registry, PathRule{
		Prefix:    "/api/v1/secondhand",
		FeatureID: "secondhand",
		AllowWhenDisabled: []AllowedPath{{
			Method: http.MethodGet,
			Path:   "/api/v1/secondhand/status",
		}},
	}))
	router.GET("/api/v1/secondhand/status", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/api/v1/secondhand/threads", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/api/v1/secondhand/status", func(c *gin.Context) { c.Status(http.StatusOK) })

	status := httptest.NewRecorder()
	router.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/secondhand/status", nil))
	if status.Code != http.StatusOK {
		t.Fatalf("declared status path was blocked: %d %s", status.Code, status.Body.String())
	}

	businessPath := httptest.NewRecorder()
	router.ServeHTTP(businessPath, httptest.NewRequest(http.MethodGet, "/api/v1/secondhand/threads", nil))
	if businessPath.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled feature business path must be rejected, got %d", businessPath.Code)
	}

	wrongMethod := httptest.NewRecorder()
	router.ServeHTTP(wrongMethod, httptest.NewRequest(http.MethodPost, "/api/v1/secondhand/status", nil))
	if wrongMethod.Code != http.StatusServiceUnavailable {
		t.Fatalf("only the declared read-only status route may bypass the gate, got %d", wrongMethod.Code)
	}
}
