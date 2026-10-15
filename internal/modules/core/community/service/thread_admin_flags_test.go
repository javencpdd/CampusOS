package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

func adminFlagFixture(t *testing.T) (*ThreadService, *repository.MemoryThreadRepository, *repository.MemoryContentGovernanceRepository, *reliability.MemoryStore, *identityrepo.MemoryAdminDomainRepository, *identityrepo.MemoryAdminSessionRepository, identityport.SessionPrincipalResolver) {
	t.Helper()
	s, threads, gov, store := authorUpdateFixture(t)
	ctx := context.Background()
	admins := identityrepo.NewMemoryAdminDomainRepository()
	sessions := identityrepo.NewMemoryAdminSessionRepository()
	now := time.Now().UTC()
	const actor = "190102" // Actual User owner has the same number; domains stay independent.
	if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: actor, Username: "flag-admin", Email: "flag-admin@example.test"}); err != nil {
		t.Fatal(err)
	}
	at := now.Add(-time.Minute)
	if err := sessions.CreateSession(ctx, identityrepo.AdminSession{ID: "adms.flags", AccountID: actor, RefreshTokenDigest: strings.Repeat("b", 64), TokenFamilyID: "adms.flags", AuthenticationStrength: "mfa", MFAAuthenticatedAt: &at, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "admin-flags-unit-test-secret", Issuer: "flags", Audience: "admin", AccessTTL: time.Hour})
	current := identityservice.NewAdminSessionService(admins, sessions, jwt)
	token, err := jwt.GenerateAccessToken(actor, "Admin", auth.AccessTokenContext{SessionID: "adms.flags", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.flags", AccountID: actor, RoleID: "security_admin", GrantedBy: actor}); err != nil {
		t.Fatal(err)
	}
	if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.pin", "community.thread.lock"}, "test"); err != nil {
		t.Fatal(err)
	}
	s.SetAdminExecutionAuthority(identityservice.NewBoardGovernanceAuthorityService(nil, admins, current))
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, current, claims)
	}
	return s, threads, gov, store, admins, sessions, proof
}

func TestThreadAdminFlagCurrentAuthorityStateAndSafeResult(t *testing.T) {
	ctx := context.Background()
	s, threads, gov, store, _, _, proof := adminFlagFixture(t)
	for _, op := range []string{"pin", "unpin", "lock", "unlock", "unlock"} {
		before, _ := threads.GetByID(ctx, "190101")
		result, err := s.AdminSetThreadFlag(ctx, before.ID, op, proof)
		if err != nil || result == nil {
			t.Fatal(op, result, err)
		}
		after, _ := threads.GetByID(ctx, before.ID)
		wantPinned, wantLocked := before.IsPinned, before.IsLocked
		switch op {
		case "pin":
			wantPinned = true
		case "unpin":
			wantPinned = false
		case "lock":
			wantLocked = true
		case "unlock":
			wantLocked = false
		}
		if after.IsPinned != wantPinned || after.IsLocked != wantLocked || after.FactsVersion <= before.FactsVersion || after.CurrentRevision != before.CurrentRevision+1 || after.Content != before.Content || after.PublicationStatus != before.PublicationStatus || after.ModerationActorID != before.ModerationActorID {
			t.Fatal(op, after)
		}
		raw, _ := json.Marshal(result)
		var fields map[string]any
		json.Unmarshal(raw, &fields)
		if len(fields) != 3 || strings.Contains(string(raw), before.Content) || strings.Contains(string(raw), before.Title) {
			t.Fatal("private result", string(raw))
		}
	}
	actions, err := gov.ListModerationActions(ctx, "190101")
	if err != nil || len(actions) != 5 {
		t.Fatal(actions, err)
	}
	for _, a := range actions {
		if a.ActorKind != "admin" || a.ActorRefID != "190102" || a.ActorID != "" {
			t.Fatal(a)
		}
	}
	records, _, err := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
	if err != nil || len(records) != 5 {
		t.Fatal(records, err)
	}
	for _, a := range records {
		if a.ActorType != "admin" || a.ActorID != "190102" {
			t.Fatal(a)
		}
	}
}

func TestThreadAdminFlagRevocationAndRequiredFailureRollback(t *testing.T) {
	for _, mode := range []string{"permission_aba", "session_revoke", "credential", "body_aba", "board_change", "owner_change", "final_permission", "final_body", "audit", "history", "missing", "no_code", "user", "trash", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, threads, gov, store, admins, sessions, proof := adminFlagFixture(t)
			ctx := context.Background()
			op := "pin"
			switch mode {
			case "audit":
				s.SetReliability(reliability.NewService(transaction.NewMemory(threads, gov, store), authorFailAudit{Store: store}))
			case "history":
				s.SetGovernanceRepository(takeDownFailHistory{ContentGovernanceRepository: gov})
			case "missing":
				s.SetAdminExecutionAuthority(nil)
			case "no_code":
				if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.take_down"}, "test"); err != nil {
					t.Fatal(err)
				}
			case "user":
				proof = authorUpdateProof("190102")
			case "trash":
				r, _ := threads.GetByID(ctx, "190101")
				r.DeletionStatus = "trashed"
				if err := threads.Update(ctx, r); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				op = "highlight"
			}
			before, _ := threads.GetByID(ctx, "190101")
			history := gov.Snapshot()
			calls := 0
			changing := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 3 || calls == 5 {
					switch {
					case calls == 3 && mode == "permission_aba" || calls == 5 && mode == "final_permission":
						if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "test"); err != nil {
							t.Fatal(err)
						}
						if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.pin", "community.thread.lock"}, "test"); err != nil {
							t.Fatal(err)
						}
					case calls == 3 && mode == "session_revoke":
						if _, _, err := sessions.RevokeSessionCAS(ctx, "adms.flags", 1, "test", time.Now()); err != nil {
							t.Fatal(err)
						}
					case calls == 3 && (mode == "body_aba" || mode == "board_change" || mode == "owner_change") || calls == 5 && mode == "final_body":
						r, _ := threads.GetByID(ctx, before.ID)
						switch mode {
						case "board_change":
							r.CategoryID = "other-board"
						case "owner_change":
							r.AuthorID = "other-owner"
						default:
							r.Content = "changed"
						}
						if err := threads.Update(ctx, r); err != nil {
							t.Fatal(err)
						}
						if mode == "body_aba" {
							r.Content = before.Content
							if err := threads.Update(ctx, r); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
				if mode == "credential" && calls >= 3 {
					return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
				}
				return proof(ctx)
			}
			result, err := s.AdminSetThreadFlag(ctx, before.ID, op, changing)
			if err == nil || result != nil {
				t.Fatal("failed flag accepted", result, err)
			}
			after, _ := threads.GetByID(ctx, before.ID)
			if after.IsPinned != before.IsPinned || after.IsLocked != before.IsLocked || !reflect.DeepEqual(history, gov.Snapshot()) {
				t.Fatal("partial flag/history persisted", after)
			}
			if mode == "unknown" && !errors.Is(err, identityport.ErrResourcePolicyUnknownAction) {
				t.Fatal(err)
			}
		})
	}
}
