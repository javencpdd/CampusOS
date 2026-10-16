package richtext

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	repo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	core "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func articleCreateFixture(t *testing.T) (*Service, *repo.MemoryThreadRepository, *repo.MemoryCategoryRepository, *repo.MemoryThreadTypePolicyRepository, *reliability.MemoryStore, *core.ThreadService) {
	t.Helper()
	ctx := context.Background()
	s, threads := newTestService(true)
	cats := repo.NewMemoryCategoryRepository()
	parent := "2"
	for _, c := range []*domain.Category{{ID: parent, NodeKind: "group", LifecycleStatus: "active", Version: 1}, {ID: "1", ParentID: &parent, NodeKind: "board", LifecycleStatus: "active", Version: 1}} {
		if err := cats.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	types := repo.NewMemoryThreadTypePolicyRepository()
	if err := types.Replace(ctx, "1", []domain.ThreadType{domain.ThreadTypeArticle}); err != nil {
		t.Fatal(err)
	}
	svc := core.NewThreadService(threads, nil)
	svc.SetCategoryRepository(cats)
	svc.SetThreadTypePolicyRepository(types)
	svc.SetGovernanceRepository(repo.NewMemoryContentGovernanceRepository())
	events := reliability.NewMemoryStore()
	durable := reliability.NewService(transaction.NewMemory(cats), events)
	svc.SetReliability(durable)
	s.community = community.NewContentGateway(threads, svc)
	s.SetReliability(durable)
	return s, threads, cats, types, events, svc
}
func articleCreateRequest() SaveArticleRequest {
	return SaveArticleRequest{Title: "Author draft", CategoryID: "1", ContentHTML: `<p>body</p><script>bad()</script>`, ContentJSON: json.RawMessage(`{"z":1e2,"a":9007199254740993}`)}
}
func articleCreateProof(context.Context) (identityport.PrincipalContext, error) {
	return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "1001"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "article-session"}, nil
}

type articleRawGateway struct{ communityport.ContentGateway }
type articleCreateDriftStore struct {
	Store
	afterCreate func(context.Context, *Article)
}

func (s *articleCreateDriftStore) CreateArticle(ctx context.Context, a *Article) error {
	if err := s.Store.CreateArticle(ctx, a); err != nil {
		return err
	}
	if s.afterCreate != nil {
		s.afterCreate(ctx, a)
	}
	return nil
}

type articleCreateFailAudit struct{ reliability.Store }

func (s articleCreateFailAudit) RecordCommandAudit(context.Context, reliability.CommandAudit) error {
	return errors.New("required audit unavailable")
}
func TestRichTextAuthorCreateCurrentFactsAndMissingPorts(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"current", "admin", "missing-proof", "missing-command", "missing-audit", "closed", "archived", "type", "group", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			s, threads, cats, types, _, _ := articleCreateFixture(t)
			var proof identityport.SessionPrincipalResolver = articleCreateProof
			switch mode {
			case "admin":
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, _ := articleCreateProof(ctx)
					p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
					return p, nil
				}
			case "missing-proof":
				proof = nil
			case "missing-command":
				s.community = articleRawGateway{s.community}
			case "missing-audit":
				s.SetReliability(nil)
			case "disabled":
				s.SetEnabledChecker(func() bool { return false })
			case "closed", "archived", "group":
				id := "1"
				if mode == "group" {
					id = "2"
				}
				c, _ := cats.GetByID(ctx, id)
				if mode == "closed" {
					c.IsClosed = true
				} else {
					c.LifecycleStatus = "archived"
				}
				if err := cats.Update(ctx, c); err != nil {
					t.Fatal(err)
				}
			case "type":
				if err := types.Replace(ctx, "1", []domain.ThreadType{domain.ThreadTypeDiscussion}); err != nil {
					t.Fatal(err)
				}
			}
			before := threads.Snapshot()
			result, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), proof)
			if (err == nil) != (mode == "current") || (err != nil && result != nil) {
				t.Fatal(result, err)
			}
			if err != nil && !reflect.DeepEqual(before, threads.Snapshot()) {
				t.Fatal("partial base persisted")
			}
			if result != nil {
				a, err := s.store.GetArticleByThreadID(ctx, result.ThreadID)
				if err != nil || a.CreatedBy != "1001" || a.UpdatedBy != "1001" || a.Status != StatusDraft || containsAny(a.SanitizedHTML, "script") {
					t.Fatal(a, err)
				}
			}
		})
	}
}
func TestRichTextAuthorCreateFactDriftAndAuditRollback(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"board", "credential", "article", "core", "disabled", "audit"} {
		t.Run(mode, func(t *testing.T) {
			s, threads, cats, _, events, core := articleCreateFixture(t)
			persisted := s.store.(transaction.Snapshotter)
			beforeThread, beforeArticle, beforeEvent := threads.Snapshot(), persisted.Snapshot(), events.Snapshot()
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				p, _ := articleCreateProof(ctx)
				if calls == 3 && mode == "board" {
					c, _ := cats.GetByID(ctx, "1")
					c.Version++
					if err := cats.Update(ctx, c); err != nil {
						t.Fatal(err)
					}
				}
				if calls >= 4 && mode == "credential" {
					p.CredentialID = "changed"
				}
				return p, nil
			}
			s.store = &articleCreateDriftStore{Store: s.store, afterCreate: func(ctx context.Context, a *Article) {
				switch mode {
				case "article":
					fresh, _ := s.store.GetArticleByThreadID(ctx, a.ThreadID)
					fresh.Summary = "changed"
					if err := s.store.UpdateArticle(ctx, fresh); err != nil {
						t.Fatal(err)
					}
				case "core":
					row, _ := threads.GetByID(ctx, a.ThreadID)
					row.Content = "changed"
					if err := threads.Update(ctx, row); err != nil {
						t.Fatal(err)
					}
				case "disabled":
					s.SetEnabledChecker(func() bool { return false })
				}
			}}
			if mode == "audit" {
				d := reliability.NewService(transaction.NewMemory(cats, persisted, events), articleCreateFailAudit{events})
				core.SetReliability(d)
				s.SetReliability(d)
			}
			result, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), proof)
			if err == nil || result != nil || !reflect.DeepEqual(beforeThread, threads.Snapshot()) || !reflect.DeepEqual(beforeArticle, persisted.Snapshot()) || !reflect.DeepEqual(beforeEvent, events.Snapshot()) {
				t.Fatal("aggregate/audit rollback failed", result, err)
			}
		})
	}
}
