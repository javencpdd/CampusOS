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
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestThreadGovernanceTakeDownHTTPCurrentDomainsAndSafeBody(t *testing.T) {
	for _, mode := range []string{"user", "admin", "password", "no_grant", "wrong_board", "user_to_admin", "admin_to_user", "forged_subject", "missing_proof", "revoked", "client_facts"} {
		t.Run(mode, func(t *testing.T) {
			e := newOwnerHTTPEnvironment(t)
			ctx := context.Background()
			now := time.Now().UTC()
			grants := identityrepo.NewMemoryDelegationRepository()
			admins := identityrepo.NewMemoryAdminDomainRepository()
			ar := identityrepo.NewMemoryAdminSessionRepository()
			if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: e.owner.ID, Username: "gov-http-admin", Email: "gov-http-admin@example.test"}); err != nil {
				t.Fatal(err)
			}
			strength := "mfa"
			if mode == "password" {
				strength = "password"
			}
			at := now.Add(-time.Minute)
			if err := ar.CreateSession(ctx, identityrepo.AdminSession{ID: "adms.gov-http", AccountID: e.owner.ID, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: "adms.gov-http", AuthenticationStrength: strength, MFAAuthenticatedAt: &at, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			admin := identityservice.NewAdminSessionService(admins, ar, e.jwts.Admin)
			if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.gov-http", AccountID: e.owner.ID, RoleID: "security_admin", GrantedBy: e.owner.ID}); err != nil {
				t.Fatal(err)
			}
			if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.take_down"}, "test"); err != nil {
				t.Fatal(err)
			}
			board := e.thread.CategoryID
			if mode == "wrong_board" {
				board = "other-board"
			}
			if mode != "no_grant" {
				if err := grants.InsertDelegations(ctx, []identityrepo.Delegation{{ID: "grant.gov-http", Kind: "grant", SubjectKind: "user", SubjectID: e.owner.ID, Action: "community.thread.take_down", BoardID: board, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
					t.Fatal(err)
				}
			}
			gov := repository.NewMemoryContentGovernanceRepository()
			store := reliability.NewMemoryStore()
			s := service.NewThreadService(e.repo, nil)
			s.SetGovernanceRepository(gov)
			s.SetReliability(reliability.NewService(transaction.NewMemory(e.repo, gov, store), store))
			s.SetGovernanceAuthority(identityservice.NewBoardGovernanceAuthorityService(grants, admins, admin))
			h := NewThreadHandler(s)
			if mode != "missing_proof" {
				h.SetOwnerSessionProof(e.identity, e.identity)
				h.SetAdminSessionAuthority(admin)
			}
			r := gin.New()
			r.POST("/threads/:id/take-down", middleware.JWTAuth(e.jwts.User, e.identity), h.TakeDown)
			r.POST("/admin/threads/:id/take-down", middleware.RequireAdminDomainAccess(e.jwts.Admin, portadapt.NewAdminDomainSessionVerifier(admin), nil), h.AdminTakeDown)
			token := e.tokens.AccessToken
			route := "/threads/98311/take-down"
			expected := 403
			switch mode {
			case "user":
				expected = 200
			case "admin", "password", "admin_to_user", "forged_subject":
				subject := e.owner.ID
				if mode == "forged_subject" {
					subject = "other-admin"
				}
				var err error
				token, err = e.jwts.Admin.GenerateAccessToken(subject, "Admin", auth.AccessTokenContext{SessionID: "adms.gov-http", AuthVersion: 1})
				if err != nil {
					t.Fatal(err)
				}
				if mode != "admin_to_user" {
					route = "/admin/threads/98311/take-down"
				}
				if mode == "admin" {
					expected = 200
				}
				if mode == "admin_to_user" || mode == "forged_subject" {
					expected = 401
				}
			case "user_to_admin":
				route = "/admin/threads/98311/take-down"
				expected = 401
			case "missing_proof":
				expected = 401
			case "revoked":
				if _, err := e.sessions.RevokeByUser(ctx, e.owner.ID, "gov-test", now); err != nil {
					t.Fatal(err)
				}
				expected = 401
			case "client_facts":
				expected = 400
			}
			body := `{"reason":"governance reason"}`
			if mode == "client_facts" {
				body = `{"reason":"forged","actor_kind":"admin","board_id":"98321"}`
			}
			req := httptest.NewRequest("POST", route+"?actor_kind=admin&authentication_strength=mfa&board_id=other", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Actor-ID", e.owner.ID)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != expected {
				t.Fatal(mode, rec.Code, expected, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), e.thread.Content) || strings.Contains(rec.Body.String(), e.thread.Title) || strings.Contains(rec.Body.String(), "facts_version") {
				t.Fatal("execute acknowledgement leaked private resource", rec.Body.String())
			}
			stored, _ := e.repo.GetByID(ctx, e.thread.ID)
			if (stored.ModerationStatus == "taken_down") != (expected == 200) {
				t.Fatal(stored)
			}
		})
	}
}
