package richtext

import (
	"context"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetAdminLifecycleModuleCurrentExecutionAuthorityWiring(t *testing.T) {
	for _, mode := range []string{"bound", "missing_reader", "missing_session", "invalid_reader"} {
		t.Run(mode, func(t *testing.T) {
			g, a := assetAdminFixture(t, "adm.opaque.asset", "quarantine")
			ctx := context.Background()
			storage, err := corestorage.NewLocalAdapter(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			app := platformmodule.NewAppContext()
			ports := map[string]any{portStore: g.s.store, "community.content-gateway": g.s.community, "storage.user": storage, "storage.objects": g.s.objects, "platform.reliability.service": g.s.reliable}
			if mode != "missing_reader" {
				ports["identity.admin-execution-authority"] = g.s.adminExecution
			}
			if mode != "missing_session" {
				ports["identity.admin-session-authority"] = g.current
			}
			if mode == "invalid_reader" {
				ports["identity.admin-execution-authority"] = struct{}{}
			}
			for key, value := range ports {
				if err := app.Provide(key, value); err != nil {
					t.Fatal(err)
				}
			}
			m := NewModule(ModuleConfig{AssetStoreConfig: func() AssetStoreConfig { return AssetStoreConfig{RootDir: t.TempDir()} }})
			if err := m.Register(app); err != nil {
				t.Fatal(err)
			}
			err = m.Start(ctx)
			if mode == "invalid_reader" {
				if err == nil {
					t.Fatal("invalid execution reader accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := m.Stop(ctx); err != nil {
					t.Error(err)
				}
			})
			token, err := g.jwt.GenerateAccessToken(g.actor, "Admin", auth.AccessTokenContext{SessionID: g.claims.SessionID, AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.POST("/write/:id", middleware.RequireAdminDomainAccess(g.jwt, portadapt.NewAdminDomainSessionVerifier(g.current), nil), m.Handler().AdminQuarantineUserAsset)
			req := httptest.NewRequest("POST", "/write/"+a.AssetID, strings.NewReader(`{"reason":"production module"}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			want := 200
			if mode == "missing_reader" {
				want = 503
			}
			if mode == "missing_session" {
				want = 401
			}
			if rec.Code != want {
				t.Fatal(rec.Code, want, rec.Body.String())
			}
		})
	}
}
