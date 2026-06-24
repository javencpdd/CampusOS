package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// APIKeyAuth API Key 认证中间件
func APIKeyAuth(keyRepo plugin.APIKeyRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 Authorization 头提取 API Key
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// 也支持 X-API-Key 头
			authHeader = c.GetHeader("X-API-Key")
			if authHeader == "" {
				c.Next()
				return
			}
		}

		// 支持 "Bearer pk_xxx" 或直接 "pk_xxx"
		apiKey := strings.TrimPrefix(authHeader, "Bearer ")
		apiKey = strings.TrimSpace(apiKey)

		if apiKey == "" {
			c.Next()
			return
		}

		rec, err := keyRepo.GetByKey(c.Request.Context(), apiKey)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, 20005, "invalid API key")
			c.Abort()
			return
		}

		if !rec.IsActive {
			response.Error(c, http.StatusUnauthorized, 20005, "API key is deactivated")
			c.Abort()
			return
		}

		if rec.ExpiresAt != nil && rec.ExpiresAt.Before(time.Now()) {
			response.Error(c, http.StatusUnauthorized, 20005, "API key has expired")
			c.Abort()
			return
		}

		// 注入 API Key 信息到 context
		c.Set("api_key", rec.Key)
		c.Set("api_key_name", rec.Name)
		if rec.PluginName != nil {
			c.Set("api_key_plugin", *rec.PluginName)
		}

		// 更新最后使用时间（异步）
		go keyRepo.UpdateLastUsed(c.Request.Context(), apiKey)

		c.Next()
	}
}
