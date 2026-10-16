package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestAdminThreadPreviewHTTPActualJWTCurrentPasswordOpaqueAndStrictBoundedIDs(t *testing.T) {
	for _, opaque := range []bool{false, true} {
		for _, mode := range []string{"current", "password", "expired_mfa", "no_code", "missing_source", "missing_session", "missing_governance", "user_token", "unknown_fields", "empty", "duplicate", "large", "unknown_id", "revoked"} {
			t.Run(mode+map[bool]string{true: "/opaque", false: "/numeric"}[opaque], func(t *testing.T) {
				ctx := context.Background()
				e := newEventMetadataHTTP(t, mode)
				if opaque {
					actor, sid := "adm.metadata.opaque", "adms.metadata.opaque"
					if err := e.admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: actor, Username: "opaque-metadata", Email: "opaque-metadata@example.test"}); err != nil {
						t.Fatal(err)
					}
					now := time.Now()
					at := now.Add(-time.Minute)
					strength := "mfa"
					if mode == "password" {
						strength = "password"
					}
					if mode == "expired_mfa" {
						at = now.Add(-time.Hour)
					}
					if err := e.sessions.CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: actor, RefreshTokenDigest: strings.Repeat("b", 64), TokenFamilyID: sid, AuthenticationStrength: strength, MFAAuthenticatedAt: &at, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
						t.Fatal(err)
					}
					if err := e.admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.opaque.metadata", AccountID: actor, RoleID: "security_admin", GrantedBy: actor}); err != nil {
						t.Fatal(err)
					}
					var err error
					e.token, err = e.owner.jwts.Admin.GenerateAccessToken(actor, "Admin", auth.AccessTokenContext{SessionID: sid, AuthVersion: 1})
					if err != nil {
						t.Fatal(err)
					}
					if mode == "revoked" {
						if _, _, err := e.sessions.RevokeSessionCAS(ctx, sid, 1, "test", time.Now()); err != nil {
							t.Fatal(err)
						}
					}
				} else if mode == "revoked" {
					if _, _, err := e.sessions.RevokeSessionCAS(ctx, "adms.events", 1, "test", time.Now()); err != nil {
						t.Fatal(err)
					}
				}
				codes := []string{"community.thread.read", "community.thread.pin", "community.thread.lock", "community.thread.review", "community.thread.trash", "community.thread.restore", "community.thread.direct_restore", "community.thread.purge", "community.thread.take_down"}
				if mode == "no_code" {
					codes = nil
				}
				if err := e.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", codes, "preview-http"); err != nil {
					t.Fatal(err)
				}
				s := service.NewThreadService(e.owner.repo, nil)
				s.SetGovernanceRepository(repository.NewMemoryContentGovernanceRepository())
				if mode != "missing_source" {
					s.SetAdminExecutionAuthority(e.authority)
				}
				if mode != "missing_governance" {
					s.SetGovernanceAuthority(e.authority.(identityport.BoardGovernanceAuthorityReader))
				}
				h := NewThreadHandler(s)
				if mode != "missing_session" {
					h.SetAdminSessionAuthority(e.admin)
				}
				if mode == "user_token" {
					e.token = e.owner.tokens.AccessToken
				}
				body := `{"thread_ids":["` + e.owner.thread.ID + `"]}`
				switch mode {
				case "unknown_fields":
					body = `{"thread_ids":["` + e.owner.thread.ID + `"],"principal":{"kind":"admin"}}`
				case "empty":
					body = `{"thread_ids":[]}`
				case "duplicate":
					body = `{"thread_ids":["` + e.owner.thread.ID + `","` + e.owner.thread.ID + `"]}`
				case "large":
					body = strings.Repeat(" ", 9000) + body
				case "unknown_id":
					body = `{"thread_ids":["missing"]}`
				}
				r := gin.New()
				r.POST("/preview", middleware.RequireAdminDomainAccess(e.owner.jwts.Admin, portadapt.NewAdminDomainSessionVerifier(e.admin), nil), h.AdminPermissionsPreview)
				request := httptest.NewRequest("POST", "/preview?scope=all&strength=mfa", strings.NewReader(body))
				request.Header.Set("Authorization", "Bearer "+e.token)
				record := httptest.NewRecorder()
				r.ServeHTTP(record, request)
				want := 200
				switch mode {
				case "no_code":
					want = 403
				case "missing_source", "missing_governance":
					want = 503
				case "missing_session", "user_token", "revoked":
					want = 401
				case "unknown_fields", "empty", "duplicate", "large":
					want = 400
				case "unknown_id":
					want = 404
				}
				if record.Code != want {
					t.Fatal(record.Code, want, record.Body.String())
				}
				if want == 200 {
					var response struct {
						Data struct {
							Items []service.AdminThreadPermissionsPreview `json:"items"`
						} `json:"data"`
					}
					if err := json.Unmarshal(record.Body.Bytes(), &response); err != nil || len(response.Data.Items) != 1 {
						t.Fatal(response, err)
					}
					if mode == "password" || mode == "expired_mfa" {
						if response.Data.Items[0].Permissions != (service.AdminThreadPermissionFlags{}) {
							t.Fatal("read session became MFA", response)
						}
					}
					if record.Header().Get("Cache-Control") != "private, no-store" {
						t.Fatal("cache header")
					}
				}
				for _, secret := range []string{e.owner.thread.Content, e.owner.thread.Title, "facts_version", "credential_id", "grants", "execution_query"} {
					if secret != "" && strings.Contains(record.Body.String(), secret) {
						t.Fatal("private preview", record.Body.String())
					}
				}
			})
		}
	}
}
