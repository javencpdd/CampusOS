package richtext

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

const (
	PortPublicTextSource = "richtext.public-text-source"
	MaxPublicTextBytes   = 64 * 1024
	MaxPublicTitleBytes  = 1024
)

var (
	ErrPublicTextUnavailable = errors.New("public article text unavailable")
	ErrPublicTextChanged     = errors.New("public article text changed")
	ErrPublicTextTooLarge    = errors.New("public article text exceeds source limit")
)

// PublicTextSource is a trusted Built-in Feature projection, not an HTTP or
// plugin API. It contains no raw HTML/JSON, author, attachment, or asset URL.
// Revision binds BOTH the owning article and Community's non-reused epochs.
type PublicTextSource struct {
	ThreadID, ArticleID, CategoryID, Title, Text, Revision string
}

type PublicTextSourceReader interface {
	ReadPublicTextSource(context.Context, string) (PublicTextSource, error)
}

type publicTextSourceReader struct{ service *Service }

func (s *Service) PublicTextSources() PublicTextSourceReader {
	return publicTextSourceReader{service: s}
}

func publicTextID(id string) bool {
	n, e := strconv.ParseInt(id, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == id
}

func (r publicTextSourceReader) ReadPublicTextSource(ctx context.Context, id string) (PublicTextSource, error) {
	var empty PublicTextSource
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if r.service == nil || !publicTextID(id) {
		return empty, ErrPublicTextUnavailable
	}
	s := r.service
	result, err := identityport.ReadBoundProjection(ctx, nil, true,
		func(ctx context.Context, p identityport.PrincipalContext) (articleReadSnapshot, error) {
			// nil resolver is deliberately anonymous, even if the caller's
			// context carries an owner/Admin proof. No owner fallback exists.
			snap, e := s.articleReadSnapshot(ctx, p, id)
			if e != nil {
				return articleReadSnapshot{}, e
			}
			if snap.Publication != domain.PublicationStatusPublished || snap.Moderation != domain.ModerationStatusClear || snap.Deletion != domain.DeletionStatusActive ||
				snap.Version < 1 || snap.Article == nil || snap.Article.FactsVersion < 1 || !publicTextID(snap.Article.ID) || !publicTextID(snap.CategoryID) {
				return articleReadSnapshot{}, ErrPublicTextUnavailable
			}
			return snap, nil
		}, func(_ context.Context, _ identityport.PrincipalContext, snap articleReadSnapshot) (PublicTextSource, error) {
			a := snap.Article
			if len(a.ContentHTML) > MaxHTMLBytes || len(a.Title) > MaxPublicTitleBytes {
				return empty, ErrPublicTextTooLarge
			}
			if a.Title == "" || !utf8.ValidString(a.Title) || !utf8.ValidString(a.ContentHTML) {
				return empty, ErrPublicTextUnavailable
			}
			// Use the owning editor's bounded sanitizer/Text conversion. The
			// Community excerpt and stored render/sanitized cache are not the
			// original article body. No DOM execution, URL fetch or asset IO.
			clean, e := Sanitize(a.ContentHTML)
			if e != nil {
				return empty, ErrPublicTextUnavailable
			}
			if len(clean.Text) > MaxPublicTextBytes {
				return empty, ErrPublicTextTooLarge
			}
			if clean.Text == "" || !utf8.ValidString(clean.Text) {
				return empty, ErrPublicTextUnavailable
			}
			bound := struct {
				Contract, ThreadID, ArticleID, CategoryID, Title, Text string
				ThreadVersion, ArticleVersion                          int64
			}{"campusos.richtext.public-text-source/v1", id, a.ID, snap.CategoryID, a.Title, clean.Text, snap.Version, a.FactsVersion}
			wire, e := json.Marshal(bound)
			if e != nil {
				return empty, ErrPublicTextUnavailable
			}
			h := sha256.Sum256(wire)
			return PublicTextSource{ThreadID: id, ArticleID: a.ID, CategoryID: snap.CategoryID, Title: a.Title, Text: clean.Text, Revision: hex.EncodeToString(h[:])}, nil
		}, nil)
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if errors.Is(err, ErrPublicTextTooLarge) {
		return empty, ErrPublicTextTooLarge
	}
	if errors.Is(err, identityport.ErrPolicyFactsChanged) {
		return empty, ErrPublicTextChanged
	}
	if err != nil || s.ensureEnabled() != nil {
		return empty, ErrPublicTextUnavailable
	}
	return result, nil
}

type modulePublicTextSource struct{ module *Module }

func (r modulePublicTextSource) ReadPublicTextSource(ctx context.Context, id string) (PublicTextSource, error) {
	if r.module == nil || r.module.service == nil {
		return PublicTextSource{}, ErrPublicTextUnavailable
	}
	return r.module.service.PublicTextSources().ReadPublicTextSource(ctx, id)
}
