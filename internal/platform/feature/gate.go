package feature

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type PathRule struct {
	Prefix            string
	FeatureID         string
	AllowWhenDisabled []AllowedPath
	// DisabledHandler lets the owning module preserve its stable error/cache
	// contract. The gate always aborts, including when the handler does not.
	DisabledHandler gin.HandlerFunc
}

// AllowedPath is an explicit, read-only route exception for a disabled
// feature. It keeps the route table static while allowing clients to discover
// that a feature is unavailable without probing a protected business path.
type AllowedPath struct {
	Method string
	Path   string
}

func (r PathRule) allowsWhenDisabled(method, path string) bool {
	for _, allowed := range r.AllowWhenDisabled {
		if strings.EqualFold(strings.TrimSpace(allowed.Method), method) && allowed.Path == path {
			return true
		}
	}
	return false
}

// PathGate keeps Gin routes static and rejects disabled feature requests.
func PathGate(registry *Registry, rules ...PathRule) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, rule := range rules {
			if strings.HasPrefix(c.Request.URL.Path, rule.Prefix) &&
				!registry.Enabled(rule.FeatureID) &&
				!rule.allowsWhenDisabled(c.Request.Method, c.Request.URL.Path) {
				if rule.DisabledHandler != nil {
					c.Abort()
					rule.DisabledHandler(c)
					return
				}
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 50301, "msg": "built-in feature is disabled", "data": gin.H{"feature_id": rule.FeatureID}})
				return
			}
		}
		c.Next()
	}
}
