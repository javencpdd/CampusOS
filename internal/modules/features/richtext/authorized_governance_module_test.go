package richtext

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestRichTextGovernanceModuleActualAdminAuthorityWiring(t *testing.T) {
	for _, mode := range []string{"bound", "missing_session", "missing_authority", "invalid_session"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			g := articleGovernanceFixture(t, "adm.opaque.article", "direct_restore")
			if mode == "missing_authority" {
				g.core.SetAdminExecutionAuthority(nil)
			}
			app := platformmodule.NewAppContext()
			storage, err := corestorage.NewLocalAdapter(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			objects, err := corestorage.NewObjectService(storage, storage, corestorage.NewMemoryObjectRepository(), 8*1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			ports := map[string]any{portStore: g.s.store, "community.content-gateway": g.s.community, "storage.user": storage, "storage.objects": objects, "platform.reliability.service": g.s.reliable}
			if mode != "missing_session" {
				ports["identity.admin-session-authority"] = g.current
			}
			if mode == "invalid_session" {
				ports["identity.admin-session-authority"] = struct{}{}
			}
			for name, value := range ports {
				if err := app.Provide(name, value); err != nil {
					t.Fatal(err)
				}
			}
			m := NewModule(ModuleConfig{AssetStoreConfig: func() AssetStoreConfig { return AssetStoreConfig{RootDir: t.TempDir()} }})
			if err := m.Register(app); err != nil {
				t.Fatal(err)
			}
			if err := m.Start(ctx); mode == "invalid_session" {
				if err == nil {
					t.Fatal("invalid Admin proof port accepted")
				}
				return
			} else if err != nil {
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
			router.POST("/api/v1/richtext/articles/:id/admin/restore", middleware.RequireAdminDomainAccess(g.jwt, portadapt.NewAdminDomainSessionVerifier(g.current), nil), m.Handler().AdminRestore)
			req := httptest.NewRequest("POST", "/api/v1/richtext/articles/"+g.id+"/admin/restore", strings.NewReader(`{"reason":"module restore"}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			want := 200
			if mode == "missing_session" {
				want = 401
			}
			if mode == "missing_authority" {
				want = 503
			}
			if rec.Code != want || strings.Contains(rec.Body.String(), "Author draft") || strings.Contains(rec.Body.String(), "<p>body</p>") {
				t.Fatal(rec.Code, rec.Body.String())
			}
			after, _ := g.s.store.GetArticleByThreadID(ctx, g.id)
			if (after.UpdatedActorKind == "admin") != (want == 200) {
				t.Fatal(after)
			}
		})
	}
}
