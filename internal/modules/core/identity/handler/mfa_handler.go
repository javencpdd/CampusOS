package handler

import (
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/apperror"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type mfaLoginCompleteRequest struct {
	Ticket string `json:"mfa_ticket"`
	Code   string `json:"code"`
}

type mfaEnrollmentRequest struct {
	Password string `json:"password"`
}
type mfaCodeRequest struct {
	Code string `json:"code"`
}
type mfaRecoveryRotateRequest struct {
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}
type mfaDisableRequest struct {
	Password     string `json:"password"`
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}
type mfaPolicyRequest struct {
	Mode            domain.MFAPolicyMode `json:"mode"`
	GraceEndsAt     string               `json:"grace_ends_at"`
	ExpectedVersion int64                `json:"expected_version"`
}

// CompleteMFALogin exchanges a short-lived opaque ticket for a normal Session
// only after a valid TOTP step. It is public because it precedes Session
// issuance, but a web ticket can never produce an Admin Session.
// POST /api/v1/auth/mfa/login/complete
func (h *UserHandler) CompleteMFALogin(c *gin.Context) {
	if h == nil || h.mfa == nil || h.sessions == nil || h.svc == nil {
		response.WriteError(c, unavailableError(apperror.IdentityMFAUnavailable))
		return
	}
	var request mfaLoginCompleteRequest
	if err := requestutil.BindJSONStrict(c, &request); err != nil {
		response.ErrorDescriptor(c, apperror.IdentityMFAInvalid, nil)
		return
	}
	completed, err := h.mfa.CompleteLogin(c.Request.Context(), request.Ticket, request.Code)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(mfaErrorTranslator.Translate(err)))
		return
	}
	user, err := h.svc.GetByID(c.Request.Context(), completed.UserID)
	if err != nil || user == nil {
		response.ErrorDescriptor(c, apperror.IdentityMFATicketInvalid, nil)
		return
	}
	if completed.Audience == domain.MFAAudienceAdmin {
		if h.adminAccess == nil || h.adminAccess.RecordAuthentication(c.Request.Context(), user.ID) != nil {
			response.ErrorDescriptor(c, apperror.IdentityAdminCredentialsInvalid, nil)
			return
		}
	}
	tokens, err := h.sessions.IssueWithAuthentication(c.Request.Context(), user, h.sessionMetadata(c), domain.MFAAuthenticationTOTP, nil)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(err)))
		return
	}
	h.writeSessionCookies(c, tokens)
	roles, _ := h.svc.GetUserRoles(c.Request.Context(), user.ID)
	response.Success(c, domain.LoginResponse{
		User: user, Roles: roles, AccessToken: tokens.AccessToken, RefreshToken: h.compatRefreshToken(tokens.RefreshToken),
		TokenType: "Bearer", ExpiresIn: tokens.ExpiresIn,
	})
}

// GetMFAStatus returns only the caller's factor state and remaining recovery
// code count. It never reveals encryption key IDs, encrypted material or raw
// recovery codes.
// GET /api/v1/auth/mfa
func (h *UserHandler) GetMFAStatus(c *gin.Context) {
	userID, _, ok := currentSession(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	if h == nil || h.mfa == nil {
		response.WriteError(c, unavailableError(apperror.IdentityMFAUnavailable))
		return
	}
	status, err := h.mfa.Status(c.Request.Context(), userID)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(mfaErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, status)
}

// StartMFAEnrollment requires current-password reauthentication before any
// one-time manual key is returned.
// POST /api/v1/auth/mfa/totp/enrollment
func (h *UserHandler) StartMFAEnrollment(c *gin.Context) {
	userID, _, ok := currentSession(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	if h == nil || h.mfa == nil || h.svc == nil || !h.requireCSRF(c) {
		response.ErrorDescriptor(c, apperror.IdentityCSRFInvalid, nil)
		return
	}
	var request mfaEnrollmentRequest
	if err := requestutil.BindJSONStrict(c, &request); err != nil || strings.TrimSpace(request.Password) == "" {
		response.ErrorDescriptor(c, apperror.IdentityMFAInvalid, nil)
		return
	}
	if err := h.svc.VerifyCurrentPassword(c.Request.Context(), userID, request.Password); err != nil {
		response.ErrorDescriptor(c, apperror.IdentityCredentialsInvalid, nil)
		return
	}
	enrollment, err := h.mfa.StartEnrollment(c.Request.Context(), userID)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(mfaErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, enrollment)
}

// ConfirmMFAEnrollment activates a pending TOTP factor and returns recovery
// codes exactly once. The authenticated session is upgraded to MFA strength.
// POST /api/v1/auth/mfa/totp/confirm
func (h *UserHandler) ConfirmMFAEnrollment(c *gin.Context) {
	userID, sessionID, ok := currentSession(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	if h == nil || h.mfa == nil || h.sessions == nil || !h.requireCSRF(c) {
		response.ErrorDescriptor(c, apperror.IdentityCSRFInvalid, nil)
		return
	}
	var request mfaCodeRequest
	if err := requestutil.BindJSONStrict(c, &request); err != nil {
		response.ErrorDescriptor(c, apperror.IdentityMFAInvalid, nil)
		return
	}
	confirmation, err := h.mfa.ConfirmEnrollment(c.Request.Context(), userID, request.Code)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(mfaErrorTranslator.Translate(err)))
		return
	}
	tokens, err := h.sessions.MarkMFA(c.Request.Context(), userID, sessionID)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, gin.H{
		"recovery_codes": confirmation.RecoveryCodes, "access_token": tokens.AccessToken,
		"token_type": "Bearer", "expires_in": tokens.ExpiresIn,
	})
}

// RotateMFARecoveryCodes proves control with a current TOTP code or one unused
// recovery code, then invalidates the old set before returning a new one.
// POST /api/v1/auth/mfa/recovery-codes/rotate
func (h *UserHandler) RotateMFARecoveryCodes(c *gin.Context) {
	userID, _, ok := currentSession(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	if h == nil || h.mfa == nil || !h.requireCSRF(c) {
		response.ErrorDescriptor(c, apperror.IdentityCSRFInvalid, nil)
		return
	}
	var request mfaRecoveryRotateRequest
	if err := requestutil.BindJSONStrict(c, &request); err != nil {
		response.ErrorDescriptor(c, apperror.IdentityMFAInvalid, nil)
		return
	}
	confirmation, err := h.mfa.RotateRecoveryCodes(c.Request.Context(), userID, request.Code, request.RecoveryCode)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(mfaErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, confirmation)
}

// DisableMFA requires current-password reauthentication plus a TOTP or one
// recovery code. All Sessions are then revoked so the next login is explicit.
// DELETE /api/v1/auth/mfa/totp
func (h *UserHandler) DisableMFA(c *gin.Context) {
	userID, _, ok := currentSession(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	if h == nil || h.mfa == nil || h.svc == nil || h.sessions == nil || !h.requireCSRF(c) {
		response.ErrorDescriptor(c, apperror.IdentityCSRFInvalid, nil)
		return
	}
	var request mfaDisableRequest
	if err := requestutil.BindJSONStrict(c, &request); err != nil || strings.TrimSpace(request.Password) == "" || (strings.TrimSpace(request.Code) == "" && strings.TrimSpace(request.RecoveryCode) == "") || (strings.TrimSpace(request.Code) != "" && strings.TrimSpace(request.RecoveryCode) != "") {
		response.ErrorDescriptor(c, apperror.IdentityMFAInvalid, nil)
		return
	}
	if err := h.svc.VerifyCurrentPassword(c.Request.Context(), userID, request.Password); err != nil {
		response.ErrorDescriptor(c, apperror.IdentityCredentialsInvalid, nil)
		return
	}
	if err := h.mfa.Disable(c.Request.Context(), userID, request.Code, request.RecoveryCode); err != nil {
		response.WriteError(c, unavailableIfInternal(mfaErrorTranslator.Translate(err)))
		return
	}
	h.clearSessionCookies(c)
	response.Success(c, gin.H{"disabled": true, "relogin_required": true})
}

// StepUpMFA upgrades the current session's server-side authentication strength
// and returns a fresh Access Token. It never rotates or exposes Refresh data.
// POST /api/v1/auth/mfa/step-up
func (h *UserHandler) StepUpMFA(c *gin.Context) {
	userID, sessionID, ok := currentSession(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	if h == nil || h.mfa == nil || h.sessions == nil || !h.requireCSRF(c) {
		response.ErrorDescriptor(c, apperror.IdentityCSRFInvalid, nil)
		return
	}
	var request mfaCodeRequest
	if err := requestutil.BindJSONStrict(c, &request); err != nil {
		response.ErrorDescriptor(c, apperror.IdentityMFAInvalid, nil)
		return
	}
	if err := h.mfa.VerifyCurrentFactor(c.Request.Context(), userID, request.Code); err != nil {
		response.WriteError(c, unavailableIfInternal(mfaErrorTranslator.Translate(err)))
		return
	}
	tokens, err := h.sessions.MarkMFA(c.Request.Context(), userID, sessionID)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, gin.H{"access_token": tokens.AccessToken, "token_type": "Bearer", "expires_in": tokens.ExpiresIn})
}

// GetMFAAdminPolicy returns aggregate readiness information only; individual
// user factor state belongs to the user account security endpoint.
// GET /api/v1/admin/identity/mfa-policy
func (h *UserHandler) GetMFAAdminPolicy(c *gin.Context) {
	if h == nil || h.mfa == nil {
		response.WriteError(c, unavailableError(apperror.IdentityMFAUnavailable))
		return
	}
	status, err := h.mfa.GetAdminPolicy(c.Request.Context())
	if err != nil {
		response.WriteError(c, unavailableIfInternal(mfaErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, status)
}

// UpdateMFAAdminPolicy applies a versioned and audited MFA enforcement policy.
// PUT /api/v1/admin/identity/mfa-policy
func (h *UserHandler) UpdateMFAAdminPolicy(c *gin.Context) {
	actorID, sessionID, ok := currentSession(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	if h == nil || h.mfa == nil || h.sessions == nil || !h.requireCSRF(c) {
		response.ErrorDescriptor(c, apperror.IdentityCSRFInvalid, nil)
		return
	}
	var request mfaPolicyRequest
	if err := requestutil.BindJSONStrict(c, &request); err != nil {
		response.ErrorDescriptor(c, apperror.IdentityMFAInvalid, nil)
		return
	}
	var graceEndsAt *time.Time
	if strings.TrimSpace(request.GraceEndsAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(request.GraceEndsAt))
		if err != nil {
			response.ErrorDescriptor(c, apperror.IdentityMFAInvalid, nil)
			return
		}
		stamp := parsed.UTC()
		graceEndsAt = &stamp
	}
	actorStatus, statusErr := h.mfa.Status(c.Request.Context(), actorID)
	if statusErr != nil {
		response.WriteError(c, unavailableIfInternal(mfaErrorTranslator.Translate(statusErr)))
		return
	}
	if actorStatus.Enabled {
		recent, recentErr := h.sessions.HasRecentMFA(c.Request.Context(), actorID, sessionID, h.mfa.StepUpTTL())
		if recentErr != nil {
			response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(recentErr)))
			return
		}
		if !recent {
			response.ErrorDescriptor(c, apperror.IdentityMFAStepUpRequired, nil)
			return
		}
	}
	status, err := h.mfa.UpdateAdminPolicy(c.Request.Context(), actorID, service.MFAAdminPolicyUpdate{
		Mode: request.Mode, GraceEndsAt: graceEndsAt, ExpectedVersion: request.ExpectedVersion,
	})
	if err != nil {
		response.WriteError(c, unavailableIfInternal(mfaErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, status)
}
