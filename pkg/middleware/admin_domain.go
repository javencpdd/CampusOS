package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// AdminSessionFacts is the verified identity of one admin-domain session.
type AdminSessionFacts struct {
	AccountID          string
	Strength           string
	MFAAuthenticatedAt *time.Time
}

// AdminDomainSessionVerifier resolves one admin access session to current
// facts. Implementations wrap the identity AdminSessionService; user sessions
// must never verify here.
type AdminDomainSessionVerifier interface {
	VerifyAdminSession(context.Context, string) (AdminSessionFacts, error)
}

// AdminMFAPolicyGate decides whether an admin route demands MFA strength for
// this session. Nil means no additional demand beyond a valid admin session.
type AdminMFAPolicyGate func(AdminSessionFacts) bool

// RequireAdminDomainAccess gates management-plane routes on the independent
// admin identity domain: an admin-domain access JWT bound to a live admin
// session. User-domain tokens, expired sessions and deactivated accounts are
// all rejected with the same unauthenticated outcome.
func RequireAdminDomainAccess(jwt *auth.JWTManager, sessions AdminDomainSessionVerifier, mfaGate AdminMFAPolicyGate) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		if token == "" || token == header {
			response.ErrorDescriptor(c, apperror.AuthRequired, nil)
			c.Abort()
			return
		}
		claims, err := jwt.VerifyAccessToken(token)
		if err != nil || claims.SessionID == "" {
			response.ErrorDescriptor(c, apperror.AuthRequired, nil)
			c.Abort()
			return
		}
		if sessions == nil {
			response.Error(c, http.StatusServiceUnavailable, 10006, "admin session verification is unavailable")
			c.Abort()
			return
		}
		facts, err := sessions.VerifyAdminSession(c.Request.Context(), claims.SessionID)
		if err != nil {
			response.ErrorDescriptor(c, apperror.AuthRequired, nil)
			c.Abort()
			return
		}
		if mfaGate != nil && mfaGate(facts) {
			response.ErrorDescriptor(c, apperror.IdentityMFAStepUpRequired, nil)
			c.Abort()
			return
		}
		c.Set("user_id", facts.AccountID)
		c.Set("admin_account_id", facts.AccountID)
		c.Set("session_id", claims.SessionID)
		c.Set("authentication_strength", facts.Strength)
		c.Next()
	}
}

// AdminCodeChecker resolves one permission code for an admin-domain account
// through its active admin-role bindings.
type AdminCodeChecker interface {
	HasAdminCode(ctx context.Context, accountID, code string) (bool, error)
}

// RequireAdminDomainPermission authorizes one admin route by the admin
// domain's own role bindings. The legacy user catalog is never consulted for
// admin-domain principals.
func RequireAdminDomainPermission(checker AdminCodeChecker, code string) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID := c.GetString("admin_account_id")
		if accountID == "" || checker == nil {
			response.Error(c, http.StatusForbidden, 20004, "administrator account is not active")
			c.Abort()
			return
		}
		allowed, err := checker.HasAdminCode(c.Request.Context(), accountID, code)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 10006, "admin permission check failed")
			c.Abort()
			return
		}
		if !allowed {
			response.Error(c, http.StatusForbidden, 20004, "administrator account lacks the required permission")
			c.Abort()
			return
		}
		c.Next()
	}
}
