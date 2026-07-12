package feature

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type PathRule struct {
	Prefix    string
	FeatureID string
}

// PathGate keeps Gin routes static and rejects disabled feature requests.
func PathGate(registry *Registry, rules ...PathRule) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, rule := range rules {
			if strings.HasPrefix(c.Request.URL.Path, rule.Prefix) && !registry.Enabled(rule.FeatureID) {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 50301, "msg": "built-in feature is disabled", "data": gin.H{"feature_id": rule.FeatureID}})
				return
			}
		}
		c.Next()
	}
}
