package space

import (
	"context"
	"encoding/json"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSpaceQuotaAdminHTTPRealCurrentDomainStrictInputAndMetadataOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"current", "password", "expired_mfa"} {
		for _, actor := range []string{"1001", "adm.quota.http"} {
			for _, op := range []string{"quota", "storage", "summary"} {
				t.Run(mode+"/"+actor+"/"+op, func(t *testing.T) {
					ctx := context.Background()
					f := spaceAdminFixture(t, true, actor, mode)
					adapter, err := corestorage.NewLocalAdapter(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					store, err := NewLocalFileStoreWithStorage(FileStorageConfig{}, adapter)
					if err != nil {
						t.Fatal(err)
					}
					f.s.SetFileStore(store)
					f.s.SetQuotaManager(adapter)
					serve := func(token, body string, proof, loose bool) *httptest.ResponseRecorder {
						h := NewHandler(f.s)
						if proof {
							h.SetAdminSessionAuthority(f.current)
						}
						r := gin.New()
						gate := middleware.RequireAdminDomainAccess(f.jwt, portadapt.NewAdminDomainSessionVerifier(f.current), nil)
						if loose {
							gate = func(c *gin.Context) {
								c.Set("user_id", actor)
								c.Set("admin_account_id", actor)
								c.Set("session_id", "fake")
								c.Set("auth_version", int64(1))
								c.Next()
							}
						}
						method, path, pattern := "GET", "/spaces/1001/storage", "/spaces/:user_id/storage"
						handler := h.AdminStorageStatus
						if op == "quota" {
							method, handler = "PUT", h.SetStorageQuota
						}
						if op == "summary" {
							path, pattern, handler = "/summary", "/summary", h.AdminSummary
						}
						r.Handle(method, pattern, gate, handler)
						req := httptest.NewRequest(method, path, strings.NewReader(body))
						req.Header.Set("Content-Type", "application/json")
						req.Header.Set("Authorization", "Bearer "+token)
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
					if op == "quota" {
						body = `{"quota_bytes":2097152}`
					}
					userJWT := auth.NewJWTManager(auth.JWTConfig{Secret: "quota-user-test", Issuer: "quota-user", Audience: "user", AccessTTL: time.Hour})
					token, err := userJWT.GenerateAccessToken("1001", "owner", auth.AccessTokenContext{SessionID: "user-session", AuthVersion: 1})
					if err != nil {
						t.Fatal(err)
					}
					check(serve(token, body, true, false), 401)
					check(serve("", body, true, true), 401)
					check(serve(f.token, body, false, false), 503)
					source := f.s.adminExecution
					f.s.adminExecution = nil
					check(serve(f.token, body, true, false), 503)
					f.s.adminExecution = source
					if err := f.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "revoke"); err != nil {
						t.Fatal(err)
					}
					check(serve(f.token, body, true, false), 403)
					if err := f.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"personal_space.space.manage"}, "restore"); err != nil {
						t.Fatal(err)
					}
					if op == "quota" {
						for _, bad := range []string{`{"quota_bytes":2097152,"actor_id":"1001"}`, `{"quota_bytes":0}`, `{"quota_bytes":107374182401}`, `{"quota_bytes":"2097152"}`, `{} {}`, `{"quota_bytes":2097152,"reason":"` + strings.Repeat("x", 9000) + `"}`} {
							check(serve(f.token, bad, true, false), 400)
						}
					} else {
						check(serve(f.token, `{}`, true, false), 400)
					}
					want := 200
					if op == "quota" && mode != "current" {
						want = 403
					}
					rec := serve(f.token, body, true, false)
					check(rec, want)
					if want == 200 {
						if rec.Header().Get("Cache-Control") != "private, no-store" || strings.Contains(rec.Body.String(), "/host/private") || strings.Contains(rec.Body.String(), "space-owner") {
							t.Fatal(rec.Body.String())
						}
						var result struct {
							Data map[string]json.RawMessage `json:"data"`
						}
						if json.Unmarshal(rec.Body.Bytes(), &result) != nil {
							t.Fatal(rec.Body.String())
						}
						if op == "quota" && (len(result.Data) != 2 || result.Data["owner_id"] == nil || result.Data["quota_bytes"] == nil) {
							t.Fatal("unsafe quota ACK", rec.Body.String())
						}
						if op == "summary" && (result.Data["total_spaces"] == nil || result.Data["items"] != nil || result.Data["space"] != nil) {
							t.Fatal("unsafe aggregate", rec.Body.String())
						}
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
}
