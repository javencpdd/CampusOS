package richtext

import (
	"bytes"
	"context"
	"encoding/json"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInlineImageUploadHTTPCurrentSessionsStrictFieldsAndCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scope := range []string{"content", "rich_inventory", "rich_article"} {
		t.Run(scope, func(t *testing.T) {
			ctx := context.Background()
			s, _, _, _, _, _ := articleCreateFixture(t)
			adapter, objects := inlineUploadStorage(t, s)
			draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
			if err != nil {
				t.Fatal(err)
			}
			images, err := corestorage.NewContentImageStore(adapter, adapter, 1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			images.SetUploadCommand(s.reliable, objects)
			users, sessions := identityrepo.NewMemoryUserRepository(), identityrepo.NewMemorySessionRepository()
			jwt := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "inline-image-user-test", Issuer: "inline-user", Audience: "user", KeyID: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour}, auth.JWTConfig{Secret: "inline-image-admin-test", Issuer: "inline-admin", Audience: "admin", KeyID: "admin", AccessTTL: time.Hour})
			identity, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "inline-image-ip-test"})
			if err != nil {
				t.Fatal(err)
			}
			tokens := map[string]string{}
			for _, id := range []string{"1001", "1002"} {
				u := &identitydomain.User{ID: id, Username: "image-" + id, Email: "image-" + id + "@example.test", Status: "active", AuthVersion: 1}
				if err := users.Create(ctx, u); err != nil {
					t.Fatal(err)
				}
				issued, err := identity.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Image upload"})
				if err != nil {
					t.Fatal(err)
				}
				tokens[id] = issued.AccessToken
			}
			serve := func(token, mode string, proof bool) *httptest.ResponseRecorder {
				r := gin.New()
				if scope == "content" {
					h := corestorage.NewHandler(images)
					if proof {
						h.SetReadProof(nil, identity, identity)
					}
					r.POST("/upload", middleware.JWTAuth(jwt.User, identity), h.UploadContentImage)
				} else {
					h := NewHandler(s)
					if proof {
						h.SetSessionProof(identity, identity)
					}
					r.POST("/upload", middleware.JWTAuth(jwt.User, identity), h.UploadAsset)
				}
				var body bytes.Buffer
				form := multipart.NewWriter(&body)
				file, err := form.CreateFormFile("file", "../../../untrusted-name.png")
				if err != nil {
					t.Fatal(err)
				}
				payload := tinyPNG
				if mode == "invalid" {
					payload = []byte("untrusted content")
				}
				if _, err := file.Write(payload); err != nil {
					t.Fatal(err)
				}
				if mode == "multiple" {
					file, err = form.CreateFormFile("file", "other.png")
					if err != nil {
						t.Fatal(err)
					}
					if _, err = file.Write(tinyPNG); err != nil {
						t.Fatal(err)
					}
				}
				if scope == "rich_article" {
					if err = form.WriteField("thread_id", draft.ThreadID); err != nil {
						t.Fatal(err)
					}
					if err = form.WriteField("article_content_id", draft.Article.ID); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "actor" {
					if err = form.WriteField("owner_id", "1002"); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "binding_only" {
					if err = form.WriteField("article_content_id", draft.Article.ID); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "duplicate" {
					if err = form.WriteField("thread_id", draft.ThreadID); err != nil {
						t.Fatal(err)
					}
					if err = form.WriteField("thread_id", draft.ThreadID); err != nil {
						t.Fatal(err)
					}
				}
				if err = form.Close(); err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, "/upload?owner_id=1002&scope=admin", &body)
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", form.FormDataContentType())
				req.Header.Set("X-Actor-ID", "1002")
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				return rec
			}
			check := func(rec *httptest.ResponseRecorder, want int) {
				t.Helper()
				if rec.Code != want {
					t.Fatal(scope, want, rec.Code, rec.Body.String())
				}
				if strings.Contains(rec.Body.String(), adapter.Root()) || strings.Contains(rec.Body.String(), "untrusted-name") {
					t.Fatal("internal/client path exposed", rec.Body.String())
				}
			}
			check(serve(tokens["1001"], "valid", false), 401)
			admin, err := jwt.Admin.GenerateAccessToken("1001", "Admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			check(serve(admin, "valid", true), 401)
			for _, mode := range []string{"actor", "multiple", "duplicate", "binding_only", "invalid"} {
				check(serve(tokens["1001"], mode, true), 400)
			}
			if scope == "rich_article" {
				check(serve(tokens["1002"], "valid", true), 403)
			}
			if usage, err := adapter.Usage("1001"); err != nil || usage != 0 {
				t.Fatal("rejected ingress wrote files", usage, err)
			}
			rec := serve(tokens["1001"], "valid", true)
			check(rec, 200)
			var result struct{ Data map[string]json.RawMessage }
			if json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.Data["file_url"] == nil || result.Data["file_name"] == nil {
				t.Fatal("editor metadata missing", rec.Body.String())
			}
			var url string
			if json.Unmarshal(result.Data["file_url"], &url) != nil || !strings.Contains(url, "/1001/") {
				t.Fatal("actor spoof changed owner", rec.Body.String())
			}
			for _, key := range []string{"storage_key", "facts_version", "actor_id", "physical_path", "bytes", "object_id"} {
				if result.Data[key] != nil {
					t.Fatal("unsafe image response", key)
				}
			}
			before, _ := adapter.Usage("1001")
			if _, err := sessions.RevokeByUser(ctx, "1001", "test image revoke", time.Now()); err != nil {
				t.Fatal(err)
			}
			check(serve(tokens["1001"], "valid", true), 401)
			if usage, _ := adapter.Usage("1001"); usage != before {
				t.Fatal("revoked session wrote another file", usage, before)
			}
		})
	}
}
