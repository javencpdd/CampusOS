package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
)

func namedAdminQuery(code string) identityport.AdminExecutionQuery {
	q := identityport.AdminExecutionQuery{PermissionCode: code, Scope: identityport.PolicyResourceRef{Kind: "community.board", ID: "actual-board"}}
	if code == "platform.reliability.read" {
		q.Scope = identityport.PolicyResourceRef{Kind: "platform.event_metadata", ID: "recent"}
	}
	if code == "personal_space.asset.manage" {
		q.Scope = identityport.PolicyResourceRef{Kind: "richtext.user_asset", ID: "actual-asset"}
	}
	if code == "personal_space.asset.read_audit" {
		q.Scope = identityport.PolicyResourceRef{Kind: "richtext.asset_governance", ID: "summary"}
	}
	return q
}
func TestAdminExecutionAuthorityExactActionDomainAndMFA(t *testing.T) {
	for _, code := range []string{"community.thread.read", "community.thread.pin", "community.thread.lock", "community.thread.review", "community.thread.trash", "community.thread.direct_restore", "community.thread.restore", "community.thread.purge", "platform.reliability.read", "personal_space.asset.manage", "personal_space.asset.read_audit"} {
		for _, mode := range []string{"current", "password", "expired", "other_code", "no_code", "user", "missing", "wrong_scope", "unknown"} {
			t.Run(code+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				admin, proof := governanceAdminProof(t, mode)
				repo := admin.admins.(*repository.MemoryAdminDomainRepository)
				if err := repo.BindRole(ctx, repository.AdminDomainRoleBinding{ID: "binding.named", AccountID: "22001", RoleID: "security_admin", GrantedBy: "22001"}); err != nil {
					t.Fatal(err)
				}
				codes := []string{code}
				if mode == "other_code" {
					codes = []string{"community.thread.take_down"}
				}
				if mode == "no_code" {
					codes = nil
				}
				if err := repo.ReplaceAdminRolePermissionFacts(ctx, "security_admin", codes, "seed"); err != nil {
					t.Fatal(err)
				}
				s := NewBoardGovernanceAuthorityService(nil, repo, admin)
				q := namedAdminQuery(code)
				switch mode {
				case "user":
					proof = governanceUserProof(t)
				case "missing":
					s.adminSessions = nil
				case "wrong_scope":
					q.Scope.ID = ""
				case "unknown":
					q.PermissionCode = "community.thread.take_down"
				}
				a, err := s.LoadAdminExecutionAuthority(ctx, q, proof, false)
				if mode == "user" || mode == "missing" || mode == "wrong_scope" || mode == "unknown" {
					if err == nil {
						t.Fatal("invalid authority accepted", a)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				want := mode == "current" || (!q.RequiresCurrentMFA() && (mode == "password" || mode == "expired"))
				if a.Allowed != want {
					t.Fatal(mode, a, want)
				}
				if err := identityport.CheckAdminExecutionAuthority(a, a); err != nil {
					t.Fatal(err)
				}
				if raw, err := json.Marshal(a); err != nil || string(raw) != "{}" {
					t.Fatal("authority leaked as DTO", string(raw), err)
				}
				changed := a
				changed.Query.Scope.ID = "other"
				if !errors.Is(identityport.CheckAdminExecutionAuthority(a, changed), identityport.ErrPolicyFactsChanged) {
					t.Fatal("scope replay accepted")
				}
				changed = a
				changed.Principal.CredentialID = "another"
				if !errors.Is(identityport.CheckAdminExecutionAuthority(a, changed), identityport.ErrPolicyFactsChanged) {
					t.Fatal("credential replay accepted")
				}
				changed = a
				var ptr *int
				changed.Binding = ptr
				if !errors.Is(identityport.CheckAdminExecutionAuthority(a, changed), identityport.ErrPolicyFactsChanged) {
					t.Fatal("typed nil accepted")
				}
			})
		}
	}
}

func TestAdminExecutionAuthorityCurrentWholeWitnessRecheck(t *testing.T) {
	for _, mode := range []string{"permission_aba", "session_revoke", "session_version", "binding_revoke", "missing_facts", "outside_transaction"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			admin, base := governanceAdminProof(t, "current")
			repo := admin.admins.(*repository.MemoryAdminDomainRepository)
			if err := repo.BindRole(ctx, repository.AdminDomainRoleBinding{ID: "binding.named", AccountID: "22001", RoleID: "security_admin", GrantedBy: "22001"}); err != nil {
				t.Fatal(err)
			}
			q := namedAdminQuery("community.thread.review")
			if mode != "missing_facts" {
				if err := repo.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{q.PermissionCode}, "seed"); err != nil {
					t.Fatal(err)
				}
			}
			s := NewBoardGovernanceAuthorityService(nil, repo, admin)
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 2 {
					switch mode {
					case "permission_aba":
						repo.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "seed")
						repo.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{q.PermissionCode}, "seed")
					case "session_revoke":
						admin.sessions.RevokeSessionCAS(ctx, "adms.governance.memory", 1, "test", time.Now())
					case "session_version":
						admin.sessions.UpgradeSessionStrengthCAS(ctx, "adms.governance.memory", 1, "mfa", time.Now(), time.Now())
					case "binding_revoke":
						repo.RevokeBindingCAS(ctx, "binding.named", 1, time.Now())
					}
				}
				return base(ctx)
			}
			if mode == "outside_transaction" {
				if _, err := s.LoadAdminExecutionAuthority(ctx, q, proof, true); err == nil {
					t.Fatal("lock without transaction accepted")
				}
				return
			}
			a, err := s.LoadAdminExecutionAuthority(ctx, q, proof, false)
			if mode == "missing_facts" {
				if err != nil || a.Allowed {
					t.Fatal(a, err)
				}
				return
			}
			if err == nil {
				t.Fatal("changed witness accepted", a)
			}
		})
	}
}
