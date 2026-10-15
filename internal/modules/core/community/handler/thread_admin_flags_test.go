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
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestThreadAdminFlagHTTPCurrentAuthorityAndSafeAcknowledgement(t *testing.T) {
	for _, op := range []string{"pin", "unpin", "lock", "unlock"} {
		for _, mode := range []string{"current", "password", "expired_mfa", "no_code", "wrong_code", "missing_source", "missing_session", "forged_subject", "user_token", "revoked", "no_token", "trash", "unknown_resource", "client_facts"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				e := newEventMetadataHTTP(t, mode)
				ctx := context.Background()
				codes := []string{"community.thread.pin", "community.thread.lock"}
				if mode == "no_code" {
					codes = nil
				}
				if mode == "wrong_code" {
					codes = []string{"community.thread.take_down"}
				}
				if err := e.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", codes, "flag-http"); err != nil {
					t.Fatal(err)
				}
				gov := repository.NewMemoryContentGovernanceRepository()
				store := reliability.NewMemoryStore()
				s := service.NewThreadService(e.owner.repo, nil)
				s.SetGovernanceRepository(gov)
				s.SetReliability(reliability.NewService(transaction.NewMemory(e.owner.repo, gov, store), store))
				if mode != "missing_source" {
					s.SetAdminExecutionAuthority(e.authority)
				}
				h := NewThreadHandler(s)
				if mode != "missing_session" {
					h.SetAdminSessionAuthority(e.admin)
				}
				if mode == "user_token" {
					e.token = e.owner.tokens.AccessToken
				}
				if mode == "no_token" {
					e.token = ""
				}
				if mode == "revoked" {
					if _, _, err := e.sessions.RevokeSessionCAS(ctx, "adms.events", 1, "test", time.Now()); err != nil {
						t.Fatal(err)
					}
				}
				row, _ := e.owner.repo.GetByID(ctx, e.owner.thread.ID)
				row.IsPinned, row.IsLocked = op == "unpin", op == "unlock"
				if mode == "trash" {
					row.DeletionStatus = "trashed"
				}
				if err := e.owner.repo.Update(ctx, row); err != nil {
					t.Fatal(err)
				}
				r := gin.New()
				fn := h.PinThread
				switch op {
				case "unpin":
					fn = h.UnpinThread
				case "lock":
					fn = h.LockThread
				case "unlock":
					fn = h.UnlockThread
				}
				r.POST("/threads/:id/"+op, middleware.RequireAdminDomainAccess(e.owner.jwts.Admin, portadapt.NewAdminDomainSessionVerifier(e.admin), nil), fn)
				id := row.ID
				if mode == "unknown_resource" {
					id = "missing"
				}
				body := ""
				if mode == "client_facts" {
					body = `{"actor_kind":"user","actor_id":"98301","is_pinned":false,"permission_code":"other","board_id":"other"}`
				}
				req := httptest.NewRequest("POST", "/threads/"+id+"/"+op+"?owner=98301&strength=mfa", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+e.token)
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				want := 200
				switch mode {
				case "password", "expired_mfa", "no_code", "wrong_code", "trash":
					want = 403
				case "missing_source":
					want = 503
				case "missing_session", "forged_subject", "user_token", "revoked", "no_token":
					want = 401
				case "unknown_resource":
					want = 404
				}
				if rec.Code != want {
					t.Fatal(op, mode, rec.Code, want, rec.Body.String())
				}
				for _, secret := range []string{row.Content, row.Title, "facts_version", "credential_id", "owner_id"} {
					if strings.Contains(rec.Body.String(), secret) {
						t.Fatal("private acknowledgement", rec.Body.String())
					}
				}
				stored, _ := e.owner.repo.GetByID(ctx, row.ID)
				if want == 200 {
					var response struct {
						Data map[string]any `json:"data"`
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || len(response.Data) != 3 {
						t.Fatal(response, err)
					}
					if stored.IsPinned != (op == "pin") && (op == "pin" || op == "unpin") {
						t.Fatal(stored)
					}
					if stored.IsLocked != (op == "lock") && (op == "lock" || op == "unlock") {
						t.Fatal(stored)
					}
				} else if stored.IsPinned != row.IsPinned || stored.IsLocked != row.IsLocked {
					t.Fatal("denied write persisted", stored)
				}
			})
		}
	}
}
