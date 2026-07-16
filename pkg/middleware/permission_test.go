package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type scopedPermissionChecker struct {
	global    bool
	candidate bool
}

func (c scopedPermissionChecker) Check(_ context.Context, _, _, _ string) (bool, error) {
	return c.global, nil
}

func (c scopedPermissionChecker) CheckCode(_ context.Context, _, _ string) (bool, error) {
	return c.global, nil
}

func (c scopedPermissionChecker) HasAnyScopedPermissionCode(_ context.Context, _, _, _ string) (bool, error) {
	return c.candidate, nil
}

func TestRequireScopedPermissionAllowsCandidateForServiceScopeCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("user_id", "2002") })
	router.POST("/governed", RequireScopedPermissionForOperation(scopedPermissionChecker{candidate: true}, "thread", "delete", "community.thread.take_down", "http.community.thread.take_down", "category"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/governed", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("category-scoped candidate should reach resource service, got %d", response.Code)
	}
}

func TestRequireScopedPermissionRejectsWithoutGlobalOrScopeCandidate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("user_id", "2002") })
	router.POST("/governed", RequireScopedPermissionForOperation(scopedPermissionChecker{}, "thread", "delete", "community.thread.take_down", "http.community.thread.take_down", "category"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/governed", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing scoped candidate should be forbidden, got %d", response.Code)
	}
}
