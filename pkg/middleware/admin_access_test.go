package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type adminAccessStub struct {
	allowed bool
	err     error
}

type scopedPermissionStub struct {
	allowed bool
	err     error
}

func (s scopedPermissionStub) Check(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (s scopedPermissionStub) HasAnyScopedPermissionCode(context.Context, string, string, string) (bool, error) {
	return s.allowed, s.err
}

func (s adminAccessStub) CheckAdminAccess(context.Context, string) (bool, error) {
	return s.allowed, s.err
}

func TestRequireAdminAccessAllowsOnlyExplicitScopedFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name        string
		permissions PermissionChecker
		want        int
	}{
		{name: "moderator candidate", permissions: scopedPermissionStub{allowed: true}, want: http.StatusNoContent},
		{name: "no scoped grant", permissions: scopedPermissionStub{}, want: http.StatusForbidden},
		{name: "scope lookup error", permissions: scopedPermissionStub{err: errors.New("db unavailable")}, want: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", "90002")
				c.Next()
			})
			router.Use(RequireAdminAccessOrScopedPermission(adminAccessStub{}, test.permissions, "community.thread.review", "category"))
			router.GET("/moderation", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/moderation", nil))
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestRequireAdminAccessFailsClosedBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		checker AdminAccessChecker
		want    int
	}{
		{name: "active", checker: adminAccessStub{allowed: true}, want: http.StatusNoContent},
		{name: "inactive", checker: adminAccessStub{}, want: http.StatusForbidden},
		{name: "repository error", checker: adminAccessStub{err: errors.New("db unavailable")}, want: http.StatusInternalServerError},
		{name: "missing checker", checker: nil, want: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", "90001")
				c.Next()
			})
			router.Use(RequireAdminAccess(test.checker))
			router.GET("/admin", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin", nil))
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}
