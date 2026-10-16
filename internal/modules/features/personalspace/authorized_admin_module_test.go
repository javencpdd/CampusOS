package space

import (
	"context"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpaceAdminActualModuleWiringAndMissingAdaptersClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"current", "missing_session", "missing_execution", "invalid_session", "invalid_execution", "legacy_user_reader"} {
		t.Run(mode, func(t *testing.T) {
			f := spaceAdminFixture(t, true, "adm.module.space", "")
			app := platformmodule.NewAppContext()
			storage, err := corestorage.NewLocalAdapter(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			values := map[string]any{portRepository: f.repo, "identity.user-reader": portadapt.NewRepositoryUserReader(f.users), "community.content-query": &authenticatedSpaceQuery{}, "storage.user": storage, "storage.quota-manager": storage, "appearance.application": &ownContentsModuleAppearance{}, "platform.event-bus": eventbus.NewMemoryEventBus(), "platform.reliability.service": f.s.reliable, "identity.admin-session-authority": f.current, "identity.admin-execution-authority": f.s.adminExecution}
			switch mode {
			case "missing_session":
				delete(values, "identity.admin-session-authority")
			case "missing_execution":
				delete(values, "identity.admin-execution-authority")
			case "invalid_session":
				values["identity.admin-session-authority"] = true
			case "invalid_execution":
				values["identity.admin-execution-authority"] = true
			case "legacy_user_reader":
				values["identity.user-reader"] = spaceOwnerModuleUserReader{}
			}
			for key, value := range values {
				if err := app.Provide(key, value); err != nil {
					t.Fatal(err)
				}
			}
			module := NewModule(ModuleConfig{FileStorageConfig: func() FileStorageConfig { return FileStorageConfig{RootDir: storage.Root()} }})
			if err := module.Register(app); err != nil {
				t.Fatal(err)
			}
			err = module.Start(context.Background())
			if strings.HasPrefix(mode, "invalid_") {
				if err == nil {
					t.Fatal("invalid adapter accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.POST("/spaces/:user_id/disable", middleware.RequireAdminDomainAccess(f.jwt, portadapt.NewAdminDomainSessionVerifier(f.current), nil), module.Handler().DisableSpace)
			req := httptest.NewRequest("POST", "/spaces/1001/disable", strings.NewReader(`{"reason":"module"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+f.token)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			want := 503
			if mode == "current" {
				want = 200
			}
			if mode == "legacy_user_reader" {
				want = 404
			}
			if rec.Code != want {
				t.Fatal(mode, rec.Code, rec.Body.String())
			}
			if mode == "current" {
				for _, step := range []struct {
					method, path, pattern, body string
					handler                     gin.HandlerFunc
				}{
					{"GET", "/summary", "/summary", "", module.Handler().AdminSummary},
					{"GET", "/spaces/1001/storage", "/spaces/:user_id/storage", "", module.Handler().AdminStorageStatus},
					{"PUT", "/spaces/1001/storage", "/spaces/:user_id/storage", `{"quota_bytes":2097152}`, module.Handler().SetStorageQuota},
				} {
					r.Handle(step.method, step.pattern, middleware.RequireAdminDomainAccess(f.jwt, portadapt.NewAdminDomainSessionVerifier(f.current), nil), step.handler)
					req := httptest.NewRequest(step.method, step.path, strings.NewReader(step.body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Authorization", "Bearer "+f.token)
					rec := httptest.NewRecorder()
					r.ServeHTTP(rec, req)
					if rec.Code != 200 {
						t.Fatal("actual Module", step.path, rec.Code, rec.Body.String())
					}
				}
			}
		})
	}
}
