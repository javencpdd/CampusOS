package runtimeconfig

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service  *Service
	sessions identityport.AdminSessionAuthorityReader
}

func NewHandler(service *Service, sessions identityport.AdminSessionAuthorityReader) *Handler {
	return &Handler{service: service, sessions: sessions}
}
func (h *Handler) resolver(c *gin.Context) identityport.SessionPrincipalResolver {
	claims := &auth.JWTClaims{UserID: c.GetString("admin_account_id"), SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, h.sessions, claims)
	}
}
func writeError(c *gin.Context, err error) {
	if errors.Is(err, ErrMFA) {
		response.ErrorDescriptor(c, apperror.IdentityMFAStepUpRequired, nil)
		return
	}
	if errors.Is(err, identityport.ErrAdminSessionPrincipalUnavailable) {
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		return
	}
	status, msg := publicError(err)
	response.Error(c, status, 10001, msg)
}
func (h *Handler) Read(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if h == nil || h.service == nil {
		writeError(c, ErrUnavailable)
		return
	}
	value, err := h.service.Read(c.Request.Context(), h.resolver(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, value)
}
func (h *Handler) Update(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if h == nil || h.service == nil {
		writeError(c, ErrUnavailable)
		return
	}
	if !configurationCSRF(c) {
		writeError(c, ErrDenied)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (config.MaxRuntimeDocumentBytes*6)+(32<<10))
	var input Update
	if c.Request.URL.RawQuery != "" || request.BindJSONStrict(c, &input) != nil || len(input.Revision) != 64 || len(input.Changes) > len(h.service.cfg.Values) {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	value, err := h.service.Apply(c.Request.Context(), h.resolver(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, value)
	if input.Restart {
		h.service.ScheduleRestart()
	}
}

func configurationCSRF(c *gin.Context) bool {
	cookie, err := c.Cookie("campusos_admin_csrf")
	header := strings.TrimSpace(c.GetHeader("X-Admin-CSRF-Token"))
	return err == nil && len(cookie) >= 16 && len(cookie) <= 256 && subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) == 1
}

func (h *Handler) Document(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if h == nil || h.service == nil {
		writeError(c, ErrUnavailable)
		return
	}
	if !configurationCSRF(c) {
		writeError(c, ErrDenied)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var input DocumentConfirmation
	if c.Request.URL.RawQuery != "" || request.BindJSONStrict(c, &input) != nil || len(input.Revision) != 64 {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	value, err := h.service.ReadDocument(c.Request.Context(), h.resolver(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, value)
}
