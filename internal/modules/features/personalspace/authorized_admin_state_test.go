package space

import (
	"context"
	"encoding/json"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"reflect"
	"strings"
	"testing"
	"time"
)

type spaceAdminFixtureData struct {
	s        *Service
	repo     *MemoryRepository
	users    *identityrepo.MemoryUserRepository
	admins   *identityrepo.MemoryAdminDomainRepository
	sessions *identityrepo.MemoryAdminSessionRepository
	events   *reliability.MemoryStore
	current  *identityservice.AdminSessionService
	jwt      *auth.JWTManager
	claims   *auth.JWTClaims
	token    string
	proof    identityport.SessionPrincipalResolver
}

func spaceAdminFixture(t *testing.T, existing bool, actor, mode string) spaceAdminFixtureData {
	t.Helper()
	ctx := context.Background()
	f := spaceAdminFixtureData{repo: NewMemoryRepository(), users: identityrepo.NewMemoryUserRepository(), admins: identityrepo.NewMemoryAdminDomainRepository(), sessions: identityrepo.NewMemoryAdminSessionRepository(), events: reliability.NewMemoryStore()}
	u := &identitydomain.User{ID: "1001", Username: "space-owner", Nickname: "Owner", Email: "owner@example.test", Status: identitydomain.UserStatusActive}
	if err := f.users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if existing {
		row := defaultSpace(u)
		row.ID = "2001"
		row.IsDefault = false
		row.Visibility = VisibilityPrivate
		row.LastSyncError = "/host/private"
		row.SyncTags = []string{"kept"}
		if err := f.repo.Upsert(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: actor, Username: "space-admin", Email: "space-admin@example.test"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	at := now.Add(-time.Minute)
	if mode == "expired_mfa" {
		at = now.Add(-time.Hour)
	}
	strength, mfaAt := "mfa", &at
	if mode == "password" {
		strength, mfaAt = "password", nil
	}
	if err := f.sessions.CreateSession(ctx, identityrepo.AdminSession{ID: "adms.space", AccountID: actor, RefreshTokenDigest: strings.Repeat("b", 64), TokenFamilyID: "adms.space", AuthenticationStrength: strength, MFAAuthenticatedAt: mfaAt, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	f.jwt = auth.NewJWTManager(auth.JWTConfig{Secret: "space-admin-unit-test-key", Issuer: "space-admin-test", Audience: "admin", AccessTTL: time.Hour})
	f.current = identityservice.NewAdminSessionService(f.admins, f.sessions, f.jwt)
	var err error
	f.token, err = f.jwt.GenerateAccessToken(actor, "Admin", auth.AccessTokenContext{SessionID: "adms.space", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.claims, err = f.jwt.VerifyAccessToken(f.token)
	if err != nil {
		t.Fatal(err)
	}
	f.proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, f.current, f.claims)
	}
	if err := f.admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.space", AccountID: actor, RoleID: "security_admin", GrantedBy: actor}); err != nil {
		t.Fatal(err)
	}
	if err := f.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"personal_space.space.manage"}, "space-test"); err != nil {
		t.Fatal(err)
	}
	f.s = NewService(f.repo, identityUserReader{reader: portadapt.NewRepositoryUserReader(f.users)})
	f.s.SetAdminExecutionAuthority(identityservice.NewBoardGovernanceAuthorityService(nil, f.admins, f.current))
	f.s.SetReliability(reliability.NewService(transaction.NewMemory(f.repo, f.users, f.admins, f.sessions, f.events), spaceOwnerAuditFailure{Store: f.events, mode: mode}))
	return f
}
func TestSpaceAdminDisableCurrentDefaultExistingTypedActorAndRollback(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, actor := range []string{"1001", "adm.space.opaque"} {
			for _, mode := range []string{"current", "suspended", "audit", "outbox", "missing_proof", "missing_authority", "user_domain", "revoked_permission", "expired_mfa", "credential", "feature_disabled", "long_reason"} {
				t.Run(actor+"/"+map[bool]string{true: "existing", false: "default"}[existing]+"/"+mode, func(t *testing.T) {
					f := spaceAdminFixture(t, existing, actor, mode)
					ctx := context.Background()
					proof := f.proof
					reason := " review required "
					switch mode {
					case "suspended":
						u, _ := f.users.GetByID(ctx, "1001")
						u.Status = identitydomain.UserStatusSuspended
						if err := f.users.Update(ctx, u); err != nil {
							t.Fatal(err)
						}
					case "missing_proof":
						proof = nil
					case "missing_authority":
						f.s.adminExecution = nil
					case "user_domain":
						proof = spaceOwnerProof
					case "revoked_permission":
						if err := f.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "revoke"); err != nil {
							t.Fatal(err)
						}
					case "credential":
						calls := 0
						proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
							p, e := f.proof(ctx)
							calls++
							if calls > 2 {
								p.CredentialID = "changed"
							}
							return p, e
						}
					case "feature_disabled":
						f.s.SetPluginEnabledChecker(func() bool { return false })
					case "long_reason":
						reason = strings.Repeat("长", 501)
					}
					before, events := f.repo.Snapshot(), f.events.Snapshot()
					ack, err := f.s.DisableSpaceForPrincipal(ctx, "1001", reason, proof)
					allowed := mode == "current" || mode == "suspended"
					if (err == nil) != allowed {
						t.Fatal(ack, err)
					}
					if !allowed {
						if ack != nil || !reflect.DeepEqual(before, f.repo.Snapshot()) || !reflect.DeepEqual(events, f.events.Snapshot()) {
							t.Fatal("denied command retained state", ack, err)
						}
						return
					}
					row, err := f.repo.GetByUserID(ctx, "1001")
					if err != nil || row.DisabledAt == nil || row.DisabledBy != "" || row.DisabledActorKind != "admin" || row.DisabledActorID != actor || row.DisabledReason != "review required" || ack.OwnerID != "1001" || ack.SpaceID != row.ID || !ack.Disabled {
						t.Fatal(row, ack, err)
					}
					if existing && (row.Visibility != VisibilityPrivate || row.LastSyncError != "/host/private" || !reflect.DeepEqual(row.SyncTags, []string{"kept"})) {
						t.Fatal("unrelated fields changed", row)
					}
					raw, _ := json.Marshal(ack)
					if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "reason") {
						t.Fatal(string(raw))
					}
					operations, _, err := f.events.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 10})
					if err != nil || len(operations) != 1 || operations[0].ActorType != "admin" || operations[0].ActorID != actor {
						t.Fatal(operations, err)
					}
				})
			}
		}
	}
}

func TestSpaceAdminEnableCurrentDefaultExistingAndRequiredRollback(t *testing.T) {
	for _, state := range []string{"default", "enabled", "disabled"} {
		for _, mode := range []string{"current", "audit", "outbox", "revoked_permission"} {
			t.Run(state+"/"+mode, func(t *testing.T) {
				f := spaceAdminFixture(t, state != "default", "adm.enable.space", "")
				ctx := context.Background()
				if state == "disabled" {
					if _, err := f.s.DisableSpaceForPrincipal(ctx, "1001", "review", f.proof); err != nil {
						t.Fatal(err)
					}
				}
				f.s.SetReliability(reliability.NewService(transaction.NewMemory(f.repo, f.users, f.admins, f.sessions, f.events), spaceOwnerAuditFailure{Store: f.events, mode: mode}))
				if mode == "revoked_permission" {
					if err := f.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "revoke"); err != nil {
						t.Fatal(err)
					}
				}
				before, events := f.repo.Snapshot(), f.events.Snapshot()
				ack, err := f.s.EnableSpaceForPrincipal(ctx, "1001", f.proof)
				if mode != "current" {
					if err == nil || ack != nil || !reflect.DeepEqual(before, f.repo.Snapshot()) || !reflect.DeepEqual(events, f.events.Snapshot()) {
						t.Fatal("failed enable retained state", ack, err)
					}
					return
				}
				if err != nil || ack == nil || ack.Disabled || ack.OwnerID != "1001" {
					t.Fatal(ack, err)
				}
				if state == "default" {
					if ack.SpaceID != "" || !reflect.DeepEqual(before, f.repo.Snapshot()) {
						t.Fatal("default enable created row", ack)
					}
					return
				}
				row, err := f.repo.GetByUserID(ctx, "1001")
				if err != nil || row.DisabledAt != nil || row.DisabledBy != "" || row.DisabledReason != "" || row.DisabledActorKind != "legacy_unknown" || row.DisabledActorID != "" || row.Visibility != VisibilityPrivate || row.LastSyncError != "/host/private" {
					t.Fatal(row, err)
				}
				if state == "enabled" && !reflect.DeepEqual(before, f.repo.Snapshot()) {
					t.Fatal("enabled no-op rewrote row")
				}
			})
		}
	}
}
