package knowledge

import (
	"context"
	"encoding/json"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
	"io"
)

func (h *Handler) privateResolver(c *gin.Context) identityport.SessionPrincipalResolver {
	claims := &auth.JWTClaims{UserID: c.GetString("user_id"), SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		if c.GetString("admin_account_id") != "" {
			return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
		}
		return identityport.ResolveUserSessionPrincipal(ctx, h.userVerifier, h.userStrength, claims)
	}
}
func (h *Handler) privateBody(c *gin.Context, out any, fields ...string) bool {
	c.Header("Cache-Control", "private, no-store")
	const max = 4096
	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, max+1))
	if e != nil || len(raw) > max || c.Request.URL.RawQuery != "" || identityport.ValidateClosedHTTPObject(raw, fields, max) != nil || json.Unmarshal(raw, out) != nil {
		knowledgeError(c, ErrInvalid)
		return false
	}
	return true
}
func (h *Handler) privateReady(c *gin.Context) bool {
	c.Header("Cache-Control", "private, no-store")
	if h == nil || h.privateIndex == nil {
		knowledgeError(c, ErrPrivateUnavailable)
		return false
	}
	return true
}
func (h *Handler) PrivateStatus(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if !noQuery(c) {
		return
	}
	if _, e := privateUser(c.Request.Context(), h.privateResolver(c)); e != nil {
		knowledgeError(c, e)
		return
	}
	ready := h.privateIndex.available()
	reason := "private_storage_unavailable"
	if ready {
		reason = "ready"
	}
	response.Success(c, gin.H{"can_index": ready, "reason": reason, "default_inclusion": false, "supported_formats": []string{"text", "markdown", "campusdoc"}, "max_sources": 100, "max_source_bytes": MaxSourceBodyBytes, "outbound_authorized": false})
}
func (h *Handler) PrivatePreview(c *gin.Context) {
	if !h.privateReady(c) {
		return
	}
	var req struct {
		Kind string `json:"kind"`
		ID   string `json:"source_id"`
	}
	if !h.privateBody(c, &req, "kind", "source_id") {
		return
	}
	src, e := h.privateIndex.Preview(c.Request.Context(), h.privateResolver(c), req.Kind, req.ID)
	if e != nil {
		knowledgeError(c, e)
		return
	}
	ready := privateSupported(src.Format)
	reason := "ready"
	if !ready {
		reason = "unsupported_format"
	}
	response.Success(c, gin.H{"source": src, "can_index": ready, "reason": reason})
}
func (h *Handler) PrivateSelect(c *gin.Context) {
	if !h.privateReady(c) {
		return
	}
	var req struct {
		Kind     string `json:"kind"`
		ID       string `json:"source_id"`
		Revision string `json:"revision"`
		Consent  *bool  `json:"consent"`
	}
	if !h.privateBody(c, &req, "kind", "source_id", "revision", "consent") {
		return
	}
	if req.Consent == nil {
		knowledgeError(c, ErrInvalid)
		return
	}
	out, e := h.privateIndex.Select(c.Request.Context(), h.privateResolver(c), req.Kind, req.ID, req.Revision, *req.Consent)
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) PrivateSelections(c *gin.Context) {
	if !h.privateReady(c) || !noQuery(c) {
		return
	}
	out, e := h.privateIndex.List(c.Request.Context(), h.privateResolver(c))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, gin.H{"items": out})
}
func (h *Handler) PrivateWithdraw(c *gin.Context) {
	if !h.privateReady(c) || !noQuery(c) {
		return
	}
	e := h.privateIndex.Withdraw(c.Request.Context(), h.privateResolver(c), c.Param("selection_id"))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, gin.H{"withdrawn": true})
}
func (h *Handler) PrivateSearch(c *gin.Context) {
	if !h.privateReady(c) {
		return
	}
	var req struct {
		Query string `json:"query"`
	}
	if !h.privateBody(c, &req, "query") {
		return
	}
	out, e := h.privateIndex.Search(c.Request.Context(), h.privateResolver(c), req.Query)
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) PrivateSource(c *gin.Context) {
	if !h.privateReady(c) {
		return
	}
	var req struct {
		Revision string `json:"revision"`
	}
	if !h.privateBody(c, &req, "revision") {
		return
	}
	if !validPrivateID(c.Param("selection_id")) || !validPrivateRevision(req.Revision) {
		knowledgeError(c, ErrInvalid)
		return
	}
	out, e := h.privateIndex.Source(c.Request.Context(), h.privateResolver(c), c.Param("selection_id"), req.Revision)
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) PrivateRetry(c *gin.Context) {
	if !h.privateReady(c) || !noQuery(c) {
		return
	}
	e := h.privateIndex.Retry(c.Request.Context(), h.privateResolver(c), c.Param("selection_id"))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, gin.H{"accepted": true})
}
