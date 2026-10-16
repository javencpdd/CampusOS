package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestAdminThreadMetadataHTTPRealCurrentJWTDomainNoBodyAndSafeProjection(t *testing.T) {
	for _, op := range []string{"list", "trash", "history"} {
		for _, mode := range []string{"current", "password", "expired_mfa", "no_code", "missing_source", "missing_session", "forged_subject", "user_token", "revoked", "no_token", "body", "private_search", "empty"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				e := newEventMetadataHTTP(t, mode)
				ctx := context.Background()
				codes := []string{"community.thread.read"}
				if mode == "no_code" {
					codes = nil
				}
				if err := e.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", codes, "metadata-http"); err != nil {
					t.Fatal(err)
				}
				row, _ := e.owner.repo.GetByID(ctx, e.owner.thread.ID)
				row.Title = "ADMIN_PRIVATE_TITLE_SENTINEL"
				row.Content = "ADMIN_PRIVATE_BODY_SENTINEL"
				row.PublicationStatus = "private"
				if op == "trash" {
					row.DeletionStatus = "trashed"
				}
				if err := e.owner.repo.Update(ctx, row); err != nil {
					t.Fatal(err)
				}
				s := service.NewThreadService(e.owner.repo, nil)
				s.SetGovernanceRepository(repository.NewMemoryContentGovernanceRepository())
				if mode != "missing_source" {
					s.SetAdminExecutionAuthority(e.authority)
				}
				h := NewThreadHandler(s)
				if mode != "missing_session" {
					h.SetAdminSessionAuthority(e.admin)
				}
				if mode == "revoked" {
					if _, _, err := e.sessions.RevokeSessionCAS(ctx, "adms.events", 1, "test", time.Now()); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "user_token" {
					e.token = e.owner.tokens.AccessToken
				}
				if mode == "no_token" {
					e.token = ""
				}
				r := gin.New()
				mw := middleware.RequireAdminDomainAccess(e.owner.jwts.Admin, portadapt.NewAdminDomainSessionVerifier(e.admin), nil)
				r.GET("/admin/threads", mw, h.AdminListThreads)
				r.GET("/admin/threads/trash", mw, h.AdminListTrash)
				r.GET("/admin/threads/:id/history", mw, h.AdminModerationActions)
				path := "/admin/threads"
				if op == "trash" {
					path += "/trash"
				}
				if op == "history" {
					path += "/" + row.ID + "/history"
				}
				path += "?actor_id=forged&strength=mfa"
				if mode == "empty" {
					path += "&page=99"
				}
				if mode == "private_search" {
					path += "&keyword=ADMIN_PRIVATE_BODY_SENTINEL"
				}
				body := ""
				if mode == "body" {
					body = `{"facts_version":1}`
				}
				request := httptest.NewRequest("GET", path, strings.NewReader(body))
				request.Header.Set("Authorization", "Bearer "+e.token)
				record := httptest.NewRecorder()
				r.ServeHTTP(record, request)
				want := 200
				switch mode {
				case "no_code":
					want = 403
				case "missing_source":
					want = 503
				case "missing_session", "forged_subject", "user_token", "revoked", "no_token":
					want = 401
				case "body":
					want = 400
				case "private_search":
					if op != "history" {
						want = 400
					}
				}
				if record.Code != want {
					t.Fatal(mode, record.Code, want, record.Body.String())
				}
				if want == 200 && record.Header().Get("Cache-Control") != "private, no-store" {
					t.Fatal("cache header")
				}
				for _, secret := range []string{row.Title, row.Content, "facts_version", "credential_id", "permission_facts", "reason\""} {
					if strings.Contains(record.Body.String(), secret) {
						t.Fatal("private metadata", record.Body.String())
					}
				}
			})
		}
	}
}
