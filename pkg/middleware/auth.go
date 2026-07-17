package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// AccessSessionVerifier binds JWT validation to a server-side session without
// making transport middleware depend on the Identity module's concrete service.
type AccessSessionVerifier interface {
	VerifyAccess(context.Context, *auth.JWTClaims) error
}

// JWTAuth JWT 认证中间件
func JWTAuth(jwtMgr *auth.JWTManager, verifiers ...AccessSessionVerifier) gin.HandlerFunc {
	verifier := firstSessionVerifier(verifiers)
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

		claims, err := verifyClaims(c, jwtMgr, verifier, tokenString)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, 20002, "invalid or expired token")
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("session_id", claims.SessionID)
		c.Set("auth_version", claims.AuthVersion)
		c.Next()
	}
}

// OptionalJWT enriches public runtime endpoints when a valid bearer token is
// present. Missing or invalid credentials remain anonymous and never grant access.
func OptionalJWT(jwtMgr *auth.JWTManager, verifiers ...AccessSessionVerifier) gin.HandlerFunc {
	verifier := firstSessionVerifier(verifiers)
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.Next()
			return
		}
		tokenString := strings.TrimPrefix(header, "Bearer ")
		if tokenString != header {
			if claims, err := verifyClaims(c, jwtMgr, verifier, tokenString); err == nil {
				c.Set("user_id", claims.UserID)
				c.Set("username", claims.Username)
				c.Set("session_id", claims.SessionID)
				c.Set("auth_version", claims.AuthVersion)
			}
		}
		c.Next()
	}
}

func firstSessionVerifier(values []AccessSessionVerifier) AccessSessionVerifier {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func verifyClaims(c *gin.Context, jwtMgr *auth.JWTManager, verifier AccessSessionVerifier, token string) (*auth.JWTClaims, error) {
	if jwtMgr == nil {
		return nil, errors.New("JWT manager is unavailable")
	}
	if verifier == nil {
		// Retain unit-test/source compatibility for middleware embedded outside a
		// CampusOS server. The application always supplies a session verifier.
		return jwtMgr.VerifyToken(token)
	}
	claims, err := jwtMgr.VerifyAccessToken(token)
	if err != nil {
		return nil, err
	}
	if err := verifier.VerifyAccess(c.Request.Context(), claims); err != nil {
		return nil, err
	}
	return claims, nil
}
