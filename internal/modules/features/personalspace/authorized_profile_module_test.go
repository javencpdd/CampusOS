package space

import (
	"bytes"
	"context"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
	"time"
)

type spaceOwnerModuleUserReader struct{ ownContentsModuleUserReader }

func (r spaceOwnerModuleUserReader) GetUser(ctx context.Context, id string) (identityport.User, error) {
	u, e := r.ownContentsModuleUserReader.GetUser(ctx, id)
	u.Status = "active"
	return u, e
}
func TestSpaceOwnerModuleActualCommandWiringAndInvalidAdaptersClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var nilCommand *reliability.Service
	for _, mode := range []string{"current", "missing", "invalid", "typed_nil"} {
		t.Run(mode, func(t *testing.T) {
			app := platformmodule.NewAppContext()
			adapter, err := corestorage.NewLocalAdapter(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			proof := &ownContentsModuleProof{}
			for name, value := range map[string]any{portRepository: NewMemoryRepository(), "identity.user-reader": spaceOwnerModuleUserReader{}, "community.content-query": &authenticatedSpaceQuery{}, "storage.user": adapter, "storage.quota-manager": adapter, "appearance.application": &ownContentsModuleAppearance{}, "platform.event-bus": eventbus.NewMemoryEventBus(), "identity.session-verifier": proof, "identity.session-strength": proof} {
				if err := app.Provide(name, value); err != nil {
					t.Fatal(err)
				}
			}
			var command any
			switch mode {
			case "current":
				command = reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
			case "invalid":
				command = "incompatible"
			case "typed_nil":
				command = nilCommand
			}
			if mode != "missing" {
				if err := app.Provide("platform.reliability.service", command); err != nil {
					t.Fatal(err)
				}
			}
			module := NewModule(ModuleConfig{FileStorageConfig: func() FileStorageConfig { return FileStorageConfig{RootDir: adapter.Root()} }})
			if err := module.Register(app); err != nil {
				t.Fatal(err)
			}
			err = module.Start(context.Background())
			if mode == "invalid" || mode == "typed_nil" {
				if err == nil {
					t.Fatal("invalid command adapter accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "profile-module-test", Issuer: "profile-module", AccessTTL: time.Hour})
			token, err := jwt.GenerateAccessToken("98401", "owner", auth.AccessTokenContext{SessionID: "session-98401", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.PUT("/me", middleware.JWTAuth(jwt, proof), module.Handler().UpdateMe)
			req := httptest.NewRequest("PUT", "/me", bytes.NewBufferString(`{"title":"Module current profile"}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			want := 200
			if mode == "missing" {
				want = 503
			}
			if rec.Code != want {
				t.Fatal(mode, rec.Code, rec.Body.String())
			}
		})
	}
}
