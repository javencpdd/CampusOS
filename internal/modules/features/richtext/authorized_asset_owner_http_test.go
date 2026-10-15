package richtext

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestAssetOwnerLifecycleHTTPRealSessionNoBodyAndSafeAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, operation := range []string{"trash", "restore"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			s, a, _, _, _ := assetOwnerFixture(t, operation)
			users, sessions := identityrepo.NewMemoryUserRepository(), identityrepo.NewMemorySessionRepository()
			jwt := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "asset-owner-user-test", Issuer: "asset-owner-user", Audience: "user", KeyID: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour}, auth.JWTConfig{Secret: "asset-owner-admin-test", Issuer: "asset-owner-admin", Audience: "admin", KeyID: "admin", AccessTTL: time.Hour})
			identity, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "asset-owner-ip-test"})
			if err != nil {
				t.Fatal(err)
			}
			tokens := map[string]string{}
			for _, id := range []string{"1001", "1002"} {
				u := &identitydomain.User{ID: id, Username: "asset-owner-" + id, Email: "asset-owner-" + id + "@example.test", Status: "active", AuthVersion: 1}
				if err := users.Create(ctx, u); err != nil {
					t.Fatal(err)
				}
				issued, err := identity.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Asset owner"})
				if err != nil {
					t.Fatal(err)
				}
				tokens[id] = issued.AccessToken
			}
			serve := func(token string, proof bool, payload string) *httptest.ResponseRecorder {
				h := NewHandler(s)
				if proof {
					h.SetSessionProof(identity, identity)
				}
				handler := h.TrashUserAsset
				if operation == "restore" {
					handler = h.RestoreUserAsset
				}
				router := gin.New()
				router.POST("/write/:id", middleware.JWTAuth(jwt.User, identity), handler)
				req := httptest.NewRequest(http.MethodPost, "/write/"+a.AssetID+"?owner_id=1002&scope=admin", strings.NewReader(payload))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("X-Actor-ID", "1002")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				return rec
			}
			check := func(rec *httptest.ResponseRecorder, want int) {
				t.Helper()
				if rec.Code != want {
					t.Fatal(rec.Code, want, rec.Body.String())
				}
			}
			check(serve(tokens["1002"], true, ""), 403)
			check(serve(tokens["1001"], false, ""), 401)
			admin, err := jwt.Admin.GenerateAccessToken("1001", "Admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			check(serve(admin, true, ""), 401)
			check(serve(tokens["1001"], true, "{\"actor_id\":\"1002\"}"), 400)
			rec := serve(tokens["1001"], true, "")
			check(rec, 200)
			var envelope struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if json.Unmarshal(rec.Body.Bytes(), &envelope) != nil || len(envelope.Data) != 2 || envelope.Data["asset_id"] == nil || envelope.Data["status"] == nil {
				t.Fatal("private asset acknowledgement", rec.Body.String())
			}
			if _, err := sessions.RevokeByUser(ctx, "1001", "test", time.Now()); err != nil {
				t.Fatal(err)
			}
			check(serve(tokens["1001"], true, ""), 401)
		})
	}
}
