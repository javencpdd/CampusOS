package space

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

func TestSpaceAdminHTTPRealDomainSessionStrictInputAndSafeAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, actor := range []string{"1001", "adm.space.http"} {
		for _, op := range []string{"disable", "enable"} {
			t.Run(actor+"/"+op, func(t *testing.T) {
				f := spaceAdminFixture(t, true, actor, "")
				ctx := context.Background()
				if op == "enable" {
					if _, err := f.s.DisableSpaceForPrincipal(ctx, "1001", "review", f.proof); err != nil {
						t.Fatal(err)
					}
				}
				serve := func(token, body string, proof, loose bool) *httptest.ResponseRecorder {
					h := NewHandler(f.s)
					if proof {
						h.SetAdminSessionAuthority(f.current)
					}
					gate := middleware.RequireAdminDomainAccess(f.jwt, portadapt.NewAdminDomainSessionVerifier(f.current), nil)
					if loose {
						gate = func(c *gin.Context) {
							c.Set("user_id", actor)
							c.Set("admin_account_id", actor)
							c.Set("session_id", "fake-session")
							c.Set("auth_version", int64(1))
							c.Next()
						}
					}
					r := gin.New()
					handler := h.DisableSpace
					if op == "enable" {
						handler = h.EnableSpace
					}
					r.POST("/spaces/:user_id/write", gate, handler)
					req := httptest.NewRequest("POST", "/spaces/1001/write?actor_id=forged&scope=global", strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Authorization", "Bearer "+token)
					req.Header.Set("X-Actor-ID", "other")
					rec := httptest.NewRecorder()
					r.ServeHTTP(rec, req)
					return rec
				}
				check := func(rec *httptest.ResponseRecorder, want int) {
					t.Helper()
					if rec.Code != want {
						t.Fatal(rec.Code, want, rec.Body.String())
					}
				}
				body := ""
				if op == "disable" {
					body = `{"reason":"actual signed administrator"}`
				}
				userJWT := auth.NewJWTManager(auth.JWTConfig{Secret: "space-user-test", Issuer: "space-user", Audience: "user", AccessTTL: time.Hour})
				user, err := userJWT.GenerateAccessToken("1001", "Owner", auth.AccessTokenContext{SessionID: "user-session", AuthVersion: 1})
				if err != nil {
					t.Fatal(err)
				}
				check(serve(user, body, true, false), 401)
				check(serve("", body, true, true), 401)
				check(serve(f.token, body, false, false), 503)
				source := f.s.adminExecution
				f.s.SetAdminExecutionAuthority(nil)
				check(serve(f.token, body, true, false), 503)
				f.s.SetAdminExecutionAuthority(source)
				if err := f.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "revoke"); err != nil {
					t.Fatal(err)
				}
				check(serve(f.token, body, true, false), 403)
				if err := f.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"personal_space.space.manage"}, "restore"); err != nil {
					t.Fatal(err)
				}
				check(serve(f.token, `{"reason":"forged","actor_id":"1001"}`, true, false), 400)
				check(serve(f.token, `{} {}`, true, false), 400)
				if op == "disable" {
					check(serve(f.token, `{"reason":"`+strings.Repeat("x", 9000)+`"}`, true, false), 400)
				} else {
					check(serve(f.token, `{}`, true, false), 400)
				}
				rec := serve(f.token, body, true, false)
				check(rec, 200)
				var response struct {
					Data map[string]json.RawMessage `json:"data"`
				}
				if json.Unmarshal(rec.Body.Bytes(), &response) != nil || len(response.Data) != 3 || response.Data["owner_id"] == nil || response.Data["space_id"] == nil || response.Data["disabled"] == nil || rec.Header().Get("Cache-Control") != "private, no-store" {
					t.Fatal(rec.Body.String())
				}
				session, err := f.sessions.GetSessionByID(ctx, f.claims.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok, err := f.sessions.RevokeSessionCAS(ctx, session.ID, session.Version, "test", time.Now()); err != nil || !ok {
					t.Fatal(ok, err)
				}
				check(serve(f.token, body, true, false), 401)
			})
		}
	}
}
