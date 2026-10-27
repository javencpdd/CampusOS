package handler

import (
	"crypto/sha256"
	"crypto/subtle"
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

const AdminBootstrapSecretHeader = "X-CampusOS-Bootstrap-Secret"

// AdminDomainHandler exposes the independent admin identity domain over HTTP:
// admin login/refresh/logout, bootstrap and account lifecycle, and TOTP
// step-up. All authenticated routes sit behind the admin-domain middleware;
// user tokens never reach these handlers.
type AdminDomainHandler struct {
	lifecycle           *service.AdminDomainService
	sessions            *service.AdminSessionService
	mfa                 *service.AdminMFAService
	currentSelf         *service.AdminSelfService
	sessionHTTP         SessionHTTPConfig
	bootstrapProof      [sha256.Size]byte
	bootstrapConfigured bool
}

func NewAdminDomainHandler(lifecycle *service.AdminDomainService, sessions *service.AdminSessionService, mfa *service.AdminMFAService, currentSelf ...*service.AdminSelfService) *AdminDomainHandler {
	h := &AdminDomainHandler{lifecycle: lifecycle, sessions: sessions, mfa: mfa}
	if len(currentSelf) > 0 {
		h.currentSelf = currentSelf[0]
	}
	return h
}

// SetBootstrapSecret binds the HTTP entry to deployment custody. Only a digest
// is retained; merely enabling the lifecycle's bootstrap policy is not proof
// that an anonymous request is the operator.
func (h *AdminDomainHandler) SetBootstrapSecret(secret string) {
	h.bootstrapProof = [sha256.Size]byte{}
	h.bootstrapConfigured = false
	secret = strings.TrimSpace(secret)
	if len(secret) < 16 || len(secret) > 4096 {
		return
	}
	h.bootstrapProof = sha256.Sum256([]byte(secret))
	h.bootstrapConfigured = true
}

type adminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type adminTokenResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
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
	c.Header("Cache-Control", "private, no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
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
		if errors.Is(err, service.ErrAdminLoginDenied) {
			response.ErrorDescriptor(c, apperror.IdentityCredentialsInvalid, nil)
		} else {
			response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		}
		return
	}
	h.writeBrowserTokens(c, tokens)
}

// POST /api/v1/admin/identity/refresh
func (h *AdminDomainHandler) Refresh(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := request.BindJSONStrictOptional(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	browser := h.browserSession(c)
	var raw string
	if browser {
		if !h.requireBrowserCSRF(c) {
			return
		}
		if req.RefreshToken != "" {
			response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
			return
		}
		raw, _ = c.Cookie(adminRefreshCookieName)
	} else if h.sessionHTTP.RefreshBodyCompat {
		raw = req.RefreshToken
	}
	if raw == "" {
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		return
	}
	if h.sessions == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	tokens, err := h.sessions.Refresh(c.Request.Context(), raw, adminSessionMetadata(c))
	if err != nil {
		if errors.Is(err, service.ErrAdminRefreshInvalid) || errors.Is(err, service.ErrAdminRefreshReuse) {
			h.clearBrowserCookies(c)
			response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		} else {
			response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		}
		return
	}
	h.writeBrowserTokens(c, tokens)
}

// POST /api/v1/admin/identity/logout (current authenticated Admin only)
func (h *AdminDomainHandler) Logout(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var req struct{}
	if request.BindJSONStrictOptional(c, &req) != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if !h.requireBrowserCSRF(c) {
		return
	}
	if h.sessions == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	if err := h.sessions.RevokeCurrentSession(c.Request.Context(), h.currentAdminResolver(c)); err != nil {
		writeAdminSelfError(c, err)
		return
	}
	h.clearBrowserCookies(c)
	response.Success(c, gin.H{"revoked": true})
}

// POST /api/v1/admin/identity/bootstrap
func (h *AdminDomainHandler) Bootstrap(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	proofs := c.Request.Header.Values(AdminBootstrapSecretHeader)
	if !h.bootstrapConfigured || h.lifecycle == nil || len(proofs) != 1 || len(proofs[0]) < 16 || len(proofs[0]) > 4096 {
		response.Error(c, http.StatusForbidden, 20004, "bootstrap denied")
		return
	}
	proof := sha256.Sum256([]byte(proofs[0]))
	if subtle.ConstantTimeCompare(proof[:], h.bootstrapProof[:]) != 1 {
		response.Error(c, http.StatusForbidden, 20004, "bootstrap denied")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
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
	actor, ok := h.currentMFAActor(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
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
	otpauth, err := h.mfa.StartEnrollment(c.Request.Context(), actor, issuer)
	if err != nil {
		writeAdminSelfError(c, err)
		return
	}
	response.Success(c, gin.H{"otpauth_url": otpauth})
}

// POST /api/v1/admin/identity/mfa/confirm
func (h *AdminDomainHandler) ConfirmTOTP(c *gin.Context) {
	actor, ok := h.currentMFAActor(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var req struct {
		Code string `json:"code"`
	}
	if err := request.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if err := h.mfa.ConfirmEnrollment(c.Request.Context(), actor, req.Code); err != nil {
		writeAdminSelfError(c, err)
		return
	}
	response.Success(c, gin.H{"status": "active"})
}

// POST /api/v1/admin/identity/mfa/step-up
func (h *AdminDomainHandler) StepUp(c *gin.Context) {
	actor, ok := h.currentMFAActor(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var req struct {
		Code string `json:"code"`
	}
	if err := request.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if err := h.mfa.VerifyStepUp(c.Request.Context(), actor, actor.CredentialID, req.Code); err != nil {
		writeAdminSelfError(c, err)
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
