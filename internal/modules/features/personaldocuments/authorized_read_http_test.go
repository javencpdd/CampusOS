package personaldocuments

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestDocumentAuthenticatedHTTPRealSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	s := newTestService(t)
	d, err := s.CreateText(ctx, "1001", CreateRequest{Name: "private.txt", Format: FormatText, Content: "private-document-body"})
	if err != nil {
		t.Fatal(err)
	}
	users := identityrepo.NewMemoryUserRepository()
	sessions := identityrepo.NewMemorySessionRepository()
	jwt := auth.NewDomainJWTManagers(
		auth.JWTConfig{Secret: "document-user-test-secret", Issuer: "document-user", Audience: "user", KeyID: "user-test", AccessTTL: time.Hour, RefreshTTL: time.Hour},
		auth.JWTConfig{Secret: "document-admin-test-secret", Issuer: "document-admin", Audience: "admin", KeyID: "admin-test", AccessTTL: time.Hour},
	)
	identity, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "document-test-ip-secret"})
	if err != nil {
		t.Fatal(err)
	}
	user := &identitydomain.User{ID: "1001", Username: "doc-owner", Email: "doc-owner@example.test", Status: identitydomain.UserStatusActive, AuthVersion: 1}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	tokens, err := identity.Issue(ctx, user, identityservice.SessionMetadata{DeviceName: "Document Test"})
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token, path string, verifier identityport.SessionVerifier, strength identityport.SessionStrengthReader) *httptest.ResponseRecorder {
		h := NewHandler(s)
		h.SetSessionProof(verifier, strength)
		r := gin.New()
		g := r.Group("/documents", middleware.JWTAuth(jwt.User, identity))
		g.GET("", h.List)
		g.GET("/:id", h.Get)
		g.GET("/:id/content", h.Content)
		g.GET("/:id/preview", h.Preview)
		g.GET("/:id/download", h.Download)
		g.GET("/:id/versions", h.Versions)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Actor-ID", "1002")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	for _, path := range []string{"/documents", "/documents/" + d.ID, "/documents/" + d.ID + "/content?owner_id=1002&authentication_strength=mfa", "/documents/" + d.ID + "/preview", "/documents/" + d.ID + "/download", "/documents/" + d.ID + "/versions"} {
		t.Run(path, func(t *testing.T) {
			rec := serve(tokens.AccessToken, path, identity, identity)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if strings.HasSuffix(path, "/download") && (rec.Body.String() != "private-document-body" || rec.Header().Get("Cache-Control") != "private, no-store") {
				t.Fatal("unsafe download")
			}
			if rec := serve(tokens.AccessToken, path, nil, identity); rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "private-document-body") {
				t.Fatalf("missing verifier status=%d", rec.Code)
			}
			if rec := serve(tokens.AccessToken, path, identity, nil); rec.Code != http.StatusUnauthorized {
				t.Fatalf("missing strength status=%d", rec.Code)
			}
		})
	}
	admin, err := jwt.Admin.GenerateAccessToken(user.ID, "same-ID admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(admin, "/documents/"+d.ID+"/content", identity, identity); rec.Code != http.StatusUnauthorized {
		t.Fatalf("same-ID admin status=%d", rec.Code)
	}
	if _, err := sessions.RevokeByUser(ctx, user.ID, "document-test-revoke", time.Now()); err != nil {
		t.Fatal(err)
	}
	if rec := serve(tokens.AccessToken, "/documents/"+d.ID+"/download", identity, identity); rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "private-document-body") {
		t.Fatalf("revoked status=%d", rec.Code)
	}
}

func TestDocumentAuthenticatedHTTPRejectsRevocationDuringRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := newTestService(t)
	d, err := s.CreateText(context.Background(), "1001", CreateRequest{Name: "private.txt", Format: FormatText, Content: "private-document-body"})
	if err != nil {
		t.Fatal(err)
	}
	proof := &documentHTTPProof{}
	h := NewHandler(s)
	h.SetSessionProof(proof, proof)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "1001")
		c.Set("session_id", "verified-session")
		c.Set("auth_version", int64(1))
	})
	r.GET("/content", h.Content)
	r.GET("/download", h.Download)
	for _, path := range []string{"/content", "/download"} {
		proof.calls = 0
		// A tiny route adapter supplies the trusted resource parameter.
		route := gin.New()
		route.Use(func(c *gin.Context) {
			c.Set("user_id", "1001")
			c.Set("session_id", "verified-session")
			c.Set("auth_version", int64(1))
		})
		if path == "/content" {
			route.GET("/:id", h.Content)
		} else {
			route.GET("/:id", h.Download)
		}
		rec := httptest.NewRecorder()
		route.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+d.ID, nil))
		if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "private-document-body") {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

type documentHTTPProof struct{ calls int }

func (p *documentHTTPProof) VerifyAccess(context.Context, *auth.JWTClaims) error { return nil }
func (p *documentHTTPProof) AuthenticationStrengthForSession(context.Context, string) (string, error) {
	p.calls++
	if p.calls > 1 {
		return "", identityport.ErrUserSessionPrincipalUnavailable
	}
	return "password", nil
}
