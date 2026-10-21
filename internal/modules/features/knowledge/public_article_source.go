package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/campusos/CampusOS/internal/modules/features/richtext"
)

const (
	PublicArticleKind           = "richtext.article"
	PortPublicArticleCandidates = "knowledge.public-article-candidates"
)

// Articles share the bounded candidate shape; their source kind and revision
// domain are distinct from Community discussion text references.
type PublicArticleCandidate = PublicThreadCandidate
type PublicArticleCandidates interface {
	PreviewPublicArticle(context.Context, string) (PublicArticleCandidate, error)
	RecheckPublicArticle(context.Context, SourceReference) (PublicArticleCandidate, error)
}
type PublicArticleSourceService struct {
	reader  richtext.PublicTextSourceReader
	enabled func() bool
}

func NewPublicArticleSourceService(reader richtext.PublicTextSourceReader, enabled func() bool) (*PublicArticleSourceService, error) {
	if nilSourceAdapter(reader) || enabled == nil {
		return nil, ErrSourceUnavailable
	}
	return &PublicArticleSourceService{reader: reader, enabled: enabled}, nil
}
func (s *PublicArticleSourceService) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilSourceAdapter(s.reader) || s.enabled == nil {
		return ErrSourceUnavailable
	}
	if !s.enabled() {
		return ErrDisabled
	}
	return nil
}
func (s *PublicArticleSourceService) read(ctx context.Context, id string) (PublicArticleCandidate, error) {
	var empty PublicArticleCandidate
	if err := s.check(ctx); err != nil {
		return empty, err
	}
	source, err := s.reader.ReadPublicTextSource(ctx, id)
	if e := s.check(ctx); e != nil {
		return empty, e
	}
	if errors.Is(err, richtext.ErrPublicTextTooLarge) {
		return empty, ErrSourceTooLarge
	}
	if errors.Is(err, richtext.ErrPublicTextChanged) {
		return empty, ErrSourceChanged
	}
	if err != nil {
		return empty, ErrSourceUnavailable
	}
	if source.ThreadID != id || !sourceID(source.ArticleID) || !sourceID(source.CategoryID) || !validReferenceFor(SourceReference{Kind: PublicArticleKind, ID: id, Revision: source.Revision}, PublicArticleKind) {
		return empty, ErrSourceUnavailable
	}
	if len(source.Title) > MaxSourceTitleBytes || len(source.Text) > MaxSourceBodyBytes {
		return empty, ErrSourceTooLarge
	}
	if source.Title == "" || source.Text == "" || !utf8.ValidString(source.Title) || !utf8.ValidString(source.Text) {
		return empty, ErrSourceUnavailable
	}
	bound := struct {
		Contract string
		Source   richtext.PublicTextSource
	}{"campusos.knowledge.public-article-candidate/v1", source}
	wire, err := json.Marshal(bound)
	if err != nil {
		return empty, ErrSourceUnavailable
	}
	h := sha256.Sum256(wire)
	return PublicArticleCandidate{Reference: SourceReference{Kind: PublicArticleKind, ID: id, Revision: hex.EncodeToString(h[:])}, Title: source.Title, Content: source.Text, Format: "text", CategoryID: source.CategoryID, URL: "/threads/" + id}, nil
}
func (s *PublicArticleSourceService) PreviewPublicArticle(ctx context.Context, id string) (PublicArticleCandidate, error) {
	var empty PublicArticleCandidate
	if !sourceID(id) {
		return empty, ErrInvalidReference
	}
	first, err := s.read(ctx, id)
	if err != nil {
		return empty, err
	}
	current, err := s.read(ctx, id)
	if err != nil {
		return empty, err
	}
	if first != current {
		return empty, ErrSourceChanged
	}
	return current, nil
}
func (s *PublicArticleSourceService) RecheckPublicArticle(ctx context.Context, ref SourceReference) (PublicArticleCandidate, error) {
	var empty PublicArticleCandidate
	if !validReferenceFor(ref, PublicArticleKind) {
		return empty, ErrInvalidReference
	}
	current, err := s.PreviewPublicArticle(ctx, ref.ID)
	if err != nil {
		return empty, err
	}
	if current.Reference != ref {
		return empty, ErrSourceChanged
	}
	return current, nil
}
