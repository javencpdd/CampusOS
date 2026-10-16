package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// This explicit public projection excludes moderation diagnostics, sessions,
// audit facts, author email and physical storage information.
type IntegrationPublicThread struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Content       string    `json:"content"`
	ContentFormat string    `json:"content_format"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (h *Handler) IntegrationThread(c *gin.Context) {
	if raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1)); err != nil || len(raw) > 0 {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	h.integrationRead(c, "integration_http", "public_thread.read", c.Param("id"))
}
func (h *Handler) IntegrationMCP(c *gin.Context) {
	if c.Param("name") != "public_thread_read" {
		response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 32769))
	var req struct {
		ID string `json:"id"`
	}
	if err != nil || identityport.ValidateIntegrationHTTPJSON(raw, []string{"id"}) != nil || json.Unmarshal(raw, &req) != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	h.integrationRead(c, "integration_mcp", "public_thread_read", req.ID)
}
func (h *Handler) integrationRead(c *gin.Context, channel, endpoint, id string) {
	token, ok := middleware.IntegrationToken(c)
	if !ok {
		return
	}
	if !identityport.IntegrationResourceIDValid(id) {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if h == nil || h.machine == nil || h.publicContent == nil {
		response.ErrorDescriptor(c, apperror.IdentityIntegrationUnavailable, nil)
		return
	}
	result, err := h.machine.ReadForIntegration(c.Request.Context(), token, identityport.IntegrationAccessTarget{Channel: channel, Endpoint: endpoint, Action: "community.thread.read", Resource: identityport.PolicyResourceRef{Kind: "community.thread", ID: id}}, func(ctx context.Context) (any, error) {
		thread, err := h.publicContent.GetPublicThread(ctx, id)
		if err != nil {
			return nil, err
		}
		if thread == nil || thread.ID != id {
			return nil, identityport.ErrIntegrationUnavailable
		}
		return IntegrationPublicThread{ID: thread.ID, Title: thread.Title, Content: thread.Content, ContentFormat: thread.ContentFormat, UpdatedAt: thread.UpdatedAt}, nil
	})
	if err != nil {
		var denied *identityport.IntegrationAccessDenied
		switch {
		case errors.Is(err, identityport.ErrIntegrationCredentialInvalid):
			response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		case errors.Is(err, communityport.ErrThreadNotFound):
			response.ErrorDescriptor(c, apperror.ResourceNotFound, nil)
		case errors.As(err, &denied), errors.Is(err, identityport.ErrIntegrationDenied):
			response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
		case errors.Is(err, identityport.ErrIntegrationChanged), errors.Is(err, identityport.ErrIntegrationFactsChanged):
			response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
		default:
			response.ErrorDescriptor(c, apperror.IdentityIntegrationUnavailable, nil)
		}
		return
	}
	response.Success(c, result)
}
