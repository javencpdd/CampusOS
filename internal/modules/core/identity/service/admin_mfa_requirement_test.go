package service

import (
	"context"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
)

func TestAdminMFAOptionalExecutionKeepsPasswordAndCurrentPermissions(t *testing.T) {
	ctx := context.Background()
	for _, code := range []string{"community.thread.pin", "community.thread.review", "personal_space.asset.manage", "personal_space.space.manage", "plugin.plugin.install", "plugin.plugin.configure", "plugin.plugin.lifecycle", "plugin.plugin.uninstall", "ai.ai.manage", "knowledge.collection.manage", "identity.integration.manage"} {
		t.Run(code, func(t *testing.T) {
			admin, proof := governanceAdminProof(t, "password")
			repo := admin.admins.(*repository.MemoryAdminDomainRepository)
			if err := repo.BindRole(ctx, repository.AdminDomainRoleBinding{ID: "optional.binding", AccountID: "22001", RoleID: "security_admin", GrantedBy: "22001"}); err != nil {
				t.Fatal(err)
			}
			if err := repo.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{code}, "test"); err != nil {
				t.Fatal(err)
			}
			q := namedAdminQuery(code)
			switch {
			case strings.HasPrefix(code, "plugin."):
				q.Scope = identityport.PolicyResourceRef{Kind: "plugin.installation_inventory", ID: "all"}
			case strings.HasPrefix(code, "ai."):
				q.Scope = identityport.PolicyResourceRef{Kind: "ai.profile_inventory", ID: "all"}
			case strings.HasPrefix(code, "knowledge."):
				q.Scope = identityport.PolicyResourceRef{Kind: "knowledge.collection_inventory", ID: "all"}
			case strings.HasPrefix(code, "identity.integration."):
				q.Scope = identityport.PolicyResourceRef{Kind: "identity.integration_inventory", ID: "all"}
			case code == "personal_space.space.manage":
				q.Scope = identityport.PolicyResourceRef{Kind: "personal_space.space", ID: "other-owner"}
			}
			s := NewBoardGovernanceAuthorityService(nil, repo, admin)
			if a, err := s.LoadAdminExecutionAuthority(ctx, q, proof, false); err != nil || a.Allowed {
				t.Fatal("default MFA requirement bypassed", a, err)
			}
			admin.SetMFAOptional(true)
			a, err := s.LoadAdminExecutionAuthority(ctx, q, proof, false)
			if err != nil || !a.Allowed || !a.Principal.AdminMFAOptional || a.Principal.AuthenticationStrength != "password" {
				t.Fatal("explicit opt-out failed or fabricated MFA", a, err)
			}
			if err := repo.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "revoke"); err != nil {
				t.Fatal(err)
			}
			if a, err := s.LoadAdminExecutionAuthority(ctx, q, proof, false); err != nil || a.Allowed {
				t.Fatal("opt-out bypassed permission revocation", a, err)
			}
			admin.SetMFAOptional(false)
			forged := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := proof(ctx)
				p.AdminMFAOptional = true
				return p, e
			}
			if _, err := s.LoadAdminExecutionAuthority(ctx, q, forged, false); err == nil {
				t.Fatal("client-forged opt-out accepted")
			}
		})
	}
}

func TestAdminMFAOptionalBoardAuthorityAndProfile(t *testing.T) {
	ctx := context.Background()
	admin, proof := governanceAdminProof(t, "password")
	repo := admin.admins.(*repository.MemoryAdminDomainRepository)
	if err := repo.BindRole(ctx, repository.AdminDomainRoleBinding{ID: "optional.board", AccountID: "22001", RoleID: "security_admin", GrantedBy: "22001"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.take_down"}, "test"); err != nil {
		t.Fatal(err)
	}
	s := NewBoardGovernanceAuthorityService(nil, repo, admin)
	q := identityport.BoardGovernanceQuery{BoardID: "actual-board", Action: "community.thread.take_down"}
	admin.SetMFAOptional(true)
	a, err := s.LoadBoardGovernanceAuthority(ctx, q, proof, false)
	if err != nil || len(a.Grants) != 1 || a.Grants[0].RequiredStrength != "password" || !decideGovernanceAuthority(t, a, q) {
		t.Fatal(a, err)
	}
	admin.SetMFAOptional(false)
	if a, err := s.LoadBoardGovernanceAuthority(ctx, q, proof, false); err != nil || len(a.Grants) != 0 {
		t.Fatal("re-enabled requirement failed", a, err)
	}
	self, env, _, current := adminSPASelfFixture(t, true)
	for _, optional := range []bool{false, true} {
		env.service.SetMFAOptional(optional)
		p, err := self.CurrentProfile(ctx, current)
		if err != nil || p.MFARequired == optional || p.AuthenticationStrength != "password" || p.MFAEnrolled {
			t.Fatal(p, err)
		}
	}
}
