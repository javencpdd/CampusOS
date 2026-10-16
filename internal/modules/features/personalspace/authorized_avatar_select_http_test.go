package space

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestSpaceAvatarSelectHTTPActualCurrentSessionStrictOwnerAndSafeAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	e := newOwnContentsHTTPEnvironment(t)
	s := NewService(e.spaces, e.users)
	s.SetReliability(reliability.NewService(transaction.NewMemory(e.spaces), reliability.NewMemoryStore()))
	name, _ := avatarSelectionFixture(t, s, e.owner.ID)
	admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "Admin", auth.AccessTokenContext{SessionID: "avatar-admin", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token, body string, bound bool) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if bound {
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		r := gin.New()
		r.PUT("/avatar", middleware.JWTAuth(e.jwts.User, e.identity), h.SelectAvatar)
		req := httptest.NewRequest("PUT", "/avatar?owner_id="+e.other.ID+"&scope=admin", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Owner-ID", e.other.ID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	valid, _ := json.Marshal(map[string]string{"file_name": name})
	check := func(token, body string, bound bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		rec := serve(token, body, bound)
		if rec.Code != want {
			t.Fatal(want, rec.Code, rec.Body.String())
		}
		return rec
	}
	before := e.spaces.Snapshot()
	check(e.tokens.AccessToken, string(valid), false, 401)
	check(admin, string(valid), true, 401)
	for _, body := range []string{`{"file_name":"../avatar.png"}`, `{"file_name":"missing.png","owner_id":"1002"}`, `{"file_name":"missing.png","facts_version":1}`, `{"file_name":"missing.png","operation":"profile"}`, `{}`} {
		check(e.tokens.AccessToken, body, true, 400)
	}
	check(e.tokens.AccessToken, `{"file_name":"missing.png"}`, true, 404)
	if !reflect.DeepEqual(before, e.spaces.Snapshot()) {
		t.Fatal("rejected HTTP changed space")
	}
	rec := check(e.tokens.AccessToken, string(valid), true, 200)
	var result struct{ Data map[string]json.RawMessage }
	if json.Unmarshal(rec.Body.Bytes(), &result) != nil || len(result.Data) != 2 || result.Data["owner_id"] == nil || result.Data["space_id"] == nil {
		t.Fatal("unsafe avatar ack", rec.Body.String())
	}
	space, err := e.spaces.GetByUserID(ctx, e.owner.ID)
	if err != nil || space.Avatar != s.fileStore.AvatarURL(e.owner.ID, name) {
		t.Fatal(space, err)
	}
	if err := e.identity.RevokeCurrent(ctx, e.owner.ID, e.tokens.Session.ID); err != nil {
		t.Fatal(err)
	}
	check(e.tokens.AccessToken, string(valid), true, 401)
}
