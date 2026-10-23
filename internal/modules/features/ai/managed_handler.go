package ai

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) managedResolver(c *gin.Context) identityport.SessionPrincipalResolver {
	id := c.GetString("admin_account_id")
	claims := &auth.JWTClaims{UserID: id, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		if id == "" || c.GetString("user_id") != id {
			return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
		}
		return identityport.ResolveAdminSessionPrincipal(ctx, h.adminSessions, claims)
	}
}
func managedHTTPError(c *gin.Context, e error) {
	c.Header("Cache-Control", "private, no-store")
	switch {
	case errors.Is(e, ErrManagedMFA):
		response.ErrorDescriptor(c, apperror.IdentityMFAStepUpRequired, nil)
	case errors.Is(e, ErrManagedDenied):
		response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
	case errors.Is(e, ErrManagedConflict), errors.Is(e, identityport.ErrPolicyFactsChanged):
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
	case errors.Is(e, identityport.ErrAdminSessionPrincipalUnavailable):
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
	default:
		response.ErrorDescriptor(c, apperror.InternalError, nil)
	}
}
func (h *Handler) managedReady(c *gin.Context) bool {
	c.Header("Cache-Control", "private, no-store")
	if h == nil || h.service == nil || h.service.managed == nil {
		managedHTTPError(c, ErrManagedUnavailable)
		return false
	}
	return true
}
func managedBody(c *gin.Context, out any, fields ...string) bool {
	_, refreshErr := c.Cookie("campusos_admin_refresh")
	if c.GetHeader("X-CampusOS-Client") == "browser" || c.GetHeader("Origin") != "" || c.GetHeader("Sec-Fetch-Site") != "" || refreshErr == nil {
		cookie, e := c.Cookie("campusos_admin_csrf")
		header := strings.TrimSpace(c.GetHeader("X-Admin-CSRF-Token"))
		if e != nil || len(cookie) < 16 || len(cookie) > 256 || subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) != 1 {
			managedHTTPError(c, ErrManagedDenied)
			return false
		}
	}
	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, 4097))
	if e != nil || c.Request.URL.RawQuery != "" || identityport.ValidateClosedHTTPObject(raw, fields, 4096) != nil || json.Unmarshal(raw, out) != nil {
		c.Header("Cache-Control", "private, no-store")
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return false
	}
	return true
}
func managedVersion(s string) int64 {
	v, e := strconv.ParseInt(s, 10, 64)
	if e != nil || v < 1 || strconv.FormatInt(v, 10) != s {
		return 0
	}
	return v
}
func (h *Handler) Profiles(c *gin.Context) {
	if !h.managedReady(c) {
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 0 {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	out, e := h.service.managed.Inventory(c.Request.Context(), h.managedResolver(c))
	if e != nil {
		managedHTTPError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) BindChat(c *gin.Context) {
	if !h.managedReady(c) {
		return
	}
	var req struct {
		ProfileID      string `json:"profile_id"`
		BindingVersion string `json:"binding_version"`
		ProfileVersion string `json:"profile_version"`
	}
	if !managedBody(c, &req, "profile_id", "binding_version", "profile_version") {
		return
	}
	if managedVersion(req.BindingVersion) == 0 || managedVersion(req.ProfileVersion) == 0 {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if e := h.service.managed.Bind(c.Request.Context(), h.managedResolver(c), req.ProfileID, managedVersion(req.BindingVersion), managedVersion(req.ProfileVersion)); e != nil {
		managedHTTPError(c, e)
		return
	}
	response.Success(c, gin.H{"updated": true})
}
func (h *Handler) ProfileState(c *gin.Context) {
	if !h.managedReady(c) {
		return
	}
	var req struct {
		Version string `json:"version"`
		Enabled bool   `json:"enabled"`
	}
	if !managedBody(c, &req, "version", "enabled") {
		return
	}
	if managedVersion(req.Version) == 0 {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if e := h.service.managed.SetProfileEnabled(c.Request.Context(), h.managedResolver(c), c.Param("profile_id"), managedVersion(req.Version), req.Enabled); e != nil {
		managedHTTPError(c, e)
		return
	}
	response.Success(c, gin.H{"updated": true})
}
func (h *Handler) managedLogs(c *gin.Context) {
	if !h.managedReady(c) {
		return
	}
	values := c.Request.URL.Query()
	if len(values) > 1 || len(values["limit"]) > 1 || (len(values) > 0 && len(values["limit"]) == 0) {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > 100 || strconv.Itoa(n) != raw {
			response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
			return
		}
		limit = n
	}
	s := h.service.managed
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resolve := h.managedResolver(c)
	a, e := s.admin(ctx, "ai.ai.read", resolve, false)
	if e != nil {
		managedHTTPError(c, e)
		return
	}
	rows, e := s.pool.Query(ctx, `SELECT profile_id,state,usage_known,prompt_tokens,completion_tokens,total_tokens,error_code,created_at FROM ai_model_runs ORDER BY created_at DESC,id DESC LIMIT $1`, limit)
	if e != nil {
		managedHTTPError(c, ErrManagedUnavailable)
		return
	}
	out := []CallLog{}
	for rows.Next() {
		var l CallLog
		var state string
		var known bool
		if e = rows.Scan(&l.Provider, &state, &known, &l.Usage.PromptTokens, &l.Usage.CompletionTokens, &l.Usage.TotalTokens, &l.Error, &l.Timestamp); e != nil {
			break
		}
		l.Source = "managed-chat"
		l.Status = CallStatus(state)
		out = append(out, l)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		managedHTTPError(c, ErrManagedUnavailable)
		return
	}
	current, e := s.admin(ctx, "ai.ai.read", resolve, false)
	if e != nil || identityport.CheckAdminExecutionAuthority(a, current) != nil {
		managedHTTPError(c, ErrManagedDenied)
		return
	}
	response.Success(c, gin.H{"items": out, "total": len(out)})
}
