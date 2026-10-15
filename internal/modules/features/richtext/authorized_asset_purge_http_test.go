package richtext

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAssetPurgeHTTPActualDomainCurrentProofConfirmationAndSafeAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, actor := range []string{"1001", "adm.opaque.asset"} {
		t.Run(actor, func(t *testing.T) {
			g, a, _ := assetPurgeFixture(t, actor)
			ctx := context.Background()
			token, err := g.jwt.GenerateAccessToken(actor, "Admin", auth.AccessTokenContext{SessionID: g.claims.SessionID, AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			serve := func(token, body string, proof bool) *httptest.ResponseRecorder {
				h := NewHandler(g.s)
				if proof {
					h.SetAdminSessionAuthority(g.current)
				}
				router := gin.New()
				router.DELETE("/purge/:id", middleware.RequireAdminDomainAccess(g.jwt, portadapt.NewAdminDomainSessionVerifier(g.current), nil), h.AdminPurgeUserAsset)
				req := httptest.NewRequest("DELETE", "/purge/"+a.AssetID+"?actor_id=1001&scope=admin", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("X-Actor-ID", "1001")
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
			body := fmt.Sprintf(`{"reason":"confirmed","confirm_asset_id":%q}`, a.AssetID)
			userJWT := auth.NewJWTManager(auth.JWTConfig{Secret: "asset-purge-user-key", Issuer: "asset-purge-user", Audience: "user", AccessTTL: time.Hour})
			user, err := userJWT.GenerateAccessToken("1001", "Owner", auth.AccessTokenContext{SessionID: "user", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			check(serve(user, body, true), 401)
			check(serve(token, body, false), 401)
			check(serve(token, `{"reason":"missing confirmation"}`, true), 400)
			check(serve(token, `{"reason":"wrong","confirm_asset_id":"another"}`, true), 400)
			check(serve(token, fmt.Sprintf(`{"reason":"forge","confirm_asset_id":%q,"actor_id":"1001"}`, a.AssetID), true), 400)
			if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "test"); err != nil {
				t.Fatal(err)
			}
			check(serve(token, body, true), 403)
			if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"personal_space.asset.manage"}, "test"); err != nil {
				t.Fatal(err)
			}
			row, _ := g.sessions.GetSessionByID(ctx, g.claims.SessionID)
			if _, ok, err := g.sessions.UpgradeSessionStrengthCAS(ctx, row.ID, row.Version, "password", time.Now(), time.Now()); err != nil || !ok {
				t.Fatal(err, ok)
			}
			check(serve(token, body, true), 403)
			row, _ = g.sessions.GetSessionByID(ctx, g.claims.SessionID)
			if _, ok, err := g.sessions.UpgradeSessionStrengthCAS(ctx, row.ID, row.Version, "mfa", time.Now(), time.Now()); err != nil || !ok {
				t.Fatal(err, ok)
			}
			rec := serve(token, body, true)
			check(rec, 200)
			var envelope struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if json.Unmarshal(rec.Body.Bytes(), &envelope) != nil || len(envelope.Data) != 2 || envelope.Data["asset_id"] == nil || string(envelope.Data["status"]) != `"deleted"` {
				t.Fatal("private purge result leaked", rec.Body.String())
			}
			row, _ = g.sessions.GetSessionByID(ctx, g.claims.SessionID)
			if _, ok, err := g.sessions.RevokeSessionCAS(ctx, row.ID, row.Version, "test", time.Now()); err != nil || !ok {
				t.Fatal(err, ok)
			}
			check(serve(token, body, true), 401)
		})
	}
}
