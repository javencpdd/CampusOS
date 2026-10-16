package community

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestThreadGovernanceModuleAuthorityWiring(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"bound", "missing", "invalid", "invalid_admin_execution"} {
		t.Run(mode, func(t *testing.T) {
			app := platformmodule.NewAppContext()
			if err := BindMemoryAdapters(app); err != nil {
				t.Fatal(err)
			}
			features := platformfeature.NewRegistry(nil)
			if err := features.Register(platformfeature.Definition{ID: "controlled-richtext-article", Mode: platformfeature.Restart, DefaultEnabled: true}); err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]any{portEventBus: eventbus.NewMemoryEventBus(), portMemoryEventBus: eventbus.NewMemoryEventBus(), portCache: cache.NewMemoryCache(), "platform.feature-registry": features, "identity.session-verifier": ownerModuleSessionProof{}, "identity.session-strength": ownerModuleSessionProof{}, "platform.reliability.service": reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())} {
				if err := app.Provide(name, value); err != nil {
					t.Fatal(err)
				}
			}
			grants := identityrepo.NewMemoryDelegationRepository()
			now := time.Now()
			if err := grants.InsertDelegations(context.Background(), []identityrepo.Delegation{{ID: "grant.module", Kind: "grant", SubjectKind: "user", SubjectID: "98331", Action: "community.thread.take_down", BoardID: "98351", NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
				t.Fatal(err)
			}
			if mode != "missing" {
				var reader any = identityservice.NewBoardGovernanceAuthorityService(grants, nil, nil)
				if mode == "invalid" {
					reader = true
				}
				if err := app.Provide("identity.board-governance-authority", reader); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "invalid_admin_execution" {
				if err := app.Provide("identity.admin-execution-authority", true); err != nil {
					t.Fatal(err)
				}
			}
			m := NewModule()
			if err := m.Register(app); err != nil {
				t.Fatal(err)
			}
			err := m.Start(context.Background())
			if mode == "invalid_admin_execution" {
				if err == nil || !strings.Contains(err.Error(), "admin execution authority") {
					t.Fatal(err)
				}
				return
			}
			if mode == "invalid" {
				if err == nil || !strings.Contains(err.Error(), "governance authority") {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			row := &domain.Thread{ID: "98341", AuthorID: "98332", CategoryID: "98351", Title: "private module title", Content: "private module body", PublicationStatus: "private", ModerationStatus: "clear", DeletionStatus: "active"}
			if err := m.ThreadRepository().Create(context.Background(), row); err != nil {
				t.Fatal(err)
			}
			jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "governance-module-test-key", Issuer: "governance-module", Audience: "user", AccessTTL: time.Hour})
			token, err := jwt.GenerateAccessToken("98331", "Moderator", auth.AccessTokenContext{SessionID: "session-98331", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.POST("/threads/:id/take-down", middleware.JWTAuth(jwt, ownerModuleSessionProof{}), m.Handlers().Thread.TakeDown)
			req := httptest.NewRequest("POST", "/threads/98341/take-down", strings.NewReader(`{"reason":"module wiring"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			expected := 200
			if mode == "missing" {
				expected = 503
			}
			if rec.Code != expected {
				t.Fatal(mode, rec.Code, rec.Body.String())
			}
		})
	}
}
