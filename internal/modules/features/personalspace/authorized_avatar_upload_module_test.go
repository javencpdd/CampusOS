package space

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"testing"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestSpaceAvatarUploadModuleCurrentAndMissingOrInvalidAccountingClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	var nilAccount *corestorage.ObjectService
	for _, mode := range []string{"current", "missing", "invalid", "typed_nil", "custom_root"} {
		t.Run(mode, func(t *testing.T) {
			app := platformmodule.NewAppContext()
			adapter, err := corestorage.NewLocalAdapter(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			proof := &ownContentsModuleProof{}
			objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), 1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]any{portRepository: NewMemoryRepository(), "identity.user-reader": spaceOwnerModuleUserReader{}, "community.content-query": &authenticatedSpaceQuery{}, "storage.user": adapter, "storage.quota-manager": adapter, "appearance.application": &ownContentsModuleAppearance{}, "platform.event-bus": eventbus.NewMemoryEventBus(), "identity.session-verifier": proof, "identity.session-strength": proof, "platform.reliability.service": reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())} {
				if err := app.Provide(name, value); err != nil {
					t.Fatal(err)
				}
			}
			var account any = objects
			if mode == "invalid" {
				account = "incompatible"
			}
			if mode == "typed_nil" {
				account = nilAccount
			}
			if mode != "missing" {
				if err := app.Provide("storage.objects", account); err != nil {
					t.Fatal(err)
				}
			}
			root := adapter.Root()
			if mode == "custom_root" {
				root = t.TempDir()
			}
			module := NewModule(ModuleConfig{FileStorageConfig: func() FileStorageConfig { return FileStorageConfig{RootDir: root} }})
			if err := module.Register(app); err != nil {
				t.Fatal(err)
			}
			err = module.Start(ctx)
			if mode == "invalid" || mode == "typed_nil" {
				if err == nil {
					t.Fatal("bad accounting accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "avatar-module-test", Issuer: "avatar-module", AccessTTL: time.Hour})
			token, err := jwt.GenerateAccessToken("98401", "owner", auth.AccessTokenContext{SessionID: "session-98401", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			var body bytes.Buffer
			w := multipart.NewWriter(&body)
			part, err := w.CreateFormFile("file", "avatar.png")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(tinyPNG); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.POST("/avatar", middleware.JWTAuth(jwt, proof), module.Handler().UploadAvatar)
			req := httptest.NewRequest("POST", "/avatar", &body)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", w.FormDataContentType())
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			want := 200
			if mode == "missing" || mode == "custom_root" {
				want = 503
			}
			if rec.Code != want {
				t.Fatal(mode, want, rec.Code, rec.Body.String())
			}
		})
	}
}
