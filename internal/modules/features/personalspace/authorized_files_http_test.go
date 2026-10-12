package space

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
	"time"

	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestSpaceFilesHTTPCurrentProfileAndRealSession(t *testing.T) {
	e := newOwnContentsHTTPEnvironment(t)
	ctx := context.Background()
	s, current, old := fileFixture(t, e.spaces, e.users, e.owner.ID)
	profile := defaultSpace(e.owner)
	profile.ID = "101010"
	profile.Avatar = s.fileStore.AvatarURL(e.owner.ID, current)
	if err := e.spaces.Upsert(ctx, profile); err != nil {
		t.Fatal(err)
	}
	other, err := e.identity.Issue(ctx, e.other, identityservice.SessionMetadata{DeviceName: "Other file reader"})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "same-id-admin", auth.AccessTokenContext{SessionID: "files-admin-session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token, url string, bound bool) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if bound {
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		r := gin.New()
		r.GET("/avatars/:user_id/:filename", middleware.OptionalJWT(e.jwts.User, e.identity), h.ServeAvatarFile)
		g := r.Group("/me", middleware.JWTAuth(e.jwts.User, e.identity))
		g.GET("/avatars", h.ListAvatars)
		g.GET("/storage", h.GetStorageStatus)
		req := httptest.NewRequest("GET", url, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("X-Owner-ID", e.owner.ID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	public := "/avatars/" + e.owner.ID + "/" + current
	history := "/avatars/" + e.owner.ID + "/" + old + "?owner_id=" + e.owner.ID + "&authentication_strength=mfa"
	for _, tc := range []struct {
		token, path string
		status      int
	}{{"", public, 200}, {"", history, 404}, {other.AccessToken, history, 404}, {e.tokens.AccessToken, history, 200}, {admin, history, 401}} {
		rec := serve(tc.token, tc.path, true)
		if rec.Code != tc.status {
			t.Fatalf("%s code=%d expected=%d body=%s", tc.path, rec.Code, tc.status, rec.Body.String())
		}
	}
	for _, path := range []string{"/me/avatars?owner_id=" + e.other.ID, "/me/storage?owner_id=" + e.other.ID} {
		if rec := serve(e.tokens.AccessToken, path, true); rec.Code != 200 {
			t.Fatalf("owner %d %s", rec.Code, rec.Body.String())
		}
		if rec := serve(e.tokens.AccessToken, path, false); rec.Code != 401 {
			t.Fatalf("missing proof %d %s", rec.Code, rec.Body.String())
		}
	}
	profile.Visibility = VisibilityPrivate
	if err := e.spaces.Upsert(ctx, profile); err != nil {
		t.Fatal(err)
	}
	if rec := serve("", public, true); rec.Code != 404 {
		t.Fatalf("private profile %d", rec.Code)
	}
	if rec := serve(e.tokens.AccessToken, public, true); rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	hook := &filesRepoHook{Repository: e.spaces}
	hook.after = func(n int) {
		if n == 1 {
			if _, err := e.sessions.RevokeByUser(ctx, e.owner.ID, "files-test", time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.repo = hook
	if rec := serve(e.tokens.AccessToken, history, true); rec.Code != 401 || bytes.HasPrefix(rec.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatalf("revoked %d %s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{history, "/me/avatars", "/me/storage"} {
		if rec := serve(e.tokens.AccessToken, path, true); rec.Code != 401 {
			t.Fatalf("revoked endpoint %d", rec.Code)
		}
	}
}
