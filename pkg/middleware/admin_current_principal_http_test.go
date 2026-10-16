package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestAdminCurrentPrincipalHTTPDomainAndMFA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"fresh", "password", "expired_mfa", "nil_mfa", "future_mfa", "subject_swap", "user_token", "session_revoked", "account_suspended"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now().UTC().Truncate(time.Microsecond)
			admins := repository.NewMemoryAdminDomainRepository()
			sessions := repository.NewMemoryAdminSessionRepository()
			const subject = "1001"
			const sessionID = "admin-http-session"
			if err := admins.CreateAccount(ctx, repository.AdminDomainAccount{ID: subject, Username: "current-admin", Email: "current-admin@example.test"}); err != nil {
				t.Fatal(err)
			}
			at := now.Add(-time.Minute)
			mfaAt := &at
			strength := "mfa"
			if mode == "password" {
				strength = "password"
				mfaAt = nil
			}
			if mode == "expired_mfa" {
				at = now.Add(-service.AdminGovernanceMFAWindow)
			}
			if mode == "nil_mfa" {
				mfaAt = nil
			}
			if mode == "future_mfa" {
				at = now.Add(time.Second)
			}
			if err := sessions.CreateSession(ctx, repository.AdminSession{ID: sessionID, AccountID: subject, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: sessionID, AuthenticationStrength: strength, MFAAuthenticatedAt: mfaAt, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			domains := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "current-admin-user-test", Issuer: "current-user", Audience: "user", KeyID: "user", AccessTTL: time.Hour}, auth.JWTConfig{Secret: "current-admin-admin-test", Issuer: "current-admin", Audience: "admin", KeyID: "admin", AccessTTL: time.Hour})
			svc := service.NewAdminSessionService(admins, sessions, domains.Admin)
			svc.SetClock(func() time.Time { return now })
			tokenSubject := subject
			if mode == "subject_swap" {
				tokenSubject = "1002"
			}
			manager := domains.Admin
			if mode == "user_token" {
				manager = domains.User
			}
			token, err := manager.GenerateAccessToken(tokenSubject, "name", auth.AccessTokenContext{SessionID: sessionID, AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "session_revoked" {
				if _, _, err := sessions.RevokeSessionCAS(ctx, sessionID, 1, "test", now); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "account_suspended" {
				if _, _, err := admins.SetAccountStatusCAS(ctx, subject, 1, "suspended", "test", subject, now); err != nil {
					t.Fatal(err)
				}
			}
			r := gin.New()
			r.Use(middleware.RequireAdminDomainAccess(domains.Admin, portadapt.NewAdminDomainSessionVerifier(svc), func(f middleware.AdminSessionFacts) bool { return f.Strength != "mfa" }))
			r.POST("/governance-proof", func(c *gin.Context) {
				p, err := identityport.ResolveAdminSessionPrincipal(c.Request.Context(), svc, &auth.JWTClaims{UserID: c.GetString("admin_account_id"), SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType})
				if err != nil {
					t.Fatal(err)
				}
				if p.Actor.Kind != "admin" || p.Actor.ID != subject || p.AuthenticationStrength != "mfa" {
					t.Fatal(p)
				}
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/governance-proof", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Actor-ID", "1002")
			req.Header.Set("X-Authentication-Strength", "mfa")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			want := 401
			if mode == "fresh" {
				want = 200
			}
			if mode == "password" || mode == "expired_mfa" {
				want = 403
			}
			if rec.Code != want {
				t.Fatal(mode, rec.Code, rec.Body.String())
			}
		})
	}
}
