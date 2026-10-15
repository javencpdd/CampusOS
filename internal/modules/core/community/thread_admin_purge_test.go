package community

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
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

func TestThreadAdminPurgeModuleCurrentAuthorityWiring(t *testing.T) {
	for _, mode := range []string{"bound", "missing_authority", "missing_session"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			app := platformmodule.NewAppContext()
			if err := BindMemoryAdapters(app); err != nil {
				t.Fatal(err)
			}
			features := platformfeature.NewRegistry(nil)
			if err := features.Register(platformfeature.Definition{ID: "controlled-richtext-article", Mode: platformfeature.Restart, DefaultEnabled: true}); err != nil {
				t.Fatal(err)
			}
			admins := identityrepo.NewMemoryAdminDomainRepository()
			sessions := identityrepo.NewMemoryAdminSessionRepository()
			const actor = "98332"
			if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: actor, Username: "module-flags", Email: "module-flags@example.test"}); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			at := now.Add(-time.Minute)
			if err := sessions.CreateSession(ctx, identityrepo.AdminSession{ID: "adms.module.flags", AccountID: actor, RefreshTokenDigest: strings.Repeat("b", 64), TokenFamilyID: "adms.module.flags", AuthenticationStrength: "mfa", MFAAuthenticatedAt: &at, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "admin-flags-module-test-key", Issuer: "admin-flags-module", Audience: "admin", AccessTTL: time.Hour})
			admin := identityservice.NewAdminSessionService(admins, sessions, jwt)
			if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.module.flags", AccountID: actor, RoleID: "security_admin", GrantedBy: actor}); err != nil {
				t.Fatal(err)
			}
			if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.purge"}, "module-test"); err != nil {
				t.Fatal(err)
			}
			ports := map[string]any{portEventBus: eventbus.NewMemoryEventBus(), portMemoryEventBus: eventbus.NewMemoryEventBus(), portCache: cache.NewMemoryCache(), "platform.feature-registry": features, "identity.session-verifier": ownerModuleSessionProof{}, "identity.session-strength": ownerModuleSessionProof{}, "platform.reliability.service": reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())}
			if mode != "missing_authority" {
				ports["identity.admin-execution-authority"] = identityservice.NewBoardGovernanceAuthorityService(nil, admins, admin)
			}
			if mode != "missing_session" {
				ports["identity.admin-session-authority"] = admin
			}
			for name, value := range ports {
				if err := app.Provide(name, value); err != nil {
					t.Fatal(err)
				}
			}
			m := NewModule()
			if err := m.Register(app); err != nil {
				t.Fatal(err)
			}
			if err := m.Start(ctx); err != nil {
				t.Fatal(err)
			}
			row := &domain.Thread{ID: "98341", AuthorID: actor, CategoryID: "98351", Title: "private module flag title", Content: "private module flag body", PublicationStatus: "private", ModerationStatus: "taken_down", DeletionStatus: "trashed"}
			if err := m.ThreadRepository().Create(ctx, row); err != nil {
				t.Fatal(err)
			}
			token, err := jwt.GenerateAccessToken(actor, "Admin", auth.AccessTokenContext{SessionID: "adms.module.flags", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.DELETE("/admin/threads/:id/purge", middleware.RequireAdminDomainAccess(jwt, portadapt.NewAdminDomainSessionVerifier(admin), nil), m.Handlers().Thread.AdminPurge)
			req := httptest.NewRequest("DELETE", "/admin/threads/98341/purge", strings.NewReader(`{"reason":"module review"}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			want := 204
			if mode == "missing_authority" {
				want = 503
			}
			if mode == "missing_session" {
				want = 401
			}
			if rec.Code != want || strings.Contains(rec.Body.String(), row.Content) || strings.Contains(rec.Body.String(), row.Title) {
				t.Fatal(mode, rec.Code, rec.Body.String())
			}
			actual, getErr := m.ThreadRepository().GetByID(ctx, row.ID)
			if want == 204 {
				if getErr == nil || rec.Body.Len() != 0 {
					t.Fatal(actual, getErr)
				}
			} else if getErr != nil || actual.DeletedAt != nil {
				t.Fatal(actual, getErr)
			}
		})
	}
}
