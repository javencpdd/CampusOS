package richtext

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestAttachmentCopyHTTPRealSessionStrictDTOAndSafeAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	s, _, _, _, _, _ := articleCreateFixture(t)
	objects, _, _ := attachmentUploadObjectFixture(t, s)
	draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
	if err != nil {
		t.Fatal(err)
	}
	documents, d, _ := attachmentCopyDocuments(t, objects, articleCreateProof)
	s.SetPersonalDocumentAttachmentReader(&attachmentCopyReader{documents: documents})
	objects.puts = 0
	users, sessions := identityrepo.NewMemoryUserRepository(), identityrepo.NewMemorySessionRepository()
	jwt := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "attachment-copy-user-test", Issuer: "attachment-copy-user", Audience: "user", KeyID: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour}, auth.JWTConfig{Secret: "attachment-copy-admin-test", Issuer: "attachment-copy-admin", Audience: "admin", KeyID: "admin", AccessTTL: time.Hour})
	identity, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "attachment-copy-ip-test"})
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, id := range []string{"1001", "1002"} {
		u := &identitydomain.User{ID: id, Username: "copy-" + id, Email: "copy-" + id + "@example.test", Status: "active", AuthVersion: 1}
		if err := users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
		issued, err := identity.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Document copy"})
		if err != nil {
			t.Fatal(err)
		}
		tokens[id] = issued.AccessToken
	}
	body := fmt.Sprintf("{\"document_id\":%q,\"display_name\":\"public.pdf\"}", d.ID)
	serve := func(token string, proof bool, payload string) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if proof {
			h.SetSessionProof(identity, identity)
		}
		router := gin.New()
		router.POST("/copy/:id", middleware.JWTAuth(jwt.User, identity), h.AttachPersonalDocument)
		req := httptest.NewRequest(http.MethodPost, "/copy/"+draft.ThreadID+"?owner_id=1002&scope=admin", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
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
	check(serve(tokens["1002"], true, body), 403)
	check(serve(tokens["1001"], false, body), 401)
	admin, err := jwt.Admin.GenerateAccessToken("1001", "Admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	check(serve(admin, true, body), 401)
	check(serve(tokens["1001"], true, strings.TrimSuffix(body, "}")+",\"actor_id\":\"1002\"}"), 400)
	if objects.puts != 0 {
		t.Fatal("rejected ingress performed copy", objects.puts)
	}
	rec := serve(tokens["1001"], true, body)
	check(rec, 201)
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &envelope) != nil || len(envelope.Data) != 2 || envelope.Data["thread_id"] == nil || envelope.Data["attachment_id"] == nil {
		t.Fatal("private copy acknowledgement", rec.Body.String())
	}
	if _, err := sessions.RevokeByUser(ctx, "1001", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	check(serve(tokens["1001"], true, body), 401)
	if objects.puts != 1 {
		t.Fatal("revoked ingress performed copy", objects.puts)
	}
}
