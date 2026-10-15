package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func userExecutionFixture(t *testing.T) (*UserScopedExecutionAuthorityService, *repository.MemoryRoleRepository, identityport.SessionPrincipalResolver) {
	t.Helper()
	sessions, users, jwt := newSessionServiceForTest(t)
	u, _ := users.GetByID(context.Background(), "22001")
	tokens, err := sessions.Issue(context.Background(), u, SessionMetadata{DeviceName: "User execution"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	roles := repository.NewMemoryRoleRepository()
	board := int64(42)
	if _, err := roles.AssignRole(context.Background(), u.ID, 2, "category", &board); err != nil {
		t.Fatal(err)
	}
	if err := roles.ReplaceRolePermissions(context.Background(), 2, []string{"community.thread.pin", "community.thread.lock"}, "test"); err != nil {
		t.Fatal(err)
	}
	s := NewUserScopedExecutionAuthorityService(roles, sessions, repository.NewMemoryAdminAccountRepository())
	return s, roles, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, sessions, sessions, claims)
	}
}

func userExecutionQuery() identityport.UserScopedExecutionQuery {
	return identityport.UserScopedExecutionQuery{PermissionCode: "community.thread.pin", Scope: identityport.PolicyResourceRef{Kind: "community.board", ID: "42"}}
}

func TestUserScopedExecutionAuthorityCurrentScopeAndDomain(t *testing.T) {
	for _, code := range []string{"community.thread.pin", "community.thread.lock"} {
		for _, mode := range []string{"current", "global", "cross_scope", "no_role", "no_code", "admin", "missing", "role_admin_active", "role_admin_missing", "role_admin_revoked", "outside_transaction"} {
			t.Run(code+"/"+mode, func(t *testing.T) {
				s, roles, proof := userExecutionFixture(t)
				ctx := context.Background()
				q := userExecutionQuery()
				q.PermissionCode = code
				switch mode {
				case "global":
					roles.RevokeRole(ctx, "22001", 2)
					roles.AssignRole(ctx, "22001", 2, "global", nil)
				case "cross_scope":
					q.Scope.ID = "43"
				case "no_role":
					roles.RevokeRole(ctx, "22001", 2)
				case "no_code":
					roles.ReplaceRolePermissions(ctx, 2, nil, "test")
				case "missing":
					s.roles = nil
				case "admin":
					_, proof = governanceAdminProof(t, "current")
				case "role_admin_active", "role_admin_missing", "role_admin_revoked":
					roles.RevokeRole(ctx, "22001", 2)
					roles.AssignRole(ctx, "22001", 1, "global", nil)
					if mode != "role_admin_missing" {
						s.admissions.EnsureActive(ctx, "22001", "test")
					}
					if mode == "role_admin_revoked" {
						s.admissions.Revoke(ctx, "22001")
					}
				case "outside_transaction":
					if _, err := s.LoadUserScopedExecutionAuthority(ctx, q, proof, true); err == nil {
						t.Fatal("lock without transaction accepted")
					}
					return
				}
				a, err := s.LoadUserScopedExecutionAuthority(ctx, q, proof, false)
				if mode == "admin" || mode == "missing" {
					if err == nil {
						t.Fatal("missing or wrong-domain source accepted", a)
					}
					return
				}
				want := mode == "current" || mode == "global" || mode == "role_admin_active"
				if err != nil || a.Allowed != want {
					t.Fatal(a, err, want)
				}
				if raw, _ := json.Marshal(a); string(raw) != "{}" {
					t.Fatal("source leaked as DTO", string(raw))
				}
				if err := transaction.NewMemory().Within(ctx, func(tx context.Context) error {
					current, err := s.LoadUserScopedExecutionAuthority(tx, q, proof, true)
					if err != nil {
						return err
					}
					return identityport.CheckUserScopedExecutionAuthority(a, current)
				}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestUserScopedExecutionAuthorityWholeWitnessAndRevocation(t *testing.T) {
	for _, mode := range []string{"permission_aba", "scope_aba", "role_metadata_aba", "session_revoke", "session_metadata", "session_expiry", "session_strength", "user_version", "user_status", "user_metadata", "credential", "admission_revoke"} {
		t.Run(mode, func(t *testing.T) {
			s, roles, base := userExecutionFixture(t)
			ctx := context.Background()
			role, err := roles.CreateCustomRole(ctx, repository.Role{Name: "execution_custom", Description: "Original"})
			if err != nil {
				t.Fatal(err)
			}
			roles.RevokeRole(ctx, "22001", 2)
			roles.ReplaceRolePermissions(ctx, role.ID, []string{"community.thread.pin"}, "test")
			roles.AssignRole(ctx, "22001", role.ID, "global", nil)
			if mode == "admission_revoke" {
				roles.AssignRole(ctx, "22001", 1, "global", nil)
				s.admissions.EnsureActive(ctx, "22001", "test")
			}
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				p, err := base(ctx)
				if calls != 2 || err != nil {
					return p, err
				}
				switch mode {
				case "permission_aba":
					roles.ReplaceRolePermissions(ctx, role.ID, nil, "test")
					roles.ReplaceRolePermissions(ctx, role.ID, []string{"community.thread.pin"}, "test")
				case "scope_aba":
					roles.RevokeRole(ctx, "22001", role.ID)
					roles.AssignRole(ctx, "22001", role.ID, "global", nil)
				case "role_metadata_aba":
					role.Description = "Changed"
					roles.UpdateCustomRole(ctx, *role)
					role.Description = "Original"
					roles.UpdateCustomRole(ctx, *role)
				case "credential":
					p.CredentialID = "another-session"
				case "admission_revoke":
					s.admissions.Revoke(ctx, "22001")
				case "user_version", "user_status", "user_metadata":
					u, _ := s.sessions.users.GetByID(ctx, "22001")
					if mode == "user_version" {
						u.AuthVersion++
					} else if mode == "user_status" {
						u.Status = domain.UserStatusSuspended
					} else {
						u.Bio = "Changed profile"
					}
					s.sessions.users.Update(ctx, u)
				default:
					session, _ := s.sessions.sessions.GetByID(ctx, p.CredentialID)
					switch mode {
					case "session_revoke":
						at := time.Now().UTC()
						session.RevokedAt = &at
					case "session_metadata":
						session.TokenFamilyID = "Changed family"
					case "session_expiry":
						session.ExpiresAt = time.Now().Add(-time.Minute)
					case "session_strength":
						session.AuthenticationStrength = domain.MFAAuthenticationTOTP
					}
					s.sessions.sessions.Update(ctx, session)
				}
				return p, nil
			}
			if a, err := s.LoadUserScopedExecutionAuthority(ctx, userExecutionQuery(), proof, false); err == nil {
				t.Fatal("changed complete authority accepted", a)
			}
		})
	}
	s, _, proof := userExecutionFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.LoadUserScopedExecutionAuthority(ctx, userExecutionQuery(), proof, false); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation not preserved", err)
	}
}
