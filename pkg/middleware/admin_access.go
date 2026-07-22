package middleware

import (
	"context"
	"net/http"

	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// AdminAccessChecker is independent from RBAC. The management-plane account
// gate runs before the route-specific permission check.
type AdminAccessChecker interface {
	CheckAdminAccess(context.Context, string) (bool, error)
}

// AdminMFAChecker is optional during a rolling upgrade, but the production
// server always supplies it. It reads server-side Session state rather than
// accepting a browser assertion of MFA.
type AdminMFAChecker interface {
	CheckAdminMFA(context.Context, string, string) (bool, error)
}

func RequireAdminAccess(checker AdminAccessChecker, mfaCheckers ...AdminMFAChecker) gin.HandlerFunc {
	mfaChecker := firstAdminMFAChecker(mfaCheckers)
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
		if !requireRecentAdminMFA(c, mfaChecker, userIDText) {
			return
		}
		c.Next()
	}
}

// RequireAdminAccessOrScopedPermission keeps category-scoped moderation routes
// available to moderators without turning those users into management-plane
// administrators. The route's normal permission middleware still performs the
// final permission and service-level resource-scope checks.
func RequireAdminAccessOrScopedPermission(checker AdminAccessChecker, permissions PermissionChecker, code, scopeType string, mfaCheckers ...AdminMFAChecker) gin.HandlerFunc {
	mfaChecker := firstAdminMFAChecker(mfaCheckers)
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
		if allowed {
			if !requireRecentAdminMFA(c, mfaChecker, userIDText) {
				return
			}
		} else {
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

func firstAdminMFAChecker(values []AdminMFAChecker) AdminMFAChecker {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func requireRecentAdminMFA(c *gin.Context, checker AdminMFAChecker, userID string) bool {
	if checker == nil {
		return true
	}
	sessionID, exists := c.Get("session_id")
	sessionIDText, valid := sessionID.(string)
	if !exists || !valid || sessionIDText == "" {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		c.Abort()
		return false
	}
	allowed, err := checker.CheckAdminMFA(c.Request.Context(), userID, sessionIDText)
	if err != nil {
		response.ErrorDescriptor(c, apperror.IdentityMFAUnavailable, nil)
		c.Abort()
		return false
	}
	if !allowed {
		response.ErrorDescriptor(c, apperror.IdentityMFAStepUpRequired, nil)
		c.Abort()
		return false
	}
	return true
}
