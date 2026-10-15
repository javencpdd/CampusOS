package space

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"reflect"
	"testing"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestSpaceAvatarUploadHTTPActualCurrentSessionStrictMultipartSafeAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	e := newOwnContentsHTTPEnvironment(t)
	s := NewService(e.spaces, e.users)
	s.SetReliability(reliability.NewService(transaction.NewMemory(e.spaces), reliability.NewMemoryStore()))
	adapter, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewLocalFileStoreWithStorage(FileStorageConfig{}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFileStore(store)
	s.SetAvatarStorage(adapter, objects)
	admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "Admin", auth.AccessTokenContext{SessionID: "avatar-admin", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token string, bound bool, field string, count int) *httptest.ResponseRecorder {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		if field != "" {
			if err := w.WriteField(field, e.other.ID); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < count; i++ {
			part, err := w.CreateFormFile("file", "avatar.png")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(tinyPNG); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		h := NewHandler(s)
		if bound {
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		r := gin.New()
		r.POST("/avatar", middleware.JWTAuth(e.jwts.User, e.identity), h.UploadAvatar)
		req := httptest.NewRequest("POST", "/avatar?owner_id="+e.other.ID+"&scope=admin", &body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set("X-Owner-ID", e.other.ID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	check := func(token string, bound bool, field string, count, want int) *httptest.ResponseRecorder {
		t.Helper()
		rec := serve(token, bound, field, count)
		if rec.Code != want {
			t.Fatal(want, rec.Code, rec.Body.String())
		}
		return rec
	}
	before := e.spaces.Snapshot()
	check(e.tokens.AccessToken, false, "", 1, 401)
	check(admin, true, "", 1, 401)
	for _, field := range []string{"owner_id", "scope", "thread_id", "facts_version"} {
		check(e.tokens.AccessToken, true, field, 1, 400)
	}
	check(e.tokens.AccessToken, true, "", 2, 400)
	check(e.tokens.AccessToken, true, "", 0, 400)
	if !reflect.DeepEqual(before, e.spaces.Snapshot()) {
		t.Fatal("rejected request changed space")
	}
	rec := check(e.tokens.AccessToken, true, "", 1, 200)
	var payload struct{ Data map[string]json.RawMessage }
	if json.Unmarshal(rec.Body.Bytes(), &payload) != nil || len(payload.Data) != 2 || payload.Data["space_id"] == nil || payload.Data["owner_id"] == nil {
		t.Fatal("unsafe upload acknowledgement", rec.Body.String())
	}
	space, err := e.spaces.GetByUserID(ctx, e.owner.ID)
	if err != nil || space.Avatar == "" {
		t.Fatal(space, err)
	}
	if err := e.identity.RevokeCurrent(ctx, e.owner.ID, e.tokens.Session.ID); err != nil {
		t.Fatal(err)
	}
	check(e.tokens.AccessToken, true, "", 1, 401)
}
