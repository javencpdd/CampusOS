package space

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func mustCurrentReadClaims(t *testing.T, e *ownContentsHTTPEnvironment) *auth.JWTClaims {
	t.Helper()
	c, err := e.jwts.User.VerifyAccessToken(e.tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestSpaceCurrentReadHTTPActualSixRoutesStrictSafeCurrentSessionAndSource(t *testing.T) {
	for _, path := range []string{"/space/98431", "/u/owner", "/space/98431/contents", "/u/owner/contents", "/spaces/me/contents", "/spaces/me/sync-status"} {
		for _, mode := range []string{"current", "body", "feature", "missing_source", "admin_token", "revoked"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				s, e := currentReadContentService(t)
				h := NewHandler(s)
				h.SetOwnerSessionProof(e.identity, e.identity)
				if mode == "feature" {
					s.SetPluginEnabledChecker(func() bool { return false })
				}
				if mode == "missing_source" {
					s.users = nil
				}
				if mode == "revoked" {
					if err := e.revoke(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				r := gin.New()
				r.GET("/space/:user_id", h.GetByUserID)
				r.GET("/u/:username", h.GetByUsername)
				r.GET("/space/:user_id/contents", h.ListContentsByUserID)
				r.GET("/u/:username/contents", h.ListContentsByUsername)
				own := r.Group("")
				own.Use(middleware.JWTAuth(e.jwts.User, e.identity))
				own.GET("/spaces/me/contents", h.ListOwnContents)
				own.GET("/spaces/me/sync-status", h.GetSyncStatus)
				body := ""
				if mode == "body" {
					body = `{"owner_id":"98432"}`
				}
				req := httptest.NewRequest(http.MethodGet, path, strings.NewReader(body))
				token := e.tokens.AccessToken
				if mode == "admin_token" {
					var err error
					token, err = e.jwts.Admin.GenerateAccessToken(e.owner.ID, "Admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
					if err != nil {
						t.Fatal(err)
					}
				}
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				owner := strings.HasPrefix(path, "/spaces/me/")
				want := 200
				switch {
				case owner && (mode == "revoked" || mode == "admin_token"):
					want = 401
				case mode == "body":
					want = 400
				case mode == "feature":
					want = 503
				case mode == "missing_source":
					want = 503
					if owner && strings.HasSuffix(path, "/contents") {
						want = 500
					}
				}
				if rec.Code != want {
					t.Fatal(rec.Code, want, rec.Body.String())
				}
				if rec.Code != 401 && rec.Header().Get("Cache-Control") != "private, no-store" {
					t.Fatal("cacheable current read", rec.Header())
				}
				for _, secret := range []string{"other-private-body", "password_hash", "sync_categories", "disabled_actor", "facts_version"} {
					if strings.Contains(rec.Body.String(), secret) {
						t.Fatal("private wire field", secret, rec.Body.String())
					}
				}
				if (!owner || want != 200) && (strings.Contains(rec.Body.String(), "owner-private-body") || strings.Contains(rec.Body.String(), "PRIVATE GOVERNANCE")) {
					t.Fatal("private content leaked", rec.Body.String())
				}
				if mode == "current" && owner && strings.HasSuffix(path, "/contents") && !strings.Contains(rec.Body.String(), "owner-private-body") {
					t.Fatal("owner lost private projection", rec.Body.String())
				}
			})
		}
	}
}
func TestSpaceCurrentReadOwnerProfileFullUserChangesClose(t *testing.T) {
	s, repo, _ := spaceOwnerFixture(t, true, "")
	s.repo = &currentReadSpaceHook{MemoryRepository: repo, at: 1, after: func() { s.users.(*fakeUserLookup).byID["1001"].Email = "changed@example.test" }}
	result, err := s.GetOwnSpaceForPrincipal(context.Background(), spaceOwnerProof)
	if err == nil || result != nil {
		t.Fatal("owner profile failed to bind full current User", result, err)
	}
	if _, err := s.GetSyncStatusForPrincipal(context.Background(), func(context.Context) (identityport.PrincipalContext, error) {
		p, _ := spaceOwnerProof(context.Background())
		p.Actor.Kind, p.Audience = "admin", "admin"
		return p, nil
	}); err == nil {
		t.Fatal("admin reused owner sync status")
	}
}
