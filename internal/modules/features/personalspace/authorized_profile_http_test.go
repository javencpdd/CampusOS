package space

import (
	"bytes"
	"context"
	"encoding/json"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"reflect"
	"testing"
)

func runSpaceOwnerHTTPCurrentSession(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	e := newOwnContentsHTTPEnvironment(t)
	s := NewService(e.spaces, e.users)
	s.SetReliability(reliability.NewService(transaction.NewMemory(e.spaces), reliability.NewMemoryStore()))
	other, err := e.identity.Issue(ctx, e.other, identityservice.SessionMetadata{DeviceName: "Other space owner"})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "Admin", auth.AccessTokenContext{SessionID: "admin-space-session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token, method, body string, bound bool) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if bound {
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		r := gin.New()
		g := r.Group("/me", middleware.JWTAuth(e.jwts.User, e.identity))
		g.PUT("", h.UpdateMe)
		g.GET("", h.GetMe)
		req := httptest.NewRequest(method, "/me?owner_id="+e.other.ID+"&scope=admin", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Owner-ID", e.other.ID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	check := func(rec *httptest.ResponseRecorder, want int) {
		t.Helper()
		if rec.Code != want {
			t.Fatal(want, rec.Code, rec.Body.String())
		}
	}
	check(serve(e.tokens.AccessToken, "GET", "", false), 401)
	check(serve(e.tokens.AccessToken, "GET", `{}`, true), 400)
	check(serve(e.tokens.AccessToken, "PUT", `{"title":"Changed"}`, false), 401)
	check(serve(admin, "PUT", `{"title":"Changed"}`, true), 401)
	for _, body := range []string{`{"title":"Changed","owner_id":"1002"}`, `{"disabled_at":"2026-01-01"}`, `{"facts_version":100}`, `{"visibility":"unknown"}`} {
		check(serve(e.tokens.AccessToken, "PUT", body, true), 400)
	}
	before := e.spaces.Snapshot()
	check(serve(e.tokens.AccessToken, "GET", "", true), 200)
	if !reflectSpaceHTTPBefore(before, e.spaces.Snapshot()) {
		t.Fatal("default read created space")
	}
	rec := serve(e.tokens.AccessToken, "PUT", `{"title":" Owner Lab ","visibility":"private"}`, true)
	check(rec, 200)
	var ack struct{ Data map[string]json.RawMessage }
	if json.Unmarshal(rec.Body.Bytes(), &ack) != nil || len(ack.Data) != 2 || ack.Data["space_id"] == nil || ack.Data["owner_id"] == nil {
		t.Fatal("unsafe command acknowledgement", rec.Body.String())
	}
	stored, err := e.spaces.GetByUserID(ctx, e.owner.ID)
	if err != nil || stored.Title != "Owner Lab" || stored.Visibility != VisibilityPrivate {
		t.Fatal(stored, err)
	}
	check(serve(other.AccessToken, "PUT", `{"title":"Other Lab"}`, true), 200)
	otherSpace, err := e.spaces.GetByUserID(ctx, e.other.ID)
	if err != nil || otherSpace.Title != "Other Lab" {
		t.Fatal("other session did not own its own space", otherSpace, err)
	}
	rec = serve(e.tokens.AccessToken, "GET", "", true)
	check(rec, 200)
	var payload struct{ Data PublicSpace }
	if json.Unmarshal(rec.Body.Bytes(), &payload) != nil || payload.Data.Owner.ID != e.owner.ID || payload.Data.Space.Title != "Owner Lab" {
		t.Fatal("query/header spoof changed owner", rec.Body.String())
	}
	if err := e.identity.RevokeCurrent(ctx, e.owner.ID, e.tokens.Session.ID); err != nil {
		t.Fatal(err)
	}
	check(serve(e.tokens.AccessToken, "GET", "", true), 401)
	check(serve(e.tokens.AccessToken, "PUT", `{"title":"Revoked"}`, true), 401)
}
func reflectSpaceHTTPBefore(a, b any) bool {
	return reflect.DeepEqual(a, b)
}
