package knowledge

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// IntegrationSource opens exactly one current public citation. A machine rule
// grants an endpoint and collection, never authority over the original source.
// The path revision is mandatory; this endpoint does not serve old snapshots.
func (h *Handler) IntegrationSource(c *gin.Context) {
	token, ok := middleware.IntegrationToken(c)
	if !ok {
		return
	}
	id, memberID, revision := c.Param("id"), c.Param("member_id"), c.Param("revision")
	if !opaque("kc_", id) || !opaque("km_", memberID) || len(revision) != 64 || strings.Trim(revision, "0123456789abcdef") != "" {
		knowledgeError(c, ErrInvalid)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	if err != nil || len(raw) != 0 {
		knowledgeError(c, ErrInvalid)
		return
	}
	if !h.ready(c) {
		return
	}
	if nilSourceAdapter(h.machine) {
		response.ErrorDescriptor(c, apperror.IdentityIntegrationUnavailable, nil)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	target := identityport.IntegrationAccessTarget{
		Channel: "integration_http", Endpoint: "knowledge.source.read", Action: "knowledge.source.read",
		Resource: identityport.PolicyResourceRef{Kind: "public_collection", ID: id},
	}
	data, err := h.machine.ReadForIntegration(ctx, token, target, func(tx context.Context) (any, error) {
		// CheckedRead reuses Identity's transaction. Collection/membership and
		// Community source-row locks survive through the proof and audit commit.
		// Other source checks remain owned by their published public-read Ports.
		return h.service.Source(tx, id, memberID, revision)
	})
	if err != nil {
		integrationSourceError(c, err)
		return
	}
	out, ok := data.(VisibleSource)
	if !ok || out.CollectionID != id || out.MemberID != memberID || out.Revision != revision {
		knowledgeError(c, ErrRetrievalProvider)
		return
	}
	// A hot disable or cancellation during Identity's final audit must also
	// discard the buffered body. No machine result or allow decision is cached.
	if err = h.service.check(ctx); err != nil {
		knowledgeError(c, err)
		return
	}
	response.Success(c, out)
}

func integrationSourceError(c *gin.Context, err error) {
	var denied *identityport.IntegrationAccessDenied
	switch {
	case errors.Is(err, identityport.ErrIntegrationCredentialInvalid):
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
	case errors.As(err, &denied), errors.Is(err, identityport.ErrIntegrationDenied):
		response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
	case errors.Is(err, identityport.ErrIntegrationChanged), errors.Is(err, identityport.ErrIntegrationFactsChanged):
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
	case errors.Is(err, identityport.ErrIntegrationUnavailable):
		response.ErrorDescriptor(c, apperror.IdentityIntegrationUnavailable, nil)
	default:
		knowledgeError(c, err)
	}
}
