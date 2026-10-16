package space

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type ownContentsModuleUserReader struct{}

func (ownContentsModuleUserReader) GetUser(_ context.Context, id string) (identityport.User, error) {
	if id != "98401" {
		return identityport.User{}, identityport.ErrUserNotFound
	}
	return identityport.User{ID: id, Username: "owner", Nickname: "Owner", Status: "active"}, nil
}
func (r ownContentsModuleUserReader) GetUserByUsername(ctx context.Context, username string) (identityport.User, error) {
	if username != "owner" {
		return identityport.User{}, identityport.ErrUserNotFound
	}
	return r.GetUser(ctx, "98401")
}

type ownContentsModuleProof struct{}

func (*ownContentsModuleProof) VerifyAccess(_ context.Context, claims *auth.JWTClaims) error {
	if claims == nil || claims.UserID != "98401" || claims.SessionID != "session-98401" || claims.AuthVersion != 1 || claims.TokenType != auth.AccessTokenType {
		return errors.New("invalid module test session")
	}
	return nil
}
func (*ownContentsModuleProof) AuthenticationStrengthForSession(_ context.Context, id string) (string, error) {
	if id != "session-98401" {
		return "", errors.New("invalid module test session")
	}
	return "password", nil
}

type ownContentsModuleAppearance struct{ StyleApplication }

func (*ownContentsModuleAppearance) AttachSpaceStyleTarget(StyleApplication) {}

func TestOwnContentsModuleSessionProofWiring(t *testing.T) {
	gin.SetMode(gin.TestMode)
	proof := &ownContentsModuleProof{}
	var nilProof *ownContentsModuleProof
	for _, test := range []struct {
		name       string
		verifier   any
		strength   any
		status     int
		startError string
	}{
		{"bound", proof, proof, 200, ""}, {"missing-both", nil, nil, 401, ""},
		{"missing-verifier", nil, proof, 401, ""}, {"missing-strength", proof, nil, 401, ""},
		{"invalid-verifier", true, proof, 0, "owner session verifier"}, {"invalid-strength", proof, true, 0, "owner session strength"},
		{"typed-nil-verifier", nilProof, proof, 0, "owner session verifier"}, {"typed-nil-strength", proof, nilProof, 0, "owner session strength"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := platformmodule.NewAppContext()
			storage, err := corestorage.NewLocalAdapter(filepath.Join(t.TempDir(), "storage"))
			if err != nil {
				t.Fatal(err)
			}
			query := &authenticatedSpaceQuery{capturingContentQuery: capturingContentQuery{authorThreads: []*communitydomain.Thread{{ID: "98411", AuthorID: "98401", Content: "module-owner-private-body", FactsVersion: 1, PublicationStatus: communitydomain.PublicationStatusPrivate, ModerationStatus: communitydomain.ModerationStatusClear, DeletionStatus: communitydomain.DeletionStatusActive}}}}
			for name, value := range map[string]any{
				portRepository: NewMemoryRepository(), "identity.user-reader": ownContentsModuleUserReader{},
				"community.content-query": query, "storage.user": storage, "storage.quota-manager": storage,
				"appearance.application": &ownContentsModuleAppearance{}, "platform.event-bus": eventbus.NewMemoryEventBus(),
				"identity.session-verifier": test.verifier, "identity.session-strength": test.strength,
			} {
				if value != nil {
					if err := app.Provide(name, value); err != nil {
						t.Fatal(err)
					}
				}
			}
			root := filepath.Join(t.TempDir(), "space-files")
			module := NewModule(ModuleConfig{FileStorageConfig: func() FileStorageConfig { return FileStorageConfig{RootDir: root} }})
			if err := module.Register(app); err != nil {
				t.Fatal(err)
			}
			err = module.Start(context.Background())
			if test.startError != "" {
				if err == nil || !strings.Contains(err.Error(), test.startError) {
					t.Fatalf("incompatible port accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "space-owner-module-test-secret", Issuer: "space-owner-module", AccessTTL: time.Hour})
			token, err := jwt.GenerateAccessToken("98401", "owner", auth.AccessTokenContext{SessionID: "session-98401", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.GET("/spaces/me/contents", middleware.JWTAuth(jwt, proof), module.Handler().ListOwnContents)
			req := httptest.NewRequest(http.MethodGet, "/spaces/me/contents", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != test.status {
				t.Fatalf("status=%d want=%d: %s", rec.Code, test.status, rec.Body.String())
			}
			if test.status != 200 && strings.Contains(rec.Body.String(), "module-owner-private-body") {
				t.Fatalf("unbound proof leaked data: %s", rec.Body.String())
			}
			if test.status == 200 && !strings.Contains(rec.Body.String(), "module-owner-private-body") {
				t.Fatalf("bound proof lost owner list: %s", rec.Body.String())
			}
			if test.name == "bound" {
				for _, read := range []struct {
					pattern, url string
					handler      gin.HandlerFunc
				}{
					{"/space/:user_id", "/space/98401", module.Handler().GetByUserID},
					{"/u/:username", "/u/owner", module.Handler().GetByUsername},
					{"/space/:user_id/contents", "/space/98401/contents", module.Handler().ListContentsByUserID},
					{"/u/:username/contents", "/u/owner/contents", module.Handler().ListContentsByUsername},
					{"/spaces/me/sync-status", "/spaces/me/sync-status", module.Handler().GetSyncStatus},
				} {
					router := gin.New()
					if strings.HasPrefix(read.url, "/spaces/me/") {
						router.Use(middleware.JWTAuth(jwt, proof))
					}
					router.GET(read.pattern, read.handler)
					request := httptest.NewRequest(http.MethodGet, read.url, nil)
					request.Header.Set("Authorization", "Bearer "+token)
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					if response.Code != 200 || response.Header().Get("Cache-Control") != "private, no-store" || strings.Contains(response.Body.String(), "module-owner-private-body") {
						t.Fatal("actual module current read wiring", read.url, response.Code, response.Body.String())
					}
				}
			}
		})
	}
}
