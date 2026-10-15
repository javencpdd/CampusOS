package richtext

import (
	"context"
	"encoding/json"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAssetAdminReadHTTPRealSessionBoundScopesNoBodyAndSafeProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, op := range []string{"summary", "preview"} {
		t.Run(op, func(t *testing.T) {
			g, a := assetAdminReadFixture(t, op)
			ctx := context.Background()
			token, err := g.jwt.GenerateAccessToken(g.actor, "Admin", auth.AccessTokenContext{SessionID: g.claims.SessionID, AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			serve := func(token, body string, proof bool) *httptest.ResponseRecorder {
				h := NewHandler(g.s)
				if proof {
					h.SetAdminSessionAuthority(g.current)
				}
				handler := h.AssetGovernanceSummary
				if op == "preview" {
					handler = h.AdminPreviewUserAssetPurge
				}
				router := gin.New()
				router.GET("/read/:id", middleware.RequireAdminDomainAccess(g.jwt, portadapt.NewAdminDomainSessionVerifier(g.current), nil), handler)
				req := httptest.NewRequest("GET", "/read/"+a.AssetID+"?actor_id=1001&permission=personal_space.asset.manage&scope=admin", strings.NewReader(body))
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
			userJWT := auth.NewJWTManager(auth.JWTConfig{Secret: "asset-admin-read-user-key", Issuer: "read-user", Audience: "user", AccessTTL: time.Hour})
			user, err := userJWT.GenerateAccessToken("1001", "Owner", auth.AccessTokenContext{SessionID: "user", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			check(serve(user, "", true), 401)
			check(serve(token, "", false), 401)
			reader := g.s.adminExecution
			g.s.SetAdminExecutionAuthority(nil)
			check(serve(token, "", true), 503)
			g.s.SetAdminExecutionAuthority(reader)
			if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "test"); err != nil {
				t.Fatal(err)
			}
			check(serve(token, "", true), 403)
			code := "personal_space.asset.manage"
			if op == "summary" {
				code = "personal_space.asset.read_audit"
			}
			if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{code}, "test"); err != nil {
				t.Fatal(err)
			}
			check(serve(token, `{"facts":{"eligible":true}}`, true), 400)
			rec := serve(token, "", true)
			check(rec, 200)
			var envelope struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			want := 4
			if op == "preview" {
				want = 6
			}
			if len(envelope.Data) != want || strings.Contains(rec.Body.String(), "original_name") || strings.Contains(rec.Body.String(), "storage_object_id") || strings.Contains(rec.Body.String(), "owner_user_id") {
				t.Fatal("private asset facts leaked", rec.Body.String())
			}
			row, _ := g.sessions.GetSessionByID(ctx, g.claims.SessionID)
			if _, ok, err := g.sessions.UpgradeSessionStrengthCAS(ctx, row.ID, row.Version, "password", time.Now(), time.Now()); err != nil || !ok {
				t.Fatal(err, ok)
			}
			passwordWant := 403
			if op == "summary" {
				passwordWant = 200
			}
			check(serve(token, "", true), passwordWant)
			row, _ = g.sessions.GetSessionByID(ctx, g.claims.SessionID)
			if _, ok, err := g.sessions.RevokeSessionCAS(ctx, row.ID, row.Version, "test", time.Now()); err != nil || !ok {
				t.Fatal(err, ok)
			}
			check(serve(token, "", true), 401)
		})
	}
}
