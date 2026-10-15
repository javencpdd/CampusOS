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
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

func takeDownFixture(t *testing.T, kind string) (*ThreadService, *repository.MemoryThreadRepository, *repository.MemoryContentGovernanceRepository, *reliability.MemoryStore, *identityrepo.MemoryDelegationRepository, identityport.SessionPrincipalResolver) {
	t.Helper()
	s, threads, gov, store := authorUpdateFixture(t)
	grants := identityrepo.NewMemoryDelegationRepository()
	ctx := context.Background()
	now := time.Now().UTC()
	proof := authorUpdateProof("190104")
	var admins *identityrepo.MemoryAdminDomainRepository
	var sessions *identityservice.AdminSessionService
	if kind == "admin" {
		admins = identityrepo.NewMemoryAdminDomainRepository()
		sr := identityrepo.NewMemoryAdminSessionRepository()
		const id = "190102" // Equal to actual User owner, independently authenticated.
		if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: id, Username: "take-down-admin", Email: "take-down-admin@example.test"}); err != nil {
			t.Fatal(err)
		}
		at := now.Add(-time.Minute)
		if err := sr.CreateSession(ctx, identityrepo.AdminSession{ID: "adms.take-down", AccountID: id, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: "adms.take-down", AuthenticationStrength: "mfa", MFAAuthenticatedAt: &at, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "take-down-memory-admin-test-key", Issuer: "take-down-test", Audience: "admin", AccessTTL: time.Hour})
		sessions = identityservice.NewAdminSessionService(admins, sr, jwt)
		token, err := jwt.GenerateAccessToken(id, "Admin", auth.AccessTokenContext{SessionID: "adms.take-down", AuthVersion: 1})
		if err != nil {
			t.Fatal(err)
		}
		claims, err := jwt.VerifyAccessToken(token)
		if err != nil {
			t.Fatal(err)
		}
		proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
			return identityport.ResolveAdminSessionPrincipal(ctx, sessions, claims)
		}
		if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.take-down", AccountID: id, RoleID: "security_admin", GrantedBy: id}); err != nil {
			t.Fatal(err)
		}
		if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.take_down"}, "test"); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := grants.InsertDelegations(ctx, []identityrepo.Delegation{{ID: "grant.take-down", Kind: "grant", SubjectKind: "user", SubjectID: "190104", Action: "community.thread.take_down", BoardID: "190103", NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	s.SetGovernanceAuthority(identityservice.NewBoardGovernanceAuthorityService(grants, admins, sessions))
	return s, threads, gov, store, grants, proof
}

func TestThreadGovernanceTakeDownCurrentDomainAndPrivateAcknowledgement(t *testing.T) {
	for _, kind := range []string{"user", "admin"} {
		t.Run(kind, func(t *testing.T) {
			s, threads, gov, store, _, proof := takeDownFixture(t, kind)
			ctx := context.Background()
			before, _ := threads.GetByID(ctx, "190101")
			result, err := s.TakeDownForPrincipal(ctx, "190101", "required reason", proof)
			if err != nil || result == nil || result.ModerationStatus != "taken_down" {
				t.Fatal(result, err)
			}
			after, _ := threads.GetByID(ctx, "190101")
			p, _ := proof(ctx)
			if after.FactsVersion <= before.FactsVersion || after.CurrentRevision != before.CurrentRevision+1 || after.ModerationActorKind != kind || after.ModerationActorID != p.Actor.ID || after.Content != before.Content || after.PublicationStatus != before.PublicationStatus {
				t.Fatal(after)
			}
			if kind == "admin" && after.ModerationBy != "" || kind == "user" && after.ModerationBy != p.Actor.ID {
				t.Fatal("actor placed in wrong legacy domain", after)
			}
			actions, err := gov.ListModerationActions(ctx, after.ID)
			if err != nil || len(actions) != 1 || actions[0].ActorKind != kind || actions[0].ActorRefID != p.Actor.ID {
				t.Fatal(actions, err)
			}
			c, err := gov.LatestOpenCase(ctx, after.ID)
			if err != nil || c == nil || c.OpenedActorKind != kind || c.OpenedActorID != p.Actor.ID {
				t.Fatal(c, err)
			}
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 2 || fields["id"] != after.ID || strings.Contains(string(raw), before.Content) {
				t.Fatal("execute grant exposed private facts", string(raw))
			}
			records, _, err := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
			if err != nil || len(records) != 1 || records[0].ActorType != kind || records[0].ActorID != p.Actor.ID {
				t.Fatal(records, err)
			}
		})
	}
}

func TestThreadGovernanceTakeDownRevocationAndAtomicFailure(t *testing.T) {
	for _, mode := range []string{"grant_revoke", "grant_aba", "credential", "resource_aba", "audit", "history", "missing", "owner_without_grant", "trash"} {
		t.Run(mode, func(t *testing.T) {
			s, threads, gov, store, grants, proof := takeDownFixture(t, "user")
			ctx := context.Background()
			before, _ := threads.GetByID(ctx, "190101")
			historyBefore := gov.Snapshot()
			switch mode {
			case "audit":
				s.SetReliability(reliability.NewService(transaction.NewMemory(threads, gov, store), authorFailAudit{Store: store}))
			case "history":
				s.SetGovernanceRepository(takeDownFailHistory{ContentGovernanceRepository: gov})
			case "missing":
				s.SetGovernanceAuthority(nil)
			case "owner_without_grant":
				proof = authorUpdateProof(before.AuthorID)
			case "trash":
				row, _ := threads.GetByID(ctx, before.ID)
				row.DeletionStatus = "trashed"
				threads.Update(ctx, row)
			}
			calls := 0
			changing := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 3 {
					switch mode {
					case "grant_revoke":
						if _, _, err := grants.SetStatusCAS(ctx, "grant.take-down", 1, "revoked", time.Now()); err != nil {
							t.Fatal(err)
						}
					case "grant_aba":
						grants.SetStatusCAS(ctx, "grant.take-down", 1, "suspended", time.Now())
						grants.SetStatusCAS(ctx, "grant.take-down", 2, "active", time.Now())
					case "resource_aba":
						row, _ := threads.GetByID(ctx, before.ID)
						row.Content = "other"
						threads.Update(ctx, row)
						row.Content = before.Content
						threads.Update(ctx, row)
					}
				}
				if mode == "credential" && calls >= 3 {
					return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
				}
				return proof(ctx)
			}
			result, err := s.TakeDownForPrincipal(ctx, before.ID, "required reason", changing)
			if err == nil || result != nil {
				t.Fatal("failed command accepted", result, err)
			}
			after, _ := threads.GetByID(ctx, before.ID)
			if after.ModerationStatus == "taken_down" || after.ModerationActorID != "" || !reflect.DeepEqual(historyBefore, gov.Snapshot()) {
				t.Fatal("failed command persisted resource/history", after)
			}
		})
	}
}

type takeDownFailHistory struct {
	repository.ContentGovernanceRepository
}

func (takeDownFailHistory) CreateModerationAction(context.Context, *domain.ModerationAction) error {
	return errors.New("required history failure")
}
