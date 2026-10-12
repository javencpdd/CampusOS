package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func authorUpdateProof(owner string) identityport.SessionPrincipalResolver {
	return func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: owner}, Audience: "user", AuthenticationStrength: "password", CredentialID: "verified-author-session"}, nil
	}
}
func authorUpdateFixture(t *testing.T) (*ThreadService, *repository.MemoryThreadRepository, *repository.MemoryContentGovernanceRepository, *reliability.MemoryStore) {
	t.Helper()
	repo := repository.NewMemoryThreadRepository()
	row := &domain.Thread{ID: "190101", AuthorID: "190102", CategoryID: "190103", Title: "before", Content: "before-body", PublicationStatus: "private", ModerationStatus: "clear", DeletionStatus: "active"}
	if err := repo.Create(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	gov := repository.NewMemoryContentGovernanceRepository()
	store := reliability.NewMemoryStore()
	s := NewThreadService(repo, nil)
	s.SetGovernanceRepository(gov)
	s.SetReliability(reliability.NewService(transaction.NewMemory(repo, gov, store), store))
	return s, repo, gov, store
}
func TestAuthorUpdateCurrentPolicyAndDomain(t *testing.T) {
	for _, mode := range []string{"owner", "other", "admin", "missing", "trash", "invalid-state"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, repo, _, _ := authorUpdateFixture(t)
			resolve := authorUpdateProof("190102")
			switch mode {
			case "other":
				resolve = authorUpdateProof("190104")
			case "admin":
				resolve = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, _ := authorUpdateProof("190102")(ctx)
					p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
					return p, nil
				}
			case "missing":
				resolve = nil
			case "trash", "invalid-state":
				row, _ := repo.GetByID(ctx, "190101")
				if mode == "trash" {
					row.DeletionStatus = "trashed"
				} else {
					row.ModerationStatus = "unknown"
				}
				repo.Update(ctx, row)
			}
			title := "after"
			result, err := s.UpdateThreadForPrincipal(ctx, "190101", domain.UpdateThreadRequest{Title: &title}, resolve)
			if (err == nil) != (mode == "owner") || (err != nil && result != nil) {
				t.Fatalf("result=%v err=%v", result, err)
			}
			stored, _ := repo.GetByID(ctx, "190101")
			if mode != "owner" && stored.Title != "before" {
				t.Fatal("denied mutation persisted")
			}
		})
	}
}

type authorFailAudit struct{ reliability.Store }

func (s authorFailAudit) RecordCommandAudit(context.Context, reliability.CommandAudit) error {
	return errors.New("required audit failed")
}
func TestAuthorUpdateRevocationAndRequiredAuditRollback(t *testing.T) {
	for _, mode := range []string{"owner-race", "board-race", "session", "credential", "audit"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, repo, gov, store := authorUpdateFixture(t)
			beforeAudit := gov.Snapshot()
			calls := 0
			resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, _ := authorUpdateProof("190102")(ctx)
				calls++
				if calls == 2 && (mode == "owner-race" || mode == "board-race") {
					row, _ := repo.GetByID(ctx, "190101")
					if mode == "owner-race" {
						row.AuthorID = "190104"
					} else {
						row.CategoryID = "190105"
					}
					repo.Update(ctx, row)
				}
				if calls >= 3 && mode == "session" {
					return p, identityport.ErrUserSessionPrincipalUnavailable
				}
				if calls >= 3 && mode == "credential" {
					p.CredentialID = "replacement"
				}
				return p, nil
			}
			if mode == "audit" {
				s.SetReliability(reliability.NewService(transaction.NewMemory(repo, gov, store), authorFailAudit{Store: store}))
			}
			title := "must-not-commit"
			result, err := s.UpdateThreadForPrincipal(ctx, "190101", domain.UpdateThreadRequest{Title: &title}, resolve)
			if err == nil || result != nil {
				t.Fatalf("stale result=%v err=%v", result, err)
			}
			stored, _ := repo.GetByID(ctx, "190101")
			if stored.Title != "before" || stored.CurrentRevision != 0 {
				t.Fatal("partial mutation remained after failure")
			}
			if !reflect.DeepEqual(beforeAudit, gov.Snapshot()) {
				t.Fatal("partial content audit remained after failed command")
			}
		})
	}
}

func TestAuthorUpdateSanitizesStructuredBodyAndTypedBoundary(t *testing.T) {
	ctx := context.Background()
	s, repo, _, _ := authorUpdateFixture(t)
	row, _ := repo.GetByID(ctx, "190101")
	row.ContentFormat = "safe_html"
	row.Content = "<p>before</p>"
	repo.Update(ctx, row)
	body := `<p>safe text</p><img src="/api/v1/content/assets/images/190102/known.png" onerror="bad()"><script>bad()</script>`
	result, err := s.UpdateThreadForPrincipal(ctx, row.ID, domain.UpdateThreadRequest{Content: &body}, authorUpdateProof("190102"))
	if err != nil || strings.Contains(result.Content, "script") || strings.Contains(result.Content, "onerror") || !strings.Contains(result.Content, "safe text") {
		t.Fatal(result, err)
	}
	bad := domain.ThreadStatus("unexpected")
	if _, err := s.UpdateThreadForPrincipal(ctx, row.ID, domain.UpdateThreadRequest{Status: &bad}, authorUpdateProof("190102")); !errors.Is(err, ErrThreadAuthorWriteInvalid) {
		t.Fatal(err)
	}
	row, _ = repo.GetByID(ctx, row.ID)
	row.ThreadType = domain.ThreadTypeArticle
	repo.Update(ctx, row)
	title := "must use typed editor"
	if _, err := s.UpdateThreadForPrincipal(ctx, row.ID, domain.UpdateThreadRequest{Title: &title}, authorUpdateProof("190102")); !errors.Is(err, ErrThreadAuthorWriteDenied) {
		t.Fatal(err)
	}
}
