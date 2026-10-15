package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
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

func TestPostGovernanceAdminHTTPIndependentCurrentDomain(t *testing.T) {
	for _, mode := range []string{"current", "password", "expired_mfa", "no_code", "missing", "forged_subject", "user_token", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			e := newOwnerHTTPEnvironment(t)
			ctx := context.Background()
			now := time.Now().UTC()
			posts := repository.NewMemoryPostRepository()
			if err := posts.Create(ctx, &domain.Post{ID: "98315", ThreadID: e.thread.ID, AuthorID: e.owner.ID, Content: "private reply", Status: "published"}); err != nil {
				t.Fatal(err)
			}
			e.repo.IncrementReplyCount(ctx, e.thread.ID, 1)
			admins := identityrepo.NewMemoryAdminDomainRepository()
			sessions := identityrepo.NewMemoryAdminSessionRepository()
			if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: e.owner.ID, Username: "post-admin", Email: "post-admin@example.test"}); err != nil {
				t.Fatal(err)
			}
			strength := "mfa"
			at := now.Add(-time.Minute)
			if mode == "password" {
				strength = "password"
			}
			if mode == "expired_mfa" {
				at = now.Add(-time.Hour)
			}
			if err := sessions.CreateSession(ctx, identityrepo.AdminSession{ID: "adms.post-http", AccountID: e.owner.ID, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: "adms.post-http", AuthenticationStrength: strength, MFAAuthenticatedAt: &at, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			admin := identityservice.NewAdminSessionService(admins, sessions, e.jwts.Admin)
			if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.post-http", AccountID: e.owner.ID, RoleID: "security_admin", GrantedBy: e.owner.ID}); err != nil {
				t.Fatal(err)
			}
			if mode != "no_code" {
				if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.post.delete"}, "test"); err != nil {
					t.Fatal(err)
				}
			}
			s := service.NewPostService(posts, nil)
			s.SetThreadRepository(e.repo)
			s.SetGovernanceAuthority(identityservice.NewBoardGovernanceAuthorityService(identityrepo.NewMemoryDelegationRepository(), admins, admin))
			store := reliability.NewMemoryStore()
			s.SetReliability(reliability.NewService(transaction.NewMemory(posts, e.repo), store))
			h := NewPostHandler(s)
			if mode != "missing" {
				h.SetAdminSessionAuthority(admin)
			}
			subject := e.owner.ID
			if mode == "forged_subject" {
				subject = "other-admin"
			}
			token, err := e.jwts.Admin.GenerateAccessToken(subject, "Admin", auth.AccessTokenContext{SessionID: "adms.post-http", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "user_token" {
				token = e.tokens.AccessToken
			}
			if mode == "revoked" {
				if _, _, err := sessions.RevokeSessionCAS(ctx, "adms.post-http", 1, "test", now); err != nil {
					t.Fatal(err)
				}
			}
			r := gin.New()
			r.DELETE("/admin/posts/:post_id", middleware.RequireAdminDomainAccess(e.jwts.Admin, portadapt.NewAdminDomainSessionVerifier(admin), nil), h.AdminGovernanceDelete)
			req := httptest.NewRequest("DELETE", "/admin/posts/98315?actor_kind=user&authentication_strength=mfa", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			want := 403
			switch mode {
			case "current":
				want = 204
			case "missing", "forged_subject", "user_token", "revoked":
				want = 401
			}
			if rec.Code != want {
				t.Fatal(mode, rec.Code, want, rec.Body.String())
			}
			deleted, err := posts.ReadPostCommandSnapshot(ctx, "98315")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "current" {
				if deleted.DeletedActorKind != "admin" || deleted.DeletedActorID != e.owner.ID || deleted.AuthorID != e.owner.ID || deleted.DeletedAt == nil {
					t.Fatal("same numeric domains mixed", deleted)
				}
			} else if deleted.DeletedAt != nil {
				t.Fatal("denied Admin operation persisted", deleted)
			}
		})
	}
}
