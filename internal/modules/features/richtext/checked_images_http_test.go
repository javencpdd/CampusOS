package richtext

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	communitysvc "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestInlineImageHTTPRealSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	s, repo := newTestService(true)
	a, core, draft := inlineImageFixture(t, s, "1001", "1")
	users := identityrepo.NewMemoryUserRepository()
	sessions := identityrepo.NewMemorySessionRepository()
	jwt := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "inline-image-user-test-secret", Issuer: "inline-user", Audience: "user", KeyID: "user-key", AccessTTL: time.Hour}, auth.JWTConfig{Secret: "inline-image-admin-test-secret", Issuer: "inline-admin", Audience: "admin", KeyID: "admin-key", AccessTTL: time.Hour})
	proof, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "inline-image-test-ip-secret"})
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, id := range []string{"1001", "1002"} {
		u := &identitydomain.User{ID: id, Username: "image-user-" + id, Email: "image-" + id + "@example.test", Status: identitydomain.UserStatusActive, AuthVersion: 1}
		if err := users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
		issued, err := proof.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Images"})
		if err != nil {
			t.Fatal(err)
		}
		tokens[id] = issued.AccessToken
	}
	admin, err := jwt.Admin.GenerateAccessToken("1001", "admin", auth.AccessTokenContext{SessionID: "admin-image-session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	rich := NewHandler(s)
	rich.SetSessionProof(proof, proof)
	storage := corestorage.NewHandler(s.contentImages)
	storage.SetReadProof(community.NewContentQuery(communitysvc.NewThreadService(repo, nil)).(communityport.AuthenticatedThreadQuery), proof, proof)
	r := gin.New()
	r.GET("/richtext/:user_id/:filename", middleware.OptionalJWT(jwt.User, proof), rich.ServeAsset)
	r.GET("/content/:user_id/:filename", middleware.OptionalJWT(jwt.User, proof), storage.ServeContentImage)
	r.GET("/richtext/me", middleware.JWTAuth(jwt.User, proof), rich.ListMyAssets)
	r.GET("/content/me", middleware.JWTAuth(jwt.User, proof), storage.ListMyContentImages)
	serve := func(token, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("X-Owner-ID", "1001")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	direct := []string{"/richtext/1001/" + a.FileName, "/content/1001/" + core.FileName}
	parent := []string{"/richtext/1001/" + a.FileName + "?thread_id=" + draft.ThreadID, "/richtext/1001/" + core.FileName + "?store=content&thread_id=" + draft.ThreadID}
	for _, path := range append(append([]string{}, direct...), parent...) {
		for _, tc := range []struct {
			token  string
			status int
		}{{tokens["1001"], 200}, {tokens["1002"], 404}, {"", 404}, {admin, 401}} {
			rec := serve(tc.token, path)
			if rec.Code != tc.status {
				t.Fatalf("%s code=%d expected=%d body=%s", path, rec.Code, tc.status, rec.Body.String())
			}
			if rec.Code == 200 && (rec.Header().Get("Cache-Control") != "private, no-store" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("\x89PNG"))) {
				t.Fatal("unsafe image response")
			}
		}
	}
	for _, path := range []string{"/content/me", "/richtext/me"} {
		if rec := serve(tokens["1001"], path); rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	if _, err := s.Publish(ctx, draft.ThreadID, "1001"); err != nil {
		t.Fatal(err)
	}
	for _, path := range parent {
		if rec := serve("", path); rec.Code != 200 {
			t.Fatalf("public bound %d %s", rec.Code, rec.Body.String())
		}
	}
	for _, path := range direct {
		if rec := serve("", path); rec.Code != 404 {
			t.Fatalf("unbound public %d", rec.Code)
		}
	}
	wrapper := &inlineHookStore{Store: s.store}
	wrapper.after = func(n int) {
		if n == 1 {
			if _, err := sessions.RevokeByUser(ctx, "1001", "inline-revoke", time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.store = wrapper
	if rec := serve(tokens["1001"], parent[0]); rec.Code != http.StatusUnauthorized || bytes.Contains(rec.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatalf("revoked stream %d %s", rec.Code, rec.Body.String())
	}
	for _, path := range append(direct, "/content/me", "/richtext/me") {
		rec := serve(tokens["1001"], path)
		if rec.Code != 401 || strings.Contains(rec.Body.String(), a.FileName) {
			t.Fatalf("revoked %d %s", rec.Code, rec.Body.String())
		}
	}
}
