package community

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type ownerModuleSessionProof struct{}

func (ownerModuleSessionProof) VerifyAccess(_ context.Context, claims *auth.JWTClaims) error {
	if claims.UserID != "98331" || claims.SessionID != "session-98331" || claims.AuthVersion != 1 || claims.TokenType != auth.AccessTokenType {
		return errors.New("test session is invalid")
	}
	return nil
}

func (ownerModuleSessionProof) AuthenticationStrengthForSession(_ context.Context, sessionID string) (string, error) {
	if sessionID != "session-98331" {
		return "", errors.New("test session is invalid")
	}
	return "password", nil
}

func TestThreadOwnerModuleSessionProofWiring(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name       string
		verifier   any
		strength   any
		status     int
		startError string
	}{
		{"bound", ownerModuleSessionProof{}, ownerModuleSessionProof{}, 200, ""},
		{"missing-both", nil, nil, 401, ""},
		{"missing-verifier", nil, ownerModuleSessionProof{}, 401, ""},
		{"missing-strength", ownerModuleSessionProof{}, nil, 401, ""},
		{"invalid-verifier", true, ownerModuleSessionProof{}, 0, "owner session verifier"},
		{"invalid-strength", ownerModuleSessionProof{}, true, 0, "owner session strength"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := platformmodule.NewAppContext()
			if err := BindMemoryAdapters(app); err != nil {
				t.Fatal(err)
			}
			features := platformfeature.NewRegistry(nil)
			if err := features.Register(platformfeature.Definition{ID: "controlled-richtext-article", Mode: platformfeature.Restart, DefaultEnabled: true}); err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]any{
				portEventBus: eventbus.NewMemoryEventBus(), portMemoryEventBus: eventbus.NewMemoryEventBus(),
				portCache: cache.NewMemoryCache(), "platform.feature-registry": features,
				"identity.session-verifier": test.verifier, "identity.session-strength": test.strength,
			} {
				if value != nil {
					if err := app.Provide(name, value); err != nil {
						t.Fatal(err)
					}
				}
			}
			module := NewModule()
			if err := module.Register(app); err != nil {
				t.Fatal(err)
			}
			err := module.Start(context.Background())
			if test.startError != "" {
				if err == nil || !strings.Contains(err.Error(), test.startError) {
					t.Fatalf("Start failed to reject incompatible port: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			thread := &domain.Thread{ID: "98341", AuthorID: "98331", CategoryID: "98351", Title: "title", Content: "module-private-body",
				PublicationStatus: domain.PublicationStatusPrivate, ModerationStatus: domain.ModerationStatusClear, DeletionStatus: domain.DeletionStatusActive}
			if err := module.ThreadRepository().Create(context.Background(), thread); err != nil {
				t.Fatal(err)
			}
			jwtManager := auth.NewJWTManager(auth.JWTConfig{Secret: "owner-module-test-secret", AccessTTL: time.Hour, Issuer: "owner-module-test"})
			token, err := jwtManager.GenerateAccessToken("98331", "owner", auth.AccessTokenContext{SessionID: "session-98331", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.GET("/threads/:id/me", middleware.JWTAuth(jwtManager, ownerModuleSessionProof{}), module.Handlers().Thread.GetThreadForCurrentUser)
			req := httptest.NewRequest(http.MethodGet, "/threads/98341/me", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != test.status {
				t.Fatalf("bound owner handler status=%d want %d: %s", rec.Code, test.status, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "facts_version") || (test.status != 200 && strings.Contains(rec.Body.String(), "module-private-body")) {
				t.Fatalf("module owner handler leaked private facts: %s", rec.Body.String())
			}
		})
	}
}
