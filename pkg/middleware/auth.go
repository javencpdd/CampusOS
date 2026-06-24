package middleware

import (
	"net/http"
	"strings"

	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/gin-gonic/gin"
)

// JWTAuth JWT 认证中间件
func JWTAuth(jwtMgr *auth.JWTManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "missing authorization header"})
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(header, "Bearer ")
		if tokenString == header {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "invalid authorization format"})
			c.Abort()
			return
		}

		claims, err := jwtMgr.VerifyToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 20002, "msg": "invalid or expired token"})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}
