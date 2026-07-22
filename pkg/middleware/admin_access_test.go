package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type adminAccessStub struct {
	allowed bool
	err     error
}

type adminMFAStub struct {
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

func (s adminMFAStub) CheckAdminMFA(context.Context, string, string) (bool, error) {
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

func TestRequireAdminAccessEnforcesRecentMFAOnlyForAdmittedAdministrators(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name        string
		admin       AdminAccessChecker
		mfa         AdminMFAChecker
		withSession bool
		want        int
		wantCode    string
	}{
		{name: "fresh MFA", admin: adminAccessStub{allowed: true}, mfa: adminMFAStub{allowed: true}, withSession: true, want: http.StatusNoContent},
		{name: "password only", admin: adminAccessStub{allowed: true}, mfa: adminMFAStub{}, withSession: true, want: http.StatusForbidden, wantCode: "identity.mfa.step_up_required"},
		{name: "MFA state unavailable", admin: adminAccessStub{allowed: true}, mfa: adminMFAStub{err: errors.New("database unavailable")}, withSession: true, want: http.StatusServiceUnavailable, wantCode: "identity.mfa.unavailable"},
		{name: "missing session", admin: adminAccessStub{allowed: true}, mfa: adminMFAStub{allowed: true}, want: http.StatusUnauthorized, wantCode: "auth.required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", "90001")
				if test.withSession {
					c.Set("session_id", "session-1")
				}
				c.Next()
			})
			router.Use(RequireAdminAccess(test.admin, test.mfa))
			router.GET("/admin", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin", nil))
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
			if test.wantCode != "" && !strings.Contains(response.Body.String(), test.wantCode) {
				t.Fatalf("missing code %q: %s", test.wantCode, response.Body.String())
			}
		})
	}
}

func TestRequireAdminAccessKeepsScopedModeratorPathOutsideAdminMFA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "90002")
		c.Set("session_id", "moderator-session")
		c.Next()
	})
	router.Use(RequireAdminAccessOrScopedPermission(
		adminAccessStub{}, scopedPermissionStub{allowed: true}, "community.thread.review", "category", adminMFAStub{},
	))
	router.GET("/moderation", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/moderation", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("scoped moderator unexpectedly required admin MFA: status=%d body=%s", response.Code, response.Body.String())
	}
}
