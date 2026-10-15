package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type authorCommandPart struct {
	load  func(context.Context, *domain.Thread, bool) (any, error)
	apply func(context.Context, *domain.Thread) error
	check func(context.Context, *domain.Thread) error
}

func (p *authorCommandPart) ThreadType() domain.ThreadType { return domain.ThreadTypeArticle }
func (p *authorCommandPart) LoadAuthorFacts(ctx context.Context, t *domain.Thread, lock bool) (any, error) {
	return p.load(ctx, t, lock)
}
func (p *authorCommandPart) ApplyAuthorCommand(ctx context.Context, t *domain.Thread) error {
	if p.apply != nil {
		return p.apply(ctx, t)
	}
	return nil
}
func (p *authorCommandPart) CheckAuthorResult(ctx context.Context, t *domain.Thread) error {
	if p.check != nil {
		return p.check(ctx, t)
	}
	return nil
}
func (p *authorCommandPart) AuthorEventPayload() any { return nil }
func TestFeatureAuthorCommandRejectsMissingFactsAndSubjects(t *testing.T) {
	for _, mode := range []string{"missing-facts", "typed-nil-facts", "other", "admin", "missing-proof", "unknown-operation", "typed-nil", "missing-audit"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, repo, _, _ := authorUpdateFixture(t)
			row, _ := repo.GetByID(ctx, "190101")
			row.ThreadType = domain.ThreadTypeArticle
			if err := repo.Update(ctx, row); err != nil {
				t.Fatal(err)
			}
			var part communityport.ThreadAuthorParticipant = &authorCommandPart{load: func(context.Context, *domain.Thread, bool) (any, error) {
				if mode == "typed-nil-facts" {
					return (*domain.Thread)(nil), nil
				}
				return nil, nil
			}}
			proof := authorUpdateProof("190102")
			operation := "edit"
			switch mode {
			case "other":
				proof = authorUpdateProof("190104")
			case "admin":
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, _ := authorUpdateProof("190102")(ctx)
					p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
					return p, nil
				}
			case "missing-proof":
				proof = nil
			case "unknown-operation":
				operation = "made-up"
			case "typed-nil":
				part = (*authorCommandPart)(nil)
			case "missing-audit":
				s.SetReliability(nil)
			}
			before := repo.Snapshot()
			err := s.ExecuteThreadAuthorCommand(ctx, row.ID, operation, "feature.test.update", "test.updated", proof, part)
			if err == nil || !reflect.DeepEqual(before, repo.Snapshot()) {
				t.Fatal("missing facts or subject was accepted", err)
			}
		})
	}
}
func TestFeatureAuthorCommandRequiredAuditAndFinalCheck(t *testing.T) {
	for _, mode := range []string{"current", "final-check", "audit"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, repo, gov, events := authorUpdateFixture(t)
			row, _ := repo.GetByID(ctx, "190101")
			row.ThreadType = domain.ThreadTypeArticle
			if err := repo.Update(ctx, row); err != nil {
				t.Fatal(err)
			}
			before := repo.Snapshot()
			part := &authorCommandPart{load: func(ctx context.Context, t *domain.Thread, lock bool) (any, error) {
				current, err := repo.GetByID(ctx, t.ID)
				if err != nil {
					return nil, err
				}
				return current.Content, nil
			}, apply: func(ctx context.Context, t *domain.Thread) error {
				t.Content = "checked author body"
				_, err := s.SaveFeatureThread(ctx, t, "190102", "typed-update")
				return err
			}, check: func(ctx context.Context, t *domain.Thread) error {
				if mode == "final-check" {
					return identityport.ErrPolicyFactsChanged
				}
				return nil
			}}
			if mode == "audit" {
				s.SetReliability(reliability.NewService(transaction.NewMemory(repo, gov, events), authorFailAudit{events}))
			}
			err := s.ExecuteThreadAuthorCommand(ctx, row.ID, "edit", "feature.test.update", "test.updated", authorUpdateProof("190102"), part)
			if (err == nil) != (mode == "current") {
				t.Fatal(err)
			}
			if err != nil && !reflect.DeepEqual(before, repo.Snapshot()) {
				t.Fatal("required audit/final check failed to roll back")
			}
		})
	}
}
