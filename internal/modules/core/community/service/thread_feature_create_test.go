package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type featureCreateParticipant struct {
	kind    domain.ThreadType
	persist func(context.Context, *domain.Thread) error
}

func (p *featureCreateParticipant) ThreadType() domain.ThreadType { return p.kind }
func (p *featureCreateParticipant) PersistThreadDetail(ctx context.Context, t *domain.Thread) error {
	if p.persist != nil {
		return p.persist(ctx, t)
	}
	return nil
}

func TestFeatureAuthorCreateExplicitParticipantContract(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"current", "empty-type", "unknown-type", "discussion", "wrong-type", "nil", "typed-nil", "missing-proof", "unknown-status", "missing-audit", "missing-governance"} {
		t.Run(mode, func(t *testing.T) {
			s, _, types := authorCreateFixture(t)
			if err := types.Replace(ctx, "190103", []domain.ThreadType{domain.ThreadTypeArticle}); err != nil {
				t.Fatal(err)
			}
			opts := CreateThreadOptions{ThreadType: domain.ThreadTypeArticle, Status: domain.ThreadStatusDraft, ContentFormat: "safe_html"}
			var part StructuredThreadParticipant = &featureCreateParticipant{kind: domain.ThreadTypeArticle}
			proof := authorUpdateProof("190102")
			switch mode {
			case "empty-type":
				opts.ThreadType = ""
			case "unknown-type":
				opts.ThreadType = "made-up"
			case "discussion":
				opts.ThreadType = domain.ThreadTypeDiscussion
			case "wrong-type":
				part = &featureCreateParticipant{kind: domain.ThreadTypeSecondhand}
			case "nil":
				part = nil
			case "typed-nil":
				part = (*featureCreateParticipant)(nil)
			case "missing-proof":
				proof = nil
			case "unknown-status":
				opts.Status = "made-up"
			case "missing-audit":
				s.SetReliability(nil)
			case "missing-governance":
				s.SetGovernanceRepository(nil)
			}
			before := s.repo.(transaction.Snapshotter).Snapshot()
			result, err := s.CreateStructuredThreadForPrincipal(ctx, "Owner", domain.CreateThreadRequest{CategoryID: "190103", Title: "article", Content: "<p>body</p>"}, opts, part, proof)
			if (err == nil) != (mode == "current") || (err != nil && result != nil) {
				t.Fatal(result, err)
			}
			if result != nil && (result.ThreadType != domain.ThreadTypeArticle || result.AuthorID != "190102" || result.PublicationStatus != "draft") {
				t.Fatal(result)
			}
			if err != nil && !reflect.DeepEqual(before, s.repo.(transaction.Snapshotter).Snapshot()) {
				t.Fatal("rejected create persisted")
			}
		})
	}
}
func TestFeatureAuthorCreateRejectsParticipantBaseMutation(t *testing.T) {
	s, _, types := authorCreateFixture(t)
	ctx := context.Background()
	if err := types.Replace(ctx, "190103", []domain.ThreadType{domain.ThreadTypeArticle}); err != nil {
		t.Fatal(err)
	}
	before := s.repo.(transaction.Snapshotter).Snapshot()
	part := &featureCreateParticipant{kind: domain.ThreadTypeArticle, persist: func(ctx context.Context, row *domain.Thread) error {
		current, err := s.repo.GetByID(ctx, row.ID)
		if err != nil {
			return err
		}
		current.Content = "changed after insert"
		return s.repo.Update(ctx, current)
	}}
	result, err := s.CreateStructuredThreadForPrincipal(ctx, "Owner", domain.CreateThreadRequest{CategoryID: "190103", Title: "article", Content: "body"}, CreateThreadOptions{ThreadType: domain.ThreadTypeArticle, Status: domain.ThreadStatusDraft}, part, authorUpdateProof("190102"))
	if err == nil || result != nil || !reflect.DeepEqual(before, s.repo.(transaction.Snapshotter).Snapshot()) {
		t.Fatal("participant mutation accepted", result, err)
	}
}
