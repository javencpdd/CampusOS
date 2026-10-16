package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

type userFlagParticipant struct {
	check  func(context.Context, string) error
	record func(context.Context, identityport.PrincipalContext, string, *domain.Thread, *domain.Thread) error
}

func (p userFlagParticipant) CheckThreadFlagGovernance(ctx context.Context, op string) error {
	if p.check != nil {
		return p.check(ctx, op)
	}
	return nil
}
func (p userFlagParticipant) RecordThreadFlagGovernance(ctx context.Context, principal identityport.PrincipalContext, op string, before, after *domain.Thread) error {
	if p.record != nil {
		return p.record(ctx, principal, op, before, after)
	}
	return nil
}

type userFlagEnv struct {
	s        *ThreadService
	threads  *repository.MemoryThreadRepository
	gov      *repository.MemoryContentGovernanceRepository
	store    *reliability.MemoryStore
	roles    *identityrepo.MemoryRoleRepository
	sessions *identityrepo.MemorySessionRepository
	proof    identityport.SessionPrincipalResolver
}

func userFlagRevisions(t *testing.T, gov *repository.MemoryContentGovernanceRepository, id string) []*domain.ContentRevision {
	t.Helper()
	var snapshot struct {
		Revisions map[string][]*domain.ContentRevision
	}
	if err := json.Unmarshal(gov.Snapshot().([]byte), &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot.Revisions[id]
}

func userFlagFixture(t *testing.T) userFlagEnv {
	t.Helper()
	s, threads, gov, store := authorUpdateFixture(t)
	ctx := context.Background()
	users, sessions := identityrepo.NewMemoryUserRepository(), identityrepo.NewMemorySessionRepository()
	u := &identitydomain.User{ID: "190104", Username: "flag-user", Nickname: "User moderator", Email: "flag-user@example.test", Status: "active", AuthVersion: 1}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "user-flags-unit-test-secret", Issuer: "user-flags", Audience: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour})
	current, err := identityservice.NewSessionService(sessions, users, jwt, identityservice.SessionConfig{IPHashSecret: "user-flags-test-ip-key"})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := current.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "User moderator"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	roles := identityrepo.NewMemoryRoleRepository()
	board := int64(190103)
	if _, err := roles.AssignRole(ctx, u.ID, 2, "category", &board); err != nil {
		t.Fatal(err)
	}
	if err := roles.ReplaceRolePermissions(ctx, 2, []string{"community.thread.pin", "community.thread.lock"}, "test"); err != nil {
		t.Fatal(err)
	}
	s.SetUserScopedExecutionAuthority(identityservice.NewUserScopedExecutionAuthorityService(roles, current, identityrepo.NewMemoryAdminAccountRepository()))
	return userFlagEnv{s, threads, gov, store, roles, sessions, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, current, current, claims)
	}}
}

func TestThreadUserFlagCurrentScopeTypedHistoryAndDetachedCallbacks(t *testing.T) {
	e := userFlagFixture(t)
	ctx := context.Background()
	for _, operation := range []string{"pin", "unpin", "lock", "unlock", "unlock"} {
		before, _ := e.threads.GetByID(ctx, "190101")
		part := userFlagParticipant{record: func(_ context.Context, p identityport.PrincipalContext, op string, initial, written *domain.Thread) error {
			if p.Actor.Kind != "user" || p.Actor.ID != "190104" || op != operation || initial.ID != before.ID || written.CurrentRevision != initial.CurrentRevision+1 {
				t.Fatal("participant did not receive actual bound facts")
			}
			// Module code can never mutate the Core's captured original/written row.
			initial.Title, written.Content = "injected title", "injected body"
			if len(initial.Tags) > 0 {
				initial.Tags[0] = "injected tag"
			}
			if written.ModerationAt != nil {
				*written.ModerationAt = time.Now().Add(-time.Hour)
			}
			return nil
		}}
		result, err := e.s.UserSetThreadFlag(ctx, before.ID, operation, e.proof, part)
		if err != nil || result == nil {
			t.Fatal(operation, result, err)
		}
		after, _ := e.threads.GetByID(ctx, before.ID)
		wantPin, wantLock := before.IsPinned, before.IsLocked
		switch operation {
		case "pin":
			wantPin = true
		case "unpin":
			wantPin = false
		case "lock":
			wantLock = true
		case "unlock":
			wantLock = false
		}
		if after.IsPinned != wantPin || after.IsLocked != wantLock || after.FactsVersion <= before.FactsVersion || after.CurrentRevision != before.CurrentRevision+1 || after.Content != before.Content || after.Title != before.Title || !reflect.DeepEqual(after.Tags, before.Tags) || !reflect.DeepEqual(after.ModerationAt, before.ModerationAt) {
			t.Fatal("incorrect Core mutation", operation, after)
		}
		raw, _ := json.Marshal(result)
		var fields map[string]any
		json.Unmarshal(raw, &fields)
		if len(fields) != 3 || strings.Contains(string(raw), before.Content) || strings.Contains(string(raw), before.Title) {
			t.Fatal("private Thread leaked", string(raw))
		}
	}
	actions, err := e.gov.ListModerationActions(ctx, "190101")
	if err != nil || len(actions) != 5 {
		t.Fatal(actions, err)
	}
	for _, action := range actions {
		if action.ActorKind != "user" || action.ActorRefID != "190104" || action.ActorID != "190104" {
			t.Fatal("user actor lost", action)
		}
	}
	revisions := userFlagRevisions(t, e.gov, "190101")
	if len(revisions) != 5 {
		t.Fatal(revisions)
	}
	for _, revision := range revisions {
		if revision.CreatedActorKind != "user" || revision.CreatedActorID != "190104" || revision.CreatedBy != "190104" {
			t.Fatal("user revision actor lost", revision)
		}
	}
	audits, _, err := e.store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
	if err != nil || len(audits) != 5 {
		t.Fatal(audits, err)
	}
	for _, audit := range audits {
		if audit.ActorType != "user" || audit.ActorID != "190104" {
			t.Fatal("User platform audit lost", audit)
		}
	}
}

func TestThreadUserFlagCurrentProofRequiredFailureAndFinalRollback(t *testing.T) {
	for _, mode := range []string{"no_code", "cross_scope", "no_role", "admin", "missing", "missing_participant", "trash", "unknown", "audit", "history", "module_audit", "final_permission", "final_body", "final_config", "permission_aba", "body_aba", "board_change", "session_revoke"} {
		t.Run(mode, func(t *testing.T) {
			e := userFlagFixture(t)
			ctx := context.Background()
			op := "pin"
			proof := e.proof
			part := communityport.ThreadFlagGovernanceParticipant(userFlagParticipant{})
			switch mode {
			case "no_code":
				e.roles.ReplaceRolePermissions(ctx, 2, []string{"community.thread.lock"}, "test")
			case "cross_scope":
				e.roles.ReplaceRoleScopes(ctx, "190104", 2, "category", []int64{190105})
			case "no_role":
				e.roles.RevokeRole(ctx, "190104", 2)
			case "admin":
				_, _, _, _, _, _, proof = adminFlagFixture(t)
			case "missing":
				e.s.SetUserScopedExecutionAuthority(nil)
			case "missing_participant":
				part = nil
			case "trash":
				row, _ := e.threads.GetByID(ctx, "190101")
				row.DeletionStatus = "trashed"
				e.threads.Update(ctx, row)
			case "unknown":
				op = "take_down"
			case "audit":
				e.s.SetReliability(reliability.NewService(transaction.NewMemory(e.threads, e.gov, e.store), authorFailAudit{Store: e.store}))
			case "history":
				e.s.SetGovernanceRepository(takeDownFailHistory{ContentGovernanceRepository: e.gov})
			case "module_audit", "final_permission", "final_body", "final_config":
				changed := false
				part = userFlagParticipant{check: func(context.Context, string) error {
					if changed && mode == "final_config" {
						return errors.New("config disabled")
					}
					return nil
				}, record: func(ctx context.Context, _ identityport.PrincipalContext, _ string, _, _ *domain.Thread) error {
					changed = true
					if mode == "module_audit" {
						return errors.New("required module audit failed")
					}
					if mode == "final_permission" {
						return e.roles.ReplaceRolePermissions(ctx, 2, nil, "test")
					}
					if mode == "final_body" {
						row, _ := e.threads.GetByID(ctx, "190101")
						row.Content = "changed after write"
						return e.threads.Update(ctx, row)
					}
					return nil
				}}
			}
			before, _ := e.threads.GetByID(ctx, "190101")
			calls := 0
			base := proof
			proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 3 {
					switch mode {
					case "permission_aba":
						e.roles.ReplaceRolePermissions(ctx, 2, nil, "test")
						e.roles.ReplaceRolePermissions(ctx, 2, []string{"community.thread.pin", "community.thread.lock"}, "test")
					case "body_aba":
						row, _ := e.threads.GetByID(ctx, "190101")
						body := row.Content
						row.Content = "temporary body"
						e.threads.Update(ctx, row)
						row.Content = body
						e.threads.Update(ctx, row)
					case "board_change":
						row, _ := e.threads.GetByID(ctx, "190101")
						row.CategoryID = "190105"
						e.threads.Update(ctx, row)
					case "session_revoke":
						p, _ := base(ctx)
						session, _ := e.sessions.GetByID(ctx, p.CredentialID)
						at := time.Now()
						session.RevokedAt = &at
						e.sessions.Update(ctx, session)
					}
				}
				return base(ctx)
			}
			if result, err := e.s.UserSetThreadFlag(ctx, "190101", op, proof, part); err == nil || result != nil {
				t.Fatal("unsafe User flag accepted", mode, result, err)
			}
			after, _ := e.threads.GetByID(ctx, "190101")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed command changed resource", mode, before, after)
			}
			actions, _ := e.gov.ListModerationActions(ctx, "190101")
			revisions := userFlagRevisions(t, e.gov, "190101")
			audits, _, _ := e.store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
			if len(actions)+len(revisions)+len(audits) != 0 {
				t.Fatal("failed command retained history or audit", mode)
			}
		})
	}
}
