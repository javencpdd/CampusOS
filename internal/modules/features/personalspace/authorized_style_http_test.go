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

func runSpaceStyleOwnerHTTPCurrent(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	e := newOwnContentsHTTPEnvironment(t)
	s := NewService(e.spaces, e.users)
	s.SetReliability(reliability.NewService(transaction.NewMemory(e.spaces), reliability.NewMemoryStore()))
	admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "Admin", auth.AccessTokenContext{SessionID: "style-admin", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token, op, body string, bound bool) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if bound {
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		r := gin.New()
		g := r.Group("/style", middleware.JWTAuth(e.jwts.User, e.identity))
		g.POST("/apply", h.ApplyStylePackage)
		g.POST("/default", h.RestoreDefaultStyle)
		g.POST("/rollback", h.RollbackStyle)
		req := httptest.NewRequest("POST", "/style/"+op+"?owner_id="+e.other.ID+"&scope=admin", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Owner-ID", e.other.ID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	check := func(token, op, body string, bound bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		rec := serve(token, op, body, bound)
		if rec.Code != want {
			t.Fatal(want, rec.Code, rec.Body.String())
		}
		return rec
	}
	raw, err := json.Marshal(validStylePackage())
	if err != nil {
		t.Fatal(err)
	}
	before := e.spaces.Snapshot()
	check(e.tokens.AccessToken, "apply", string(raw), false, 401)
	check(admin, "apply", string(raw), true, 401)
	check(e.tokens.AccessToken, "rollback", "", true, 404)
	for _, body := range []string{`{"manifest":{},"owner_id":"1002"}`, `{"manifest":{},"operation":"style_default"}`, `{"manifest":{"actor_id":"1002"}}`} {
		check(e.tokens.AccessToken, "apply", body, true, 400)
	}
	check(e.tokens.AccessToken, "default", `{}`, true, 400)
	check(e.tokens.AccessToken, "rollback", `{"snapshot_id":"3001"}`, true, 400)
	if !reflect.DeepEqual(before, e.spaces.Snapshot()) {
		t.Fatal("rejected style changed metadata")
	}
	for _, op := range []string{"apply", "default", "rollback"} {
		body := ""
		if op == "apply" {
			body = string(raw)
		}
		rec := check(e.tokens.AccessToken, op, body, true, 200)
		var ack struct{ Data map[string]json.RawMessage }
		if json.Unmarshal(rec.Body.Bytes(), &ack) != nil || len(ack.Data) != 2 || ack.Data["space_id"] == nil || ack.Data["owner_id"] == nil {
			t.Fatal("unsafe style ack", rec.Body.String())
		}
	}
	stored, err := e.spaces.GetByUserID(ctx, e.owner.ID)
	if err != nil || stored.StyleName != validStylePackage().Manifest.Name {
		t.Fatal("rollback did not restore applied style", stored, err)
	}
	if _, err := e.spaces.GetByUserID(ctx, e.other.ID); err == nil {
		t.Fatal("query/header selected other owner")
	}
	if err := e.identity.RevokeCurrent(ctx, e.owner.ID, e.tokens.Session.ID); err != nil {
		t.Fatal(err)
	}
	check(e.tokens.AccessToken, "default", "", true, 401)
}
func TestSpaceStyleOwnerHTTPRealCurrentSessionStrictDTOAndSafeAck(t *testing.T) {
	runSpaceStyleOwnerHTTPCurrent(t)
}
