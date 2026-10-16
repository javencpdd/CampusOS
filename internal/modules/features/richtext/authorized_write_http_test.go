package richtext

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestRichTextAuthorWriteHTTPRealCurrentProof(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, operation := range []string{"update", "publish", "offline", "trash"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			s, _, _, _, _, _ := articleCreateFixture(t)
			created, err := s.CreateDraft(ctx, "1001", "Owner", articleCreateRequest())
			if err != nil {
				t.Fatal(err)
			}
			users := identityrepo.NewMemoryUserRepository()
			sessions := identityrepo.NewMemorySessionRepository()
			jwt := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "article-write-user-test-secret", Issuer: "article-write-user", Audience: "user", KeyID: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour}, auth.JWTConfig{Secret: "article-write-admin-test-secret", Issuer: "article-write-admin", Audience: "admin", KeyID: "admin", AccessTTL: time.Hour})
			identity, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "article-write-test-ip-secret"})
			if err != nil {
				t.Fatal(err)
			}
			tokens := map[string]string{}
			for _, id := range []string{"1001", "1002"} {
				u := &identitydomain.User{ID: id, Username: "article-writer-" + id, Email: "article-writer-" + id + "@example.test", Status: "active", AuthVersion: 1}
				if err := users.Create(ctx, u); err != nil {
					t.Fatal(err)
				}
				issued, err := identity.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Article Writer"})
				if err != nil {
					t.Fatal(err)
				}
				tokens[id] = issued.AccessToken
			}
			serve := func(token string, proof bool, body string) *httptest.ResponseRecorder {
				h := NewHandler(s)
				if proof {
					h.SetSessionProof(identity, identity)
				}
				handler := h.UpdateDraft
				if operation == "publish" {
					handler = h.Publish
				}
				if operation == "offline" {
					handler = h.Offline
				}
				if operation == "trash" {
					handler = h.Delete
				}
				router := gin.New()
				router.POST("/write/:id", middleware.JWTAuth(jwt.User, identity), handler)
				req := httptest.NewRequest(http.MethodPost, "/write/"+created.ThreadID+"?owner_id=1002&action=admin", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Actor-ID", "1002")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				return rec
			}
			body := `{"title":"Changed","content_html":"<p>safe updated</p>"}`
			if rec := serve(tokens["1002"], true, body); rec.Code != 403 {
				t.Fatal("other", rec.Code, rec.Body.String())
			}
			if rec := serve(tokens["1001"], false, body); rec.Code != 401 {
				t.Fatal("proof", rec.Code, rec.Body.String())
			}
			admin, err := jwt.Admin.GenerateAccessToken("1001", "Admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			if rec := serve(admin, true, body); rec.Code != 401 {
				t.Fatal("same-ID Admin", rec.Code, rec.Body.String())
			}
			if operation == "update" {
				if rec := serve(tokens["1001"], true, strings.TrimSuffix(body, "}")+`,"created_by":"1002"}`); rec.Code != 400 {
					t.Fatal("DTO", rec.Code, rec.Body.String())
				}
			}
			expected := 200
			if operation == "trash" {
				expected = 204
			}
			if rec := serve(tokens["1001"], true, body); rec.Code != expected {
				t.Fatal("owner", rec.Code, rec.Body.String())
			}
			if _, err := sessions.RevokeByUser(ctx, "1001", "test-revoke", time.Now()); err != nil {
				t.Fatal(err)
			}
			if rec := serve(tokens["1001"], true, body); rec.Code != 401 {
				t.Fatal("revoke", rec.Code, rec.Body.String())
			}
		})
	}
}
