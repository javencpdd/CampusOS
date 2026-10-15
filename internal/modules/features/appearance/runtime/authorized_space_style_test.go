package appearance

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	space "github.com/campusos/CampusOS/internal/modules/features/personalspace"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type styleUnusedContentQuery struct{ communityport.ContentQuery }
type styleLegacyOnlyTarget struct{ space.StyleApplication }

func TestSpaceStyleOwnerAppearanceActualModuleForwardingAndMissingProofClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	for _, mode := range []string{"current", "missing_proof", "legacy_target"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("RESOURCE_DIR", root)
			packFiles := appearanceVariantFiles()
			var zipBuffer bytes.Buffer
			zipWriter := zip.NewWriter(&zipBuffer)
			for name, data := range packFiles {
				path := filepath.Join(root, "space-style-packs", "variant-space", filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
				entry, err := zipWriter.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write([]byte(data)); err != nil {
					t.Fatal(err)
				}
			}
			if err := zipWriter.Close(); err != nil {
				t.Fatal(err)
			}
			users := identityrepo.NewMemoryUserRepository()
			user := &identitydomain.User{ID: "1001", Username: "style-owner", Nickname: "Style owner", Email: "style@example.test", Status: "active", AuthVersion: 1}
			if err := users.Create(ctx, user); err != nil {
				t.Fatal(err)
			}
			jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "style-module-actual", Issuer: "style-module", Audience: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour})
			identity, err := identityservice.NewSessionService(identityrepo.NewMemorySessionRepository(), users, jwt, identityservice.SessionConfig{IPHashSecret: "style-module-ip"})
			if err != nil {
				t.Fatal(err)
			}
			tokens, err := identity.Issue(ctx, user, identityservice.SessionMetadata{DeviceName: "Actual Appearance"})
			if err != nil {
				t.Fatal(err)
			}
			adapter, err := corestorage.NewLocalAdapter(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			repo := space.NewMemoryRepository()
			events := reliability.NewMemoryStore()
			reliable := reliability.NewService(transaction.NewMemory(), events)
			appearance := NewApplication(nil, nil, nil)
			appearance.SetReliability(reliable)
			app := platformmodule.NewAppContext()
			for name, value := range map[string]any{"feature.personal-space.adapter.repository": repo, "identity.user-reader": portadapt.NewRepositoryUserReader(users), "community.content-query": &styleUnusedContentQuery{}, "storage.user": adapter, "storage.quota-manager": adapter, "appearance.application": appearance, "platform.reliability.service": reliable, "platform.event-bus": eventbus.NewMemoryEventBus()} {
				if err := app.Provide(name, value); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "missing_proof" {
				for _, name := range []string{"identity.session-verifier", "identity.session-strength"} {
					if err := app.Provide(name, identity); err != nil {
						t.Fatal(err)
					}
				}
			}
			module := space.NewModule(space.ModuleConfig{FileStorageConfig: func() space.FileStorageConfig { return space.FileStorageConfig{RootDir: adapter.Root()} }})
			if err := module.Register(app); err != nil {
				t.Fatal(err)
			}
			if err := module.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if mode == "legacy_target" {
				appearance.AttachSpaceStyleTarget(&styleLegacyOnlyTarget{})
			}
			h := module.Handler()
			r := gin.New()
			g := r.Group("/style", middleware.JWTAuth(jwt, identity))
			g.POST("/apply", h.ApplyStylePackage)
			g.POST("/default", h.RestoreDefaultStyle)
			g.POST("/rollback", h.RollbackStyle)
			g.POST("/html", h.ApplyCustomHTML)
			g.POST("/zip", h.ApplyStylePackZip)
			g.POST("/source", h.ApplySourceStylePack)
			g.GET("/catalog", h.ListSourceStylePacks)
			catalogRequest := httptest.NewRequest("GET", "/style/catalog", nil)
			catalogRequest.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
			catalogResponse := httptest.NewRecorder()
			r.ServeHTTP(catalogResponse, catalogRequest)
			catalogStatus := 200
			if mode == "missing_proof" {
				catalogStatus = 401
			}
			if mode == "legacy_target" {
				catalogStatus = 503
			}
			if catalogResponse.Code != catalogStatus || strings.Contains(catalogResponse.Body.String(), root) {
				t.Fatal("actual Module/App catalog did not carry current proof", mode, catalogResponse.Code, catalogResponse.Body.String())
			}
			if mode == "current" {
				var catalog struct {
					Data struct{ Items []map[string]json.RawMessage }
				}
				if json.Unmarshal(catalogResponse.Body.Bytes(), &catalog) != nil || len(catalog.Data.Items) != 1 || catalog.Data.Items[0]["path"] != nil {
					t.Fatal("actual Module/App leaked catalog physical Path", catalogResponse.Body.String())
				}
			}
			pkg := space.StylePackage{Manifest: space.StyleManifest{SchemaVersion: space.StyleSchemaVersion, Name: "safe-style", Version: "1.0.0", Layout: "blog", Components: []space.StyleComponent{{Slot: "header", Type: "profile-header"}, {Slot: "main", Type: "content-list"}}}}
			if result := space.ValidateStylePackage(pkg); !result.Valid {
				t.Fatal("invalid actual module fixture", result)
			}
			raw, err := json.Marshal(pkg)
			if err != nil {
				t.Fatal(err)
			}
			for _, op := range []string{"apply", "default", "rollback", "html", "zip", "source"} {
				body := []byte(nil)
				contentType := "application/json"
				if op == "apply" {
					body = raw
				}
				if op == "html" {
					body = []byte(`{"html":"<section>Actual module HTML</section>"}`)
				}
				if op == "source" {
					body = []byte(`{"name":"variant-space"}`)
				}
				if op == "zip" {
					var buffer bytes.Buffer
					w := multipart.NewWriter(&buffer)
					part, err := w.CreateFormFile("file", "style.zip")
					if err != nil {
						t.Fatal(err)
					}
					if _, err := part.Write(zipBuffer.Bytes()); err != nil {
						t.Fatal(err)
					}
					if err := w.Close(); err != nil {
						t.Fatal(err)
					}
					body = buffer.Bytes()
					contentType = w.FormDataContentType()
				}
				req := httptest.NewRequest("POST", "/style/"+op, bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
				req.Header.Set("Content-Type", contentType)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				want := 200
				if mode == "missing_proof" {
					want = 401
				}
				if mode == "legacy_target" {
					want = 503
				}
				if rec.Code != want {
					t.Fatal(mode, op, want, rec.Code, rec.Body.String())
				}
				if mode == "current" {
					var ack struct{ Data map[string]json.RawMessage }
					if json.Unmarshal(rec.Body.Bytes(), &ack) != nil || len(ack.Data) != 2 || ack.Data["space_id"] == nil || ack.Data["owner_id"] == nil {
						t.Fatal("unsafe production Appearance ack", rec.Body.String())
					}
				}
			}
			operations, _, err := events.ListOperations(ctx, "", reliability.PageRequest{Page: 1, PageSize: 100})
			if err != nil || len(operations) != 0 {
				t.Fatal("checked path fell back to legacy TrackOperation", operations, err)
			}
			if mode == "current" {
				stored, err := repo.GetByUserID(ctx, "1001")
				if err != nil || stored.StyleName != "variant-space" {
					t.Fatal(stored, err)
				}
			}
		})
	}
}
