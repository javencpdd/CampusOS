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

func TestAssetAdminLifecycleHTTPRealDomainSessionStrictDTOAndSafeAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, actor := range []string{"1001", "adm.opaque.asset"} {
		for _, op := range []string{"quarantine", "restore"} {
			t.Run(actor+"/"+op, func(t *testing.T) {
				g, a := assetAdminFixture(t, actor, op)
				ctx := context.Background()
				token, err := g.jwt.GenerateAccessToken(actor, "Admin", auth.AccessTokenContext{SessionID: g.claims.SessionID, AuthVersion: 1})
				if err != nil {
					t.Fatal(err)
				}
				serve := func(token, body string, proof, loose bool) *httptest.ResponseRecorder {
					h := NewHandler(g.s)
					if proof {
						h.SetAdminSessionAuthority(g.current)
					}
					handler := h.AdminQuarantineUserAsset
					if op == "restore" {
						handler = h.AdminRestoreQuarantinedUserAsset
					}
					router := gin.New()
					gate := middleware.RequireAdminDomainAccess(g.jwt, portadapt.NewAdminDomainSessionVerifier(g.current), nil)
					if loose {
						gate = func(c *gin.Context) {
							c.Set("user_id", actor)
							c.Set("admin_account_id", actor)
							c.Set("session_id", "fake-session")
							c.Set("auth_version", int64(1))
							c.Next()
						}
					}
					router.POST("/write/:id", gate, handler)
					req := httptest.NewRequest("POST", "/write/"+a.AssetID+"?actor_id=other&scope=admin", strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Authorization", "Bearer "+token)
					req.Header.Set("X-Actor-ID", "other")
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
				body := `{"reason":"actual signed admin"}`
				userJWT := auth.NewJWTManager(auth.JWTConfig{Secret: "asset-admin-user-test", Issuer: "user-test", Audience: "user", AccessTTL: time.Hour})
				user, err := userJWT.GenerateAccessToken("1001", "Owner", auth.AccessTokenContext{SessionID: "user-session", AuthVersion: 1})
				if err != nil {
					t.Fatal(err)
				}
				check(serve(user, body, true, false), 401)
				check(serve("", body, true, true), 401)
				check(serve(token, body, false, false), 401)
				reader := g.s.adminExecution
				g.s.SetAdminExecutionAuthority(nil)
				check(serve(token, body, true, false), 503)
				g.s.SetAdminExecutionAuthority(reader)
				if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "test"); err != nil {
					t.Fatal(err)
				}
				check(serve(token, body, true, false), 403)
				if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"personal_space.asset.manage"}, "test"); err != nil {
					t.Fatal(err)
				}
				check(serve(token, `{"reason":"forge","actor_id":"1001"}`, true, false), 400)
				check(serve(token, `{"reason":"confirm","confirm_asset_id":"anything"}`, true, false), 400)
				rec := serve(token, body, true, false)
				check(rec, 200)
				var envelope struct {
					Data map[string]json.RawMessage `json:"data"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || len(envelope.Data) != 2 || envelope.Data["asset_id"] == nil || envelope.Data["status"] == nil {
					t.Fatal("unsafe asset ack", rec.Body.String(), err)
				}
				row, err := g.sessions.GetSessionByID(ctx, g.claims.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok, err := g.sessions.RevokeSessionCAS(ctx, row.ID, row.Version, "test", time.Now()); err != nil || !ok {
					t.Fatal(err, ok)
				}
				check(serve(token, body, true, false), 401)
			})
		}
	}
}
