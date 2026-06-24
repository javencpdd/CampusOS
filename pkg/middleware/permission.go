package middleware

import (
	"net/http"

	"github.com/campusos/CampusOS/internal/core/identity/service"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// RequirePermission 权限检查中间件
func RequirePermission(permSvc *service.PermissionService, resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
			c.Abort()
			return
		}

		hasPermission, err := permSvc.Check(c.Request.Context(), userID.(string), resource, action)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 10006, "permission check failed")
			c.Abort()
			return
		}

		if !hasPermission {
			response.Error(c, http.StatusForbidden, 20004, "permission denied")
			c.Abort()
			return
		}

		c.Next()
	}
}
