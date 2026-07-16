package middleware

import (
	"context"
	"net/http"

	"github.com/campusos/CampusOS/internal/modules/core/identity/permissioncode"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// PermissionChecker is the public authorization contract used by HTTP
// middleware. It prevents transports from depending on Identity's concrete
// application service.
type PermissionChecker interface {
	Check(context.Context, string, string, string) (bool, error)
}

// PermissionCodeChecker is the additive v10 contract. Implementations that do
// not yet expose the catalog remain supported through PermissionChecker.
type PermissionCodeChecker interface {
	CheckCode(context.Context, string, string) (bool, error)
}

// ScopedPermissionCandidateChecker supports route gates for actions whose
// resource scope is resolved only inside the application service. It answers
// whether a caller has the code in at least one scope; it must never be used
// as the final authorization decision.
type ScopedPermissionCandidateChecker interface {
	HasAnyScopedPermissionCode(context.Context, string, string, string) (bool, error)
}

// RouteDecisionRecorder is deliberately transport-neutral so middleware can
// persist a minimal authorization decision without importing Identity types.
type RouteDecisionRecorder interface {
	RecordHTTPAuthorizationDecision(context.Context, string, string, string, string, string, string, string)
}

// RequirePermission 权限检查中间件
func RequirePermission(permSvc PermissionChecker, resource, action string) gin.HandlerFunc {
	return RequirePermissionForOperation(permSvc, resource, action, permissioncode.FromLegacy(resource, action), "")
}

// RequirePermissionForOperation binds a legacy compatibility pair, its stable
// permission code and a stable route operation. The code path is preferred
// when available; the legacy pair prevents an incomplete rolling migration
// from silently widening access.
func RequirePermissionForOperation(permSvc PermissionChecker, resource, action, code, operation string) gin.HandlerFunc {
	return requirePermissionForOperation(permSvc, resource, action, code, operation, "")
}

// RequireScopedPermissionForOperation keeps category-scoped management
// routes reachable for a moderator while requiring the service to validate
// the concrete stored category before executing the command.
func RequireScopedPermissionForOperation(permSvc PermissionChecker, resource, action, code, operation, scopeType string) gin.HandlerFunc {
	return requirePermissionForOperation(permSvc, resource, action, code, operation, scopeType)
}

func requirePermissionForOperation(permSvc PermissionChecker, resource, action, code, operation, scopeType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
			c.Abort()
			return
		}

		userIDText, ok := userID.(string)
		if !ok || userIDText == "" {
			response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
			c.Abort()
			return
		}
		hasPermission := false
		var err error
		if codeChecker, supportsCodes := permSvc.(PermissionCodeChecker); supportsCodes && code != "" {
			hasPermission, err = codeChecker.CheckCode(c.Request.Context(), userIDText, code)
		} else {
			hasPermission, err = permSvc.Check(c.Request.Context(), userIDText, resource, action)
		}
		if err != nil {
			recordRouteDecision(c, permSvc, userIDText, code, operation, "error", "permission check failed")
			response.Error(c, http.StatusInternalServerError, 10006, "permission check failed")
			c.Abort()
			return
		}

		if !hasPermission && scopeType != "" {
			if scopedChecker, supportsScoped := permSvc.(ScopedPermissionCandidateChecker); supportsScoped && code != "" {
				candidate, candidateErr := scopedChecker.HasAnyScopedPermissionCode(c.Request.Context(), userIDText, code, scopeType)
				if candidateErr != nil {
					recordRouteDecision(c, permSvc, userIDText, code, operation, "error", "scoped permission candidate check failed")
					response.Error(c, http.StatusInternalServerError, 10006, "permission check failed")
					c.Abort()
					return
				}
				if candidate {
					recordRouteDecision(c, permSvc, userIDText, code, operation, "allow", "scope candidate; resource check required")
					c.Next()
					return
				}
			}
		}

		if !hasPermission {
			recordRouteDecision(c, permSvc, userIDText, code, operation, "deny", "permission denied")
			response.Error(c, http.StatusForbidden, 20004, "permission denied")
			c.Abort()
			return
		}

		recordRouteDecision(c, permSvc, userIDText, code, operation, "allow", "")
		c.Next()
	}
}

func recordRouteDecision(c *gin.Context, checker PermissionChecker, actorID, code, operation, outcome, reason string) {
	recorder, ok := checker.(RouteDecisionRecorder)
	if !ok {
		return
	}
	requestID, _ := c.Get("trace_id")
	requestIDText, _ := requestID.(string)
	recorder.RecordHTTPAuthorizationDecision(c.Request.Context(), actorID, code, operation, outcome, reason, requestIDText, c.ClientIP())
}
