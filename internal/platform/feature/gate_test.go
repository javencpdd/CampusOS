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

func TestPathGateOwnerDisabledContractStillAborts(t *testing.T) {
	registry := NewRegistry(nil)
	if e := registry.Register(Definition{ID: "owned", Mode: HotGated, DefaultEnabled: false}); e != nil {
		t.Fatal(e)
	}
	called := false
	r := gin.New()
	r.Use(PathGate(registry, PathRule{Prefix: "/owned", FeatureID: "owned", DisabledHandler: func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "owner.disabled"})
	}}))
	r.GET("/owned", func(c *gin.Context) { called = true; c.Status(200) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/owned", nil))
	if called || w.Code != 503 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("gate did not abort with owner contract", w.Code)
	}
	if _, e := registry.Request("owned", true); e != nil {
		t.Fatal(e)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/owned", nil))
	if !called || w.Code != 200 {
		t.Fatal("enabled path failed", w.Code)
	}
}
