package server

import (
	"context"
	"errors"
	corecommunity "github.com/campusos/CampusOS/internal/modules/core/community"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	coreeditor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/modules/features/personaldocuments"
	"github.com/jackc/pgx/v5"
	"os"
	"path/filepath"
	"reflect"
)

type privateIndexSourceAdapter struct {
	documents        *personaldocuments.Module
	community        *corecommunity.Module
	documentsEnabled func() bool
}

func (a privateIndexSourceAdapter) ReadPrivateSource(ctx context.Context, resolve identityport.SessionPrincipalResolver, kind, id string, body bool) (knowledge.PrivateSource, error) {
	var out knowledge.PrivateSource
	p, e := resolve(ctx)
	if e != nil {
		return out, e
	}
	if p.Validate() != nil || p.Actor.Kind != "user" || p.Audience != "user" {
		return out, identityport.ErrUserSessionPrincipalUnavailable
	}
	out.Kind, out.ID = kind, id
	if kind == "personal_document" {
		if a.documentsEnabled == nil || !a.documentsEnabled() {
			return out, knowledge.ErrDisabled
		}
		if a.documents == nil || a.documents.Service() == nil {
			return out, knowledge.ErrPrivateUnavailable
		}
		s := a.documents.Service()
		d, e := s.PrivateIndexMetadataForPrincipal(ctx, resolve, id)
		if e != nil {
			return out, mapPrivateSourceError(e)
		}
		if d.Status != personaldocuments.StatusActive || d.CurrentVersion == nil {
			return out, knowledge.ErrPrivateSourceMissing
		}
		out.Format, out.Title = d.Format, d.Name
		if d.CurrentVersion.SizeBytes > knowledge.MaxSourceBodyBytes {
			return out, knowledge.ErrSourceTooLarge
		}
		out.Revision, e = personaldocuments.PrivateIndexDocumentRevision(d)
		if e != nil {
			return out, knowledge.ErrPrivateUnavailable
		}
		if body {
			if d.CurrentVersion.SizeBytes > knowledge.MaxSourceBodyBytes {
				return out, knowledge.ErrSourceTooLarge
			}
			if d.Format != "text" && d.Format != "markdown" && d.Format != "campusdoc" {
				return out, knowledge.ErrUnsupportedSource
			}
			current, raw, e := s.TextContentForPrincipal(ctx, resolve, id, d.CurrentVersionID)
			if e != nil {
				return out, mapPrivateSourceError(e)
			}
			revision, e := personaldocuments.PrivateIndexDocumentRevision(current)
			if e != nil || revision != out.Revision {
				return out, knowledge.ErrSourceChanged
			}
			rendered, e := coreeditor.RenderDocument(out.Format, raw)
			if e != nil {
				return out, knowledge.ErrUnsupportedSource
			}
			out.Text = rendered.Text
		}
		if !a.documentsEnabled() {
			return knowledge.PrivateSource{}, knowledge.ErrDisabled
		}
	} else if kind == "community_thread" {
		if a.community == nil || a.community.ThreadService() == nil {
			return out, knowledge.ErrPrivateUnavailable
		}
		facts := a.community.PrivateIndexReader()
		if facts == nil {
			return out, knowledge.ErrPrivateUnavailable
		}
		f, e := facts.PrivateIndexFacts(ctx, id)
		if e != nil {
			if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, communityport.ErrThreadNotFound) {
				return out, knowledge.ErrPrivateSourceMissing
			}
			return out, knowledge.ErrPrivateUnavailable
		}
		if f.Owner != p.Actor.ID || !f.Available {
			return out, knowledge.ErrPrivateSourceMissing
		}
		if f.Bytes > knowledge.MaxSourceBodyBytes {
			return out, knowledge.ErrSourceTooLarge
		}
		thread, e := facts.ReadOwnPrivateIndexThread(ctx, id, resolve)
		if e != nil {
			return out, mapPrivateSourceError(e)
		}
		if thread.AuthorID != p.Actor.ID || thread.FactsVersion != f.Epoch || thread.ContentFormat != f.Format {
			return out, knowledge.ErrSourceChanged
		}
		out.Revision, out.Format, out.Title = f.Revision, f.Format, thread.Title
		if body {
			if out.Format != "text" && out.Format != "markdown" {
				return out, knowledge.ErrUnsupportedSource
			}
			rendered, e := coreeditor.RenderDocument(out.Format, thread.Content)
			if e != nil {
				return out, knowledge.ErrUnsupportedSource
			}
			out.Text = rendered.Text
		}
		after, e := facts.PrivateIndexFacts(ctx, id)
		if e != nil || after.Revision != out.Revision {
			return knowledge.PrivateSource{}, knowledge.ErrSourceChanged
		}
	} else {
		return out, knowledge.ErrInvalid
	}
	final, e := resolve(ctx)
	if e != nil {
		return knowledge.PrivateSource{}, e
	}
	if !reflect.DeepEqual(final, p) {
		return knowledge.PrivateSource{}, identityport.ErrPolicyFactsChanged
	}
	return out, ctx.Err()
}
func (a privateIndexSourceAdapter) CurrentPrivateSource(ctx context.Context, owner, kind, id, revision string) (bool, error) {
	if kind == "personal_document" {
		if a.documentsEnabled == nil || !a.documentsEnabled() {
			return false, knowledge.ErrDisabled
		}
		if a.documents == nil || a.documents.Service() == nil {
			return false, knowledge.ErrPrivateUnavailable
		}
		r, e := a.documents.Service().PrivateIndexRevision(ctx, owner, id)
		if errors.Is(e, personaldocuments.ErrNotFound) {
			return false, nil
		}
		return r == revision, e
	}
	if kind == "community_thread" {
		if a.community == nil {
			return false, knowledge.ErrPrivateUnavailable
		}
		facts := a.community.PrivateIndexReader()
		if facts == nil {
			return false, knowledge.ErrPrivateUnavailable
		}
		f, e := facts.PrivateIndexFacts(ctx, id)
		if errors.Is(e, communityport.ErrThreadNotFound) {
			return false, nil
		}
		if e != nil { // No rows is absence, infrastructure faults remain retries.
			if errors.Is(e, pgx.ErrNoRows) {
				return false, nil
			}
			return false, e
		}
		return f.Owner == owner && f.Available && f.Revision == revision, nil
	}
	return false, knowledge.ErrInvalid
}
func mapPrivateSourceError(e error) error {
	if errors.Is(e, identityport.ErrUserSessionPrincipalUnavailable) || errors.Is(e, identityport.ErrPolicyFactsChanged) || errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return e
	}
	if errors.Is(e, personaldocuments.ErrNotFound) || errors.Is(e, communityport.ErrThreadNotFound) {
		return knowledge.ErrPrivateSourceMissing
	}
	return knowledge.ErrPrivateUnavailable
}
func privateIndexVaultFromEnvironment() *knowledge.PrivateVault {
	path := os.Getenv("KNOWLEDGE_PRIVATE_KEY_DIR")
	if path == "" {
		return nil
	}
	wd, e := os.Getwd()
	if e != nil {
		return nil
	}
	excluded := []string{filepath.Join(wd, "data"), filepath.Join(wd, "backups"), filepath.Join(wd, "modules"), filepath.Join(wd, "migrations")}
	for _, name := range []string{"PLUGINS_DIR", "PLUGIN_DATA_DIR", "MODULES_DIR", "MODULE_DATA_DIR", "RESOURCE_DIR"} {
		if value := os.Getenv(name); value != "" {
			excluded = append(excluded, value)
		}
	}
	v, e := knowledge.NewPrivateVault(path, excluded...)
	if e != nil {
		return nil
	}
	return v
}
