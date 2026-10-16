package mutualaid

import (
	"context"
	"errors"
	"reflect"
	"testing"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	repo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	svc "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func typedCreateFixture(t *testing.T) (*Service, *repo.MemoryThreadRepository, *repo.MemoryCategoryRepository, *repo.MemoryThreadTypePolicyRepository, *reliability.MemoryStore, *svc.ThreadService) {
	t.Helper()
	ctx := context.Background()
	s, threads, events := newTestService(t, true)
	cats := repo.NewMemoryCategoryRepository()
	parent := "2"
	for _, c := range []*domain.Category{{ID: parent, NodeKind: "group", LifecycleStatus: "active", Version: 1}, {ID: "1", ParentID: &parent, NodeKind: "board", LifecycleStatus: "active", Version: 1}} {
		if err := cats.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	types := repo.NewMemoryThreadTypePolicyRepository()
	if err := types.Replace(ctx, "1", []domain.ThreadType{domain.ThreadTypeMutualAid}); err != nil {
		t.Fatal(err)
	}
	core := svc.NewThreadService(threads, nil)
	core.SetCategoryRepository(cats)
	core.SetThreadTypePolicyRepository(types)
	core.SetGovernanceRepository(repo.NewMemoryContentGovernanceRepository())
	durable := reliability.NewService(transaction.NewMemory(cats), events)
	core.SetReliability(durable)
	s.community = community.NewContentGateway(threads, core)
	s.SetReliability(durable)
	return s, threads, cats, types, events, core
}

type rawCreateGateway struct{ communityport.ContentGateway }
type createFailAudit struct{ reliability.Store }

func (s createFailAudit) RecordCommandAudit(context.Context, reliability.CommandAudit) error {
	return errors.New("required audit unavailable")
}

type createDriftStore struct {
	Store
	afterCreate func(context.Context, *Detail)
}

func (s *createDriftStore) Create(ctx context.Context, d *Detail) error {
	if err := s.Store.Create(ctx, d); err != nil {
		return err
	}
	if s.afterCreate != nil {
		s.afterCreate(ctx, d)
	}
	return nil
}

func TestMutualAidAuthorCreateCurrentFactsAndMissingPorts(t *testing.T) {
	ctx := context.Background()
	disabled := NewService(NewMemoryStore(), nil, nil)
	disabled.SetEnabledChecker(func() bool { return false })
	if _, err := disabled.Create(ctx, "1001", "Owner", validCreateRequest()); !errors.Is(err, ErrFeatureDisabled) {
		t.Fatal("disabled raw helper accessed missing gateway", err)
	}
	for _, mode := range []string{"current", "same-ID-admin", "nil-proof", "closed", "archived", "group", "type", "disabled", "missing-command", "missing-audit"} {
		t.Run(mode, func(t *testing.T) {
			s, threads, cats, types, events, _ := typedCreateFixture(t)
			proof := typedReadProof("1001")
			switch mode {
			case "same-ID-admin":
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, _ := typedReadProof("1001")(ctx)
					p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
					return p, nil
				}
			case "nil-proof":
				proof = nil
			case "closed", "archived":
				c, _ := cats.GetByID(ctx, "1")
				if mode == "closed" {
					c.IsClosed = true
				} else {
					c.LifecycleStatus = "archived"
				}
				if err := cats.Update(ctx, c); err != nil {
					t.Fatal(err)
				}
			case "group":
				c, _ := cats.GetByID(ctx, "2")
				c.LifecycleStatus = "archived"
				if err := cats.Update(ctx, c); err != nil {
					t.Fatal(err)
				}
			case "type":
				if err := types.Replace(ctx, "1", []domain.ThreadType{domain.ThreadTypeDiscussion}); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				s.SetEnabledChecker(func() bool { return false })
			case "missing-command":
				s.community = rawCreateGateway{s.community}
			case "missing-audit":
				s.SetReliability(nil)
			}
			before := threads.Snapshot()
			result, err := s.CreateForPrincipal(ctx, "Owner", validCreateRequest(), proof)
			if (err == nil) != (mode == "current") || (err != nil && result != nil) {
				t.Fatal(result, err)
			}
			if err != nil && !reflect.DeepEqual(before, threads.Snapshot()) {
				t.Fatal("denied create persisted base")
			}
			if result != nil {
				actual, err := s.store.Get(ctx, result.Thread.ID)
				if err != nil || !reflect.DeepEqual(actual, result.Detail) || actual.CreatedBy != "1001" || actual.Version != 1 || result.Thread.ThreadType != domain.ThreadTypeMutualAid {
					t.Fatal("typed binding", result, err)
				}
				audits, total, err := events.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
				if err != nil || total != 1 || len(audits) != 1 || audits[0].ActorID != "1001" || audits[0].ActorType != "user" {
					t.Fatal(audits, total, err)
				}
			}
		})
	}
}

func TestMutualAidAuthorCreateAuthorityAndDetailRollback(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"board", "type", "credential", "feature-disabled", "detail", "core-owner", "audit"} {
		t.Run(mode, func(t *testing.T) {
			s, threads, cats, types, events, core := typedCreateFixture(t)
			persisted := s.store.(transaction.Snapshotter)
			beforeThread, beforeDetail, beforeEvent := threads.Snapshot(), persisted.Snapshot(), events.Snapshot()
			proof := typedReadProof("1001")
			calls := 0
			bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				p, _ := proof(ctx)
				if calls == 3 {
					switch mode {
					case "board":
						c, _ := cats.GetByID(ctx, "1")
						c.Version++
						if err := cats.Update(ctx, c); err != nil {
							t.Fatal(err)
						}
					case "type":
						if err := types.Replace(ctx, "1", []domain.ThreadType{domain.ThreadTypeDiscussion}); err != nil {
							t.Fatal(err)
						}
					}
				}
				if calls >= 4 && mode == "credential" {
					p.CredentialID = "changed-session"
				}
				return p, nil
			}
			if mode == "feature-disabled" || mode == "detail" || mode == "core-owner" {
				s.store = &createDriftStore{Store: s.store, afterCreate: func(ctx context.Context, d *Detail) {
					switch mode {
					case "feature-disabled":
						s.SetEnabledChecker(func() bool { return false })
					case "detail":
						fresh, _ := s.store.Get(ctx, d.ThreadID)
						fresh.LocationScope = "changed in participant"
						if err := s.store.Update(ctx, fresh, 1); err != nil {
							t.Fatal(err)
						}
					case "core-owner":
						fresh, _ := threads.GetByID(ctx, d.ThreadID)
						fresh.AuthorID = "1002"
						if err := threads.Update(ctx, fresh); err != nil {
							t.Fatal(err)
						}
					}
				}}
			}
			if mode == "audit" {
				durable := reliability.NewService(transaction.NewMemory(cats, persisted, events), createFailAudit{events})
				core.SetReliability(durable)
				s.SetReliability(durable)
			}
			result, err := s.CreateForPrincipal(ctx, "Owner", validCreateRequest(), bound)
			if err == nil || result != nil {
				t.Fatal("stale typed create accepted", result, err)
			}
			if !reflect.DeepEqual(beforeThread, threads.Snapshot()) || !reflect.DeepEqual(beforeDetail, persisted.Snapshot()) || !reflect.DeepEqual(beforeEvent, events.Snapshot()) {
				t.Fatal("partial aggregate/audit/outbox survived")
			}
		})
	}
}
