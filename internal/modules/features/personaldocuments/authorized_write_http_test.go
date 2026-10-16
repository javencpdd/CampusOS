package personaldocuments

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
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

func TestDocumentAuthorHTTPRealSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, operation := range []string{"create", "upload", "save", "restore_version", "trash", "restore"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			s, _ := newReliableTestService(t)
			users := identityrepo.NewMemoryUserRepository()
			sessions := identityrepo.NewMemorySessionRepository()
			jwt := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "document-writer-user-test-secret", Issuer: "document-writer-user", Audience: "user", KeyID: "user-test", AccessTTL: time.Hour, RefreshTTL: time.Hour}, auth.JWTConfig{Secret: "document-writer-admin-test-secret", Issuer: "document-writer-admin", Audience: "admin", KeyID: "admin-test", AccessTTL: time.Hour})
			identity, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "document-writer-test-ip"})
			if err != nil {
				t.Fatal(err)
			}
			tokens := map[string]string{}
			for _, id := range []string{"1001", "1002"} {
				u := &identitydomain.User{ID: id, Username: "writer" + id, Email: id + "@example.test", Status: "active", AuthVersion: 1}
				if err := users.Create(ctx, u); err != nil {
					t.Fatal(err)
				}
				pair, e := identity.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Document writer"})
				if e != nil {
					t.Fatal(e)
				}
				tokens[id] = pair.AccessToken
			}
			admin, err := jwt.Admin.GenerateAccessToken("1001", "same-ID admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			serve := func(token string, proof bool, extra string) *httptest.ResponseRecorder {
				d, e := s.CreateText(ctx, "1001", CreateRequest{Name: "private.txt", Format: FormatText, Content: "original body"})
				if e != nil {
					t.Fatal(e)
				}
				selected := d.CurrentVersionID
				if operation == "restore" {
					d, e = s.SetStatus(ctx, "1001", d.ID, d.Version, StatusTrashed)
					if e != nil {
						t.Fatal(e)
					}
				}
				h := NewHandler(s)
				if proof {
					h.SetSessionProof(identity, identity)
				}
				r := gin.New()
				g := r.Group("/documents", middleware.JWTAuth(jwt.User, identity))
				g.POST("", h.Create)
				g.POST("/upload", h.Upload)
				g.PUT("/:id", h.Save)
				g.POST("/:id/versions/:version_id/restore", h.RestoreVersion)
				g.POST("/:id/trash", h.Trash)
				g.POST("/:id/restore", h.Restore)
				method, path, body := http.MethodPost, "/documents", `{"name":"new.txt","format":"text","content":"body"`+extra+`}`
				switch operation {
				case "save":
					method, path, body = http.MethodPut, "/documents/"+d.ID, fmt.Sprintf(`{"expected_version":%d,"content":"next"%s}`, d.Version, extra)
				case "restore_version":
					path, body = "/documents/"+d.ID+"/versions/"+selected+"/restore", fmt.Sprintf(`{"expected_version":%d%s}`, d.Version, extra)
				case "trash", "restore":
					path, body = "/documents/"+d.ID+"/"+operation, fmt.Sprintf(`{"expected_version":%d%s}`, d.Version, extra)
				}
				var reader io.Reader = strings.NewReader(body)
				contentType := "application/json"
				if operation == "upload" {
					var buffer bytes.Buffer
					writer := multipart.NewWriter(&buffer)
					file, e := writer.CreateFormFile("file", "new.pdf")
					if e != nil {
						t.Fatal(e)
					}
					_, _ = io.WriteString(file, safeTestPDF)
					if e = writer.Close(); e != nil {
						t.Fatal(e)
					}
					reader = &buffer
					contentType = writer.FormDataContentType()
					path = "/documents/upload"
				}
				req := httptest.NewRequest(method, path, reader)
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", contentType)
				req.Header.Set("X-Actor-ID", "1002")
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				return rec
			}
			want := http.StatusOK
			if operation == "create" || operation == "upload" {
				want = http.StatusCreated
			}
			if rec := serve(tokens["1001"], true, ""); rec.Code != want {
				t.Fatal(rec.Code, rec.Body.String())
			}
			if rec := serve(tokens["1001"], false, ""); rec.Code != 401 {
				t.Fatal("missing proof", rec.Code, rec.Body.String())
			}
			if rec := serve(admin, true, ""); rec.Code != 401 {
				t.Fatal("admin", rec.Code, rec.Body.String())
			}
			if operation != "create" && operation != "upload" {
				if rec := serve(tokens["1002"], true, ""); rec.Code != 404 {
					t.Fatal("other owner", rec.Code, rec.Body.String())
				}
			}
			if operation != "upload" {
				if rec := serve(tokens["1001"], true, `,"owner_id":"1002"`); rec.Code != 400 {
					t.Fatal("forged DTO", rec.Code, rec.Body.String())
				}
			}
			if _, err := sessions.RevokeByUser(ctx, "1001", "writer-test-revoke", time.Now()); err != nil {
				t.Fatal(err)
			}
			if rec := serve(tokens["1001"], true, ""); rec.Code != 401 {
				t.Fatal("revoked", rec.Code, rec.Body.String())
			}
		})
	}
}
