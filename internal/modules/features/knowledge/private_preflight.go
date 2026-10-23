package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync/atomic"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

var (
	ErrPrivateSourceMissing = errors.New("private knowledge source not found")
	ErrPrivateUnavailable   = errors.New("private knowledge preflight unavailable")
)

// PrivateDocumentMetadataReader is an in-process, composition-root adapter to
// PersonalDocuments' checked owner reader. It cannot open bytes or enumerate
// documents, and is never published to a plugin, Integration or MCP endpoint.
type PrivateDocumentMetadataReader interface {
	GetOwnDocumentMetadata(context.Context, identityport.SessionPrincipalResolver, string) (PrivateDocumentMetadata, error)
}

type PrivateDocumentMetadata struct {
	DocumentID string
	VersionID  string
	Format     string
}

type PrivateSourcePreflightRequest struct {
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
}

// This is an eligibility explanation, never consent or an indexing receipt.
// No protected private storage/parser/job path has been accepted, so CanIndex
// always remains false. Enabling the public Knowledge Feature cannot change it.
type PrivateSourcePreflightResult struct {
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
	Format     string `json:"format"`
	CanIndex   bool   `json:"can_index"`
	Reason     string `json:"reason"`
}

type privateSourcePreflight struct {
	reader  PrivateDocumentMetadataReader
	enabled func() bool
	stopped atomic.Bool
}

func privateUser(ctx context.Context, resolve identityport.SessionPrincipalResolver) (identityport.PrincipalContext, error) {
	if err := ctx.Err(); err != nil {
		return identityport.PrincipalContext{}, err
	}
	if resolve == nil {
		return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	p, err := resolve(ctx)
	if err != nil {
		return identityport.PrincipalContext{}, err
	}
	if p.Validate() != nil || p.Actor.Kind != "user" || p.Audience != "user" {
		return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	return p, nil
}

func (s *privateSourcePreflight) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.enabled == nil || nilSourceAdapter(s.reader) || s.stopped.Load() {
		return ErrPrivateUnavailable
	}
	if !s.enabled() {
		return ErrDisabled
	}
	return nil
}

func (s *privateSourcePreflight) document(ctx context.Context, req PrivateSourcePreflightRequest, resolve identityport.SessionPrincipalResolver) (PrivateSourcePreflightResult, error) {
	empty := PrivateSourcePreflightResult{}
	if err := s.check(ctx); err != nil {
		return empty, err
	}
	p, err := privateUser(ctx, resolve)
	if err != nil {
		return empty, err
	}
	if !sourceID(req.DocumentID) || !sourceID(req.VersionID) {
		return empty, ErrInvalid
	}
	// Do not permit a resolver to switch owner or credential during a read.
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		current, err := privateUser(ctx, resolve)
		if err != nil {
			return identityport.PrincipalContext{}, err
		}
		if !reflect.DeepEqual(p, current) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	metadata, err := s.reader.GetOwnDocumentMetadata(ctx, bound, req.DocumentID)
	if err != nil {
		return empty, err
	}
	if metadata.DocumentID != req.DocumentID || metadata.VersionID != req.VersionID {
		return empty, ErrSourceChanged
	}
	// Known upload formats are not a claim that a private parser is available.
	reason := "private_storage_unavailable"
	switch metadata.Format {
	case "text", "markdown", "campusdoc":
	case "pdf", "docx":
		reason = "unsupported_format"
	default:
		return empty, ErrPrivateUnavailable
	}
	current, err := s.reader.GetOwnDocumentMetadata(ctx, bound, req.DocumentID)
	if err != nil {
		return empty, err
	}
	if metadata != current {
		return empty, ErrSourceChanged
	}
	if _, err = bound(ctx); err != nil {
		return empty, err
	}
	if err = s.check(ctx); err != nil {
		return empty, err
	}
	return PrivateSourcePreflightResult{DocumentID: req.DocumentID, VersionID: req.VersionID, Format: metadata.Format, CanIndex: false, Reason: reason}, nil
}

func (h *Handler) PrivateSourcePreflight(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if h == nil || h.privateSources == nil {
		knowledgeError(c, ErrPrivateUnavailable)
		return
	}
	const maxBody = 1024
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBody+1))
	var req PrivateSourcePreflightRequest
	if err != nil || c.Request.URL.RawQuery != "" || len(raw) > maxBody ||
		identityport.ValidateClosedHTTPObject(raw, []string{"document_id", "version_id"}, maxBody) != nil || json.Unmarshal(raw, &req) != nil {
		knowledgeError(c, ErrInvalid)
		return
	}
	claims := &auth.JWTClaims{UserID: c.GetString("user_id"), SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		if c.GetString("admin_account_id") != "" {
			return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
		}
		return identityport.ResolveUserSessionPrincipal(ctx, h.userVerifier, h.userStrength, claims)
	}
	out, err := h.privateSources.document(c.Request.Context(), req, resolve)
	if err != nil {
		knowledgeError(c, err)
		return
	}
	response.Success(c, out)
}
