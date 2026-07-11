package middleware

import (
	"net/http"
	"strings"

	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// JWTAuth JWT 认证中间件
func JWTAuth(jwtMgr *auth.JWTManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			response.Error(c, http.StatusUnauthorized, 20001, "missing authorization header")
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(header, "Bearer ")
		if tokenString == header {
			response.Error(c, http.StatusUnauthorized, 20001, "invalid authorization format")
			c.Abort()
			return
		}

		claims, err := jwtMgr.VerifyToken(tokenString)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, 20002, "invalid or expired token")
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}
