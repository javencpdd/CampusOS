package richtext

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestAttachmentUploadHTTPRealSessionStrictMultipartAndSafeAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	s, _, _, _, _, _ := articleCreateFixture(t)
	objects, _, _ := attachmentUploadObjectFixture(t, s)
	draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
	if err != nil {
		t.Fatal(err)
	}
	users, sessions := identityrepo.NewMemoryUserRepository(), identityrepo.NewMemorySessionRepository()
	jwt := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "attachment-upload-user-test", Issuer: "attachment-upload-user", Audience: "user", KeyID: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour}, auth.JWTConfig{Secret: "attachment-upload-admin-test", Issuer: "attachment-upload-admin", Audience: "admin", KeyID: "admin", AccessTTL: time.Hour})
	identity, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "attachment-upload-ip-test"})
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, id := range []string{"1001", "1002"} {
		u := &identitydomain.User{ID: id, Username: "upload-" + id, Email: "upload-" + id + "@example.test", Status: "active", AuthVersion: 1}
		if err := users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
		issued, err := identity.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Attachment upload"})
		if err != nil {
			t.Fatal(err)
		}
		tokens[id] = issued.AccessToken
	}
	serve := func(token string, proof bool, mode string) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if proof {
			h.SetSessionProof(identity, identity)
		}
		router := gin.New()
		router.POST("/upload/:id", middleware.JWTAuth(jwt.User, identity), h.UploadAttachment)
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		writeFile := func(name string) {
			part, err := form.CreateFormFile("file", name)
			if err != nil {
				t.Fatal(err)
			}
			payload := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
			if mode == "unsafe" {
				payload = []byte("untrusted bytes")
			}
			if _, err := part.Write(payload); err != nil {
				t.Fatal(err)
			}
		}
		writeFile("upload.pdf")
		if mode == "actor" {
			if err := form.WriteField("actor_id", "1002"); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "multiple" {
			writeFile("second.pdf")
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/upload/"+draft.ThreadID+"?owner_id=1002&scope=admin", &body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("X-Actor-ID", "1002")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	check := func(rec *httptest.ResponseRecorder, want int) {
		t.Helper()
		if rec.Code != want {
			t.Fatal(rec.Code, want, rec.Body.String())
		}
	}
	check(serve(tokens["1002"], true, "valid"), 403)
	check(serve(tokens["1001"], false, "valid"), 401)
	admin, err := jwt.Admin.GenerateAccessToken("1001", "Admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	check(serve(admin, true, "valid"), 401)
	check(serve(tokens["1001"], true, "actor"), 400)
	check(serve(tokens["1001"], true, "multiple"), 400)
	check(serve(tokens["1001"], true, "unsafe"), 400)
	if objects.puts != 0 {
		t.Fatal("rejected ingress performed Object I/O", objects.puts)
	}
	rec := serve(tokens["1001"], true, "valid")
	check(rec, 201)
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &envelope) != nil || len(envelope.Data) != 2 || envelope.Data["thread_id"] == nil || envelope.Data["attachment_id"] == nil {
		t.Fatal("unsafe upload acknowledgement", rec.Body.String())
	}
	if _, err := sessions.RevokeByUser(ctx, "1001", "test revoke", time.Now()); err != nil {
		t.Fatal(err)
	}
	check(serve(tokens["1001"], true, "valid"), 401)
	if objects.puts != 1 {
		t.Fatal("revoked session reached Object provider", objects.puts)
	}
}
