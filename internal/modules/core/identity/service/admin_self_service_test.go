package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/pkg/auth"
)

func adminSPASelfFixture(t *testing.T, permissions bool) (*AdminSelfService, *adminSessionEnv, *repository.MemoryAdminSecurityRepository, identityport.SessionPrincipalResolver) {
	t.Helper()
	ctx := context.Background()
	env := newAdminSessionEnv()
	now := time.Now().UTC()
	id := "adm.spa.opaque"
	sid := "adms.spa.opaque"
	if err := env.admins.CreateAccount(ctx, repository.AdminDomainAccount{ID: id, Username: "spa-admin", Email: "spa-admin@example.test", DisplayName: "Current Admin"}); err != nil {
		t.Fatal(err)
	}
	if err := env.sessions.CreateSession(ctx, repository.AdminSession{ID: sid, AccountID: id, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: sid, AuthenticationStrength: "password", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := env.admins.BindRole(ctx, repository.AdminDomainRoleBinding{ID: "binding.spa.opaque", AccountID: id, RoleID: "security_admin", GrantedBy: id}); err != nil {
		t.Fatal(err)
	}
	if permissions {
		if err := env.admins.(*repository.MemoryAdminDomainRepository).ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.read"}, "spa-fixture"); err != nil {
			t.Fatal(err)
		}
	}
	security := repository.NewMemoryAdminSecurityRepository()
	s := NewAdminSelfService(env.service, env.admins, security)
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, env.service, &auth.JWTClaims{UserID: id, SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	return s, env, security, proof
}
func TestAdminSPACurrentSelfOpaqueIndependentRoleZeroPermissionsSafeDetachedAndNoWrites(t *testing.T) {
	ctx := context.Background()
	for _, permissions := range []bool{false, true} {
		s, env, security, proof := adminSPASelfFixture(t, permissions)
		admins := env.admins.(*repository.MemoryAdminDomainRepository)
		sessions := env.sessions.(*repository.MemoryAdminSessionRepository)
		beforeAdmins, beforeSessions, beforeSecurity := admins.Snapshot(), sessions.Snapshot(), security.Snapshot()
		result, err := s.CurrentProfile(ctx, proof)
		if err != nil || result.Domain != "admin" || result.Account.ID != "adm.spa.opaque" || result.AuthenticationStrength != "password" || len(result.Roles) != 1 || result.Roles[0].ID != "security_admin" || result.MFAEnrolled {
			t.Fatal(result, err)
		}
		want := 0
		if permissions {
			want = 1
		}
		if len(result.PermissionCodes) != want {
			t.Fatal(result.PermissionCodes)
		}
		encoded, _ := json.Marshal(result)
		for _, secret := range []string{"email", "credential", "refresh", "token_family", "nonce", "ciphertext", "granted_by", "facts_version", "session_id", "status_reason"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatal("private self response", string(encoded))
			}
		}
		result.Roles[0].Name = "client mutated"
		result.PermissionCodes = append(result.PermissionCodes, "identity.fake.grant")
		again, err := s.CurrentProfile(ctx, proof)
		if err != nil || again.Roles[0].Name == "client mutated" || len(again.PermissionCodes) != want {
			t.Fatal(again, err)
		}
		if !reflect.DeepEqual(beforeAdmins, admins.Snapshot()) || !reflect.DeepEqual(beforeSessions, sessions.Snapshot()) || !reflect.DeepEqual(beforeSecurity, security.Snapshot()) {
			t.Fatal("self read mutated current source")
		}
	}
}

type adminSPASelfRoleHook struct {
	*repository.MemoryAdminDomainRepository
	after func()
}

func (h *adminSPASelfRoleHook) LoadAdminRoleExecutionFacts(ctx context.Context, id string, lock bool) ([]repository.AdminRoleExecutionFact, error) {
	rows, err := h.MemoryAdminDomainRepository.LoadAdminRoleExecutionFacts(ctx, id, lock)
	if h.after != nil {
		f := h.after
		h.after = nil
		f()
	}
	return rows, err
}
func TestAdminSPACurrentSelfWholeFactsPermissionABASessionMFAAndSourceChangesClose(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"permission_aba", "session", "account", "role", "factor", "same_id_user", "missing_role_source", "missing_security", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, env, security, proof := adminSPASelfFixture(t, true)
			admins := env.admins.(*repository.MemoryAdminDomainRepository)
			hook := &adminSPASelfRoleHook{MemoryAdminDomainRepository: admins}
			s.admins = hook
			hook.after = func() {
				switch mode {
				case "permission_aba":
					if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "spa-fixture"); err != nil {
						t.Fatal(err)
					}
					if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.read"}, "spa-fixture"); err != nil {
						t.Fatal(err)
					}
				case "session":
					if _, _, err := env.sessions.RevokeSessionCAS(ctx, "adms.spa.opaque", 1, "test", time.Now()); err != nil {
						t.Fatal(err)
					}
				case "account":
					if _, _, err := admins.SetAccountStatusCAS(ctx, "adm.spa.opaque", 1, "suspended", "test", "adm.spa.opaque", time.Now()); err != nil {
						t.Fatal(err)
					}
				case "role":
					if _, _, err := admins.RevokeBindingCAS(ctx, "binding.spa.opaque", 1, time.Now()); err != nil {
						t.Fatal(err)
					}

				}
			}
			if mode == "factor" {
				original := proof
				calls := 0
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					calls++
					if calls == 2 {
						if _, err := security.UpsertTOTP(ctx, repository.AdminTOTPMethod{AccountID: "adm.spa.opaque", Status: "pending", KeyID: "k1", Nonce: "12345678", Ciphertext: strings.Repeat("c", 16), EnrollmentExpiresAt: time.Now().Add(time.Minute)}); err != nil {
							t.Fatal(err)
						}
					}
					return original(ctx)
				}
			}
			runCtx := ctx
			switch mode {
			case "same_id_user":
				proof = func(context.Context) (identityport.PrincipalContext, error) {
					return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "adm.spa.opaque"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "adms.spa.opaque"}, nil
				}
			case "missing_role_source":
				s.admins = &adminAuthorityWithoutLock{AdminDomainRepository: admins}
			case "missing_security":
				s.security = nil
			case "cancelled":
				var cancel context.CancelFunc
				runCtx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := s.CurrentProfile(runCtx, proof)
			if err == nil || result != nil {
				t.Fatal("changed self source accepted", mode, result, err)
			}
		})
	}
	if _, err := (*AdminSelfService)(nil).CurrentProfile(ctx, nil); !errors.Is(err, identityport.ErrAdminSessionPrincipalUnavailable) {
		t.Fatal(err)
	}
}
