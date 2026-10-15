package space

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestSpaceAvatarRetentionHTTPActualCurrentOwnerStrictBodyPendingAndRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	e := newOwnContentsHTTPEnvironment(t)
	s := NewService(e.spaces, e.users)
	adapter, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewLocalFileStoreWithStorage(FileStorageConfig{AvatarKeepLimit: 10}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFileStore(store)
	for i := 0; i < 5; i++ {
		if _, _, err := store.SaveAvatar(e.owner.ID, "legacy.png", bytes.NewReader(tinyPNG)); err != nil {
			t.Fatal(err)
		}
	}
	files, err := store.avatarFiles(e.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	space := defaultSpace(e.owner)
	space.ID = "990001"
	space.Avatar = store.AvatarURL(e.owner.ID, files[0].name)
	if err := e.spaces.Upsert(ctx, space); err != nil {
		t.Fatal(err)
	}
	store.cfg.AvatarKeepLimit = 3
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAvatarStorage(adapter, objects)
	audits := &avatarRetentionStoreHook{MemoryStore: reliability.NewMemoryStore(), failPhase: "finalize", failAt: "audit"}
	s.SetReliability(reliability.NewService(transaction.NewMemory(e.spaces), audits))
	admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "Admin", auth.AccessTokenContext{SessionID: "cleanup-admin", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token, body string, bound bool) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if bound {
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		r := gin.New()
		r.POST("/cleanup", middleware.JWTAuth(e.jwts.User, e.identity), h.CleanupAvatarHistory)
		req := httptest.NewRequest("POST", "/cleanup?owner_id="+e.other.ID+"&scope=admin", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Owner-ID", e.other.ID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	check := func(token, body string, bound bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		rec := serve(token, body, bound)
		if rec.Code != want {
			t.Fatal(want, rec.Code, rec.Body.String())
		}
		return rec
	}
	check(e.tokens.AccessToken, "", false, 401)
	check(admin, "", true, 401)
	for _, body := range []string{`{}`, `{"owner_id":"1002"}`, `{"operation_id":"raw"}`, `{"file_name":"old.png"}`} {
		check(e.tokens.AccessToken, body, true, 400)
	}
	rec := check(e.tokens.AccessToken, "", true, 200)
	var payload struct{ Data map[string]json.RawMessage }
	if json.Unmarshal(rec.Body.Bytes(), &payload) != nil || len(payload.Data) != 2 || string(payload.Data["cleanup_pending"]) != "true" {
		t.Fatal("pending missing", rec.Body.String())
	}
	for _, private := range []string{adapter.Root(), files[3].name, "sha256", "receipt", "identity"} {
		if strings.Contains(rec.Body.String(), private) {
			t.Fatal("private receipt leaked", private)
		}
	}
	audits.failPhase, audits.failAt = "", ""
	rec = check(e.tokens.AccessToken, "", true, 200)
	if json.Unmarshal(rec.Body.Bytes(), &payload) != nil || len(payload.Data) != 2 || string(payload.Data["cleanup_pending"]) != "false" {
		t.Fatal("retry pending not cleared", rec.Body.String())
	}
	if len(avatarUploadFileBytes(t, s, e.owner.ID)) != 3 {
		t.Fatal("history not trimmed")
	}
	if err := e.identity.RevokeCurrent(ctx, e.owner.ID, e.tokens.Session.ID); err != nil {
		t.Fatal(err)
	}
	check(e.tokens.AccessToken, "", true, 401)
}
