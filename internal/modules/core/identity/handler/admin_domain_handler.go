package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// AdminDomainHandler exposes the independent admin identity domain over HTTP:
// admin login/refresh/logout, bootstrap and account lifecycle, and TOTP
// step-up. All authenticated routes sit behind the admin-domain middleware;
// user tokens never reach these handlers.
type AdminDomainHandler struct {
	lifecycle *service.AdminDomainService
	sessions  *service.AdminSessionService
	mfa       *service.AdminMFAService
}

func NewAdminDomainHandler(lifecycle *service.AdminDomainService, sessions *service.AdminSessionService, mfa *service.AdminMFAService) *AdminDomainHandler {
	return &AdminDomainHandler{lifecycle: lifecycle, sessions: sessions, mfa: mfa}
}

type adminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type adminTokenResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func writeAdminTokens(c *gin.Context, tokens *service.AdminSessionTokens) {
	response.Success(c, adminTokenResponse{
		AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, TokenType: "Bearer", ExpiresAt: tokens.ExpiresAt,
	})
}

func adminSessionMetadata(c *gin.Context) service.AdminSessionMetadata {
	return service.AdminSessionMetadata{
		DeviceName: c.GetHeader("X-Device-Name"),
		UserAgent:  c.GetHeader("User-Agent"),
		ClientIP:   c.ClientIP(),
	}
}

// POST /api/v1/admin/identity/login
func (h *AdminDomainHandler) Login(c *gin.Context) {
	var req adminLoginRequest
	if err := request.BindJSONStrict(c, &req); err != nil || strings.TrimSpace(req.Username) == "" || req.Password == "" {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if h.sessions == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	tokens, err := h.sessions.Login(c.Request.Context(), strings.TrimSpace(req.Username), req.Password, adminSessionMetadata(c))
	if err != nil {
		// One denial shape for unknown accounts, wrong passwords, and
		// deactivated administrators.
		response.ErrorDescriptor(c, apperror.IdentityCredentialsInvalid, nil)
		return
	}
	writeAdminTokens(c, tokens)
}

// POST /api/v1/admin/identity/refresh
func (h *AdminDomainHandler) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := request.BindJSONStrict(c, &req); err != nil || req.RefreshToken == "" {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	tokens, err := h.sessions.Refresh(c.Request.Context(), req.RefreshToken, adminSessionMetadata(c))
	if errors.Is(err, service.ErrAdminRefreshReuse) {
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		return
	}
	if err != nil {
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		return
	}
	writeAdminTokens(c, tokens)
}

// POST /api/v1/admin/identity/logout (authenticated)
func (h *AdminDomainHandler) Logout(c *gin.Context) {
	sessionID := c.GetString("session_id")
	if err := h.sessions.RevokeSession(c.Request.Context(), sessionID, "logout"); err != nil {
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		return
	}
	response.Success(c, gin.H{"revoked": true})
}

// POST /api/v1/admin/identity/bootstrap
func (h *AdminDomainHandler) Bootstrap(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Reason   string `json:"reason"`
	}
	if err := request.BindJSONStrict(c, &req); err != nil || req.Username == "" || req.Email == "" || req.Password == "" || req.Reason == "" {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	account, err := h.lifecycle.BootstrapSecurityAdmin(c.Request.Context(), req.Username, req.Email, req.Password, req.Reason)
	if err != nil {
		response.Error(c, http.StatusForbidden, 20004, "bootstrap denied")
		return
	}
	response.Success(c, gin.H{"account_id": account.ID, "username": account.Username})
}

func adminActorFrom(c *gin.Context) service.AdminActor {
	return service.AdminActor{
		ID:               c.GetString("admin_account_id"),
		MFAAuthenticated: c.GetString("authentication_strength") == "mfa",
		CredentialID:     c.GetString("session_id"),
	}
}

// POST /api/v1/admin/identity/accounts (authenticated, MFA)
func (h *AdminDomainHandler) CreateAccount(c *gin.Context) {
	var req struct {
		Username      string `json:"username"`
		Email         string `json:"email"`
		Password      string `json:"password"`
		SecurityAdmin bool   `json:"security_admin"`
		Reason        string `json:"reason"`
	}
	if err := request.BindJSONStrict(c, &req); err != nil || req.Username == "" || req.Email == "" || req.Password == "" || req.Reason == "" {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	account, err := h.lifecycle.CreateAdmin(c.Request.Context(), adminActorFrom(c), req.Username, req.Email, req.Password, req.SecurityAdmin, req.Reason)
	if err != nil {
		writeAdminDomainError(c, err)
		return
	}
	response.Success(c, gin.H{"account_id": account.ID, "username": account.Username, "status": account.Status})
}

func (h *AdminDomainHandler) transition(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Reason string `json:"reason"`
		}
		if err := request.BindJSONStrict(c, &req); err != nil || req.Reason == "" {
			response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
			return
		}
		var err error
		switch kind {
		case "suspend":
			err = h.lifecycle.SuspendAdmin(c.Request.Context(), adminActorFrom(c), c.Param("id"), req.Reason)
		case "revoke":
			err = h.lifecycle.RevokeAdmin(c.Request.Context(), adminActorFrom(c), c.Param("id"), req.Reason)
		case "restore":
			err = h.lifecycle.RestoreAdmin(c.Request.Context(), adminActorFrom(c), c.Param("id"), req.Reason)
		}
		if err != nil {
			writeAdminDomainError(c, err)
			return
		}
		response.Success(c, gin.H{"account_id": c.Param("id"), "status": kind + "d"})
	}
}

func (h *AdminDomainHandler) SuspendAccount(c *gin.Context) { h.transition("suspend")(c) }
func (h *AdminDomainHandler) RevokeAccount(c *gin.Context)  { h.transition("revoke")(c) }
func (h *AdminDomainHandler) RestoreAccount(c *gin.Context) { h.transition("restore")(c) }

// POST /api/v1/admin/identity/mfa/enroll (authenticated, MFA actor)
func (h *AdminDomainHandler) EnrollTOTP(c *gin.Context) {
	var req struct {
		Issuer string `json:"issuer"`
	}
	if err := request.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	issuer := req.Issuer
	if issuer == "" {
		issuer = "campusos-admin"
	}
	otpauth, err := h.mfa.StartEnrollment(c.Request.Context(), adminActorFrom(c), issuer)
	if err != nil {
		writeAdminDomainError(c, err)
		return
	}
	response.Success(c, gin.H{"otpauth_url": otpauth})
}

// POST /api/v1/admin/identity/mfa/confirm
func (h *AdminDomainHandler) ConfirmTOTP(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if err := request.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if err := h.mfa.ConfirmEnrollment(c.Request.Context(), adminActorFrom(c), req.Code); err != nil {
		writeAdminDomainError(c, err)
		return
	}
	response.Success(c, gin.H{"status": "active"})
}

// POST /api/v1/admin/identity/mfa/step-up
func (h *AdminDomainHandler) StepUp(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if err := request.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if err := h.mfa.VerifyStepUp(c.Request.Context(), adminActorFrom(c), c.GetString("session_id"), req.Code); err != nil {
		writeAdminDomainError(c, err)
		return
	}
	response.Success(c, gin.H{"authentication_strength": "mfa"})
}

func writeAdminDomainError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAdminSelfTransition):
		response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
	case err != nil && strings.HasPrefix(err.Error(), "identity."):
		response.Error(c, http.StatusForbidden, 20004, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, 71000, "管理身份域操作失败")
	}
}
