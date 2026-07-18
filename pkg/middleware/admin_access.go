package middleware

import (
	"context"
	"net/http"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// AdminAccessChecker is independent from RBAC. The management-plane account
// gate runs before the route-specific permission check.
type AdminAccessChecker interface {
	CheckAdminAccess(context.Context, string) (bool, error)
}

func RequireAdminAccess(checker AdminAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := c.Get("user_id")
		userIDText, valid := userID.(string)
		if !ok || !valid || userIDText == "" {
			response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
			c.Abort()
			return
		}
		if checker == nil {
			response.Error(c, http.StatusServiceUnavailable, 10006, "administrator access check is unavailable")
			c.Abort()
			return
		}
		allowed, err := checker.CheckAdminAccess(c.Request.Context(), userIDText)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 10006, "administrator access check failed")
			c.Abort()
			return
		}
		if !allowed {
			response.Error(c, http.StatusForbidden, 20004, "administrator account is not active")
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireAdminAccessOrScopedPermission keeps category-scoped moderation routes
// available to moderators without turning those users into management-plane
// administrators. The route's normal permission middleware still performs the
// final permission and service-level resource-scope checks.
func RequireAdminAccessOrScopedPermission(checker AdminAccessChecker, permissions PermissionChecker, code, scopeType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := c.Get("user_id")
		userIDText, valid := userID.(string)
		if !ok || !valid || userIDText == "" {
			response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
			c.Abort()
			return
		}
		if checker == nil {
			response.Error(c, http.StatusServiceUnavailable, 10006, "administrator access check is unavailable")
			c.Abort()
			return
		}
		allowed, err := checker.CheckAdminAccess(c.Request.Context(), userIDText)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 10006, "administrator access check failed")
			c.Abort()
			return
		}
		if !allowed {
			scoped, supportsScoped := permissions.(ScopedPermissionCandidateChecker)
			if !supportsScoped || code == "" || scopeType == "" {
				response.Error(c, http.StatusForbidden, 20004, "administrator account is not active")
				c.Abort()
				return
			}
			allowed, err = scoped.HasAnyScopedPermissionCode(c.Request.Context(), userIDText, code, scopeType)
			if err != nil {
				response.Error(c, http.StatusInternalServerError, 10006, "scoped permission check failed")
				c.Abort()
				return
			}
		}
		if !allowed {
			response.Error(c, http.StatusForbidden, 20004, "administrator account is not active")
			c.Abort()
			return
		}
		c.Next()
	}
}
