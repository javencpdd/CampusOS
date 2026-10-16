package handler

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type IntegrationHandler struct {
	service *service.IntegrationService
	admin   *AdminDomainHandler
}

func NewIntegrationHandler(s *service.IntegrationService, admin *AdminDomainHandler) *IntegrationHandler {
	return &IntegrationHandler{service: s, admin: admin}
}
func integrationBody(c *gin.Context, target any, fields ...string) bool {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 32769))
	if err != nil || len(raw) > 32768 || port.ValidateIntegrationHTTPJSON(raw, fields) != nil || json.Unmarshal(raw, target) != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return false
	}
	return true
}
func (h *IntegrationHandler) managed(c *gin.Context) bool {
	c.Header("Cache-Control", "private, no-store")
	if h == nil || h.service == nil || h.admin == nil {
		response.ErrorDescriptor(c, apperror.IdentityIntegrationUnavailable, nil)
		return false
	}
	return h.admin.requireBrowserCSRF(c)
}
func writeIntegrationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrIntegrationMFARequired):
		response.ErrorDescriptor(c, apperror.IdentityMFAStepUpRequired, nil)
	case errors.Is(err, service.ErrIntegrationInvalid), errors.Is(err, port.ErrIntegrationInputInvalid):
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
	case errors.Is(err, service.ErrIntegrationChanged), errors.Is(err, port.ErrPolicyFactsChanged), errors.Is(err, repository.ErrIntegrationConflict):
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
	case errors.Is(err, repository.ErrIntegrationNotFound):
		response.ErrorDescriptor(c, apperror.ResourceNotFound, nil)
	case errors.Is(err, service.ErrIntegrationCredentialInvalid):
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
	case errors.Is(err, service.ErrIntegrationDenied):
		response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
	default:
		response.ErrorDescriptor(c, apperror.IdentityIntegrationUnavailable, nil)
	}
}
func (h *IntegrationHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if h == nil || h.service == nil || h.admin == nil {
		response.ErrorDescriptor(c, apperror.IdentityIntegrationUnavailable, nil)
		return
	}
	if c.Request.ContentLength > 0 {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	for key, values := range c.Request.URL.Query() {
		if key != "after" || len(values) != 1 {
			response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
			return
		}
	}
	out, err := h.service.Inventory(c.Request.Context(), c.Param("id"), c.Query("after"), h.admin.currentAdminResolver(c))
	if err != nil {
		writeIntegrationError(c, err)
		return
	}
	response.Success(c, out)
}
func (h *IntegrationHandler) Endpoints(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if h == nil || h.service == nil || h.admin == nil {
		writeIntegrationError(c, service.ErrIntegrationUnavailable)
		return
	}
	if raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1)); err != nil || len(raw) > 0 || c.Request.URL.RawQuery != "" {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if _, err := h.service.Inventory(c.Request.Context(), "", "", h.admin.currentAdminResolver(c)); err != nil {
		writeIntegrationError(c, err)
		return
	}
	response.Success(c, service.IntegrationEndpoints())
}
func (h *IntegrationHandler) Create(c *gin.Context) {
	if !h.managed(c) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !integrationBody(c, &req, "name") {
		return
	}
	out, err := h.service.CreatePrincipal(c.Request.Context(), req.Name, h.admin.currentAdminResolver(c))
	if err != nil {
		writeIntegrationError(c, err)
		return
	}
	response.Created(c, out)
}
func (h *IntegrationHandler) Update(c *gin.Context) {
	if !h.managed(c) {
		return
	}
	var req struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Version int64  `json:"version,string"`
	}
	if !integrationBody(c, &req, "name", "status", "version") {
		return
	}
	out, err := h.service.UpdatePrincipal(c.Request.Context(), c.Param("id"), req.Name, req.Status, req.Version, h.admin.currentAdminResolver(c))
	if err != nil {
		writeIntegrationError(c, err)
		return
	}
	response.Success(c, out)
}
func (h *IntegrationHandler) Issue(c *gin.Context) {
	if !h.managed(c) {
		return
	}
	var req service.IntegrationIssueRequest
	if !integrationBody(c, &req, "principal_version", "not_before_ms", "expires_at_ms", "rules") {
		return
	}
	out, err := h.service.IssueCredential(c.Request.Context(), c.Param("id"), req, h.admin.currentAdminResolver(c))
	if err != nil {
		writeIntegrationError(c, err)
		return
	}
	response.Created(c, out)
}
func (h *IntegrationHandler) Rotate(c *gin.Context) {
	if !h.managed(c) {
		return
	}
	var req struct {
		service.IntegrationIssueRequest
		Version int64 `json:"version,string"`
	}
	if !integrationBody(c, &req, "principal_version", "not_before_ms", "expires_at_ms", "rules", "version") {
		return
	}
	out, err := h.service.RotateCredential(c.Request.Context(), c.Param("id"), c.Param("credential_id"), req.Version, req.IntegrationIssueRequest, h.admin.currentAdminResolver(c))
	if err != nil {
		writeIntegrationError(c, err)
		return
	}
	response.Created(c, out)
}
func (h *IntegrationHandler) Scope(c *gin.Context) {
	if !h.managed(c) {
		return
	}
	var req struct {
		Version int64                        `json:"version,string"`
		Rules   []port.IntegrationAccessRule `json:"rules"`
	}
	if !integrationBody(c, &req, "version", "rules") {
		return
	}
	out, err := h.service.UpdateCredential(c.Request.Context(), c.Param("id"), c.Param("credential_id"), req.Version, req.Rules, false, h.admin.currentAdminResolver(c))
	if err != nil {
		writeIntegrationError(c, err)
		return
	}
	response.Success(c, out)
}
func (h *IntegrationHandler) Revoke(c *gin.Context) {
	if !h.managed(c) {
		return
	}
	var req struct {
		Version int64 `json:"version,string"`
	}
	if !integrationBody(c, &req, "version") {
		return
	}
	out, err := h.service.UpdateCredential(c.Request.Context(), c.Param("id"), c.Param("credential_id"), req.Version, nil, true, h.admin.currentAdminResolver(c))
	if err != nil {
		writeIntegrationError(c, err)
		return
	}
	response.Success(c, out)
}

func (h *IntegrationHandler) Me(c *gin.Context) {
	token, ok := middleware.IntegrationToken(c)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		response.ErrorDescriptor(c, apperror.IdentityIntegrationUnavailable, nil)
		return
	}
	if raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1)); err != nil || len(raw) > 0 {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	out, err := h.service.CurrentIntegration(c.Request.Context(), token)
	if err != nil {
		writeIntegrationError(c, err)
		return
	}
	response.Success(c, out)
}
