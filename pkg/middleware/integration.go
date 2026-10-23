package middleware

import (
	"strings"

	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// IntegrationToken parses only the independent machine scheme. The Identity
// gateway still verifies the digest/current rows and resource whitelist.
func IntegrationToken(c *gin.Context) (string, bool) {
	c.Header("Cache-Control", "private, no-store")
	values := c.Request.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Integration ") || strings.ContainsAny(values[0], "\r\n\t,") || strings.Count(values[0], " ") != 1 || c.Request.URL.RawQuery != "" || c.GetHeader("Origin") != "" || c.GetHeader("Cookie") != "" {
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		return "", false
	}
	return strings.TrimPrefix(values[0], "Integration "), true
}
