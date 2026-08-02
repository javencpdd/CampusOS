package handler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	platformversion "github.com/campusos/CampusOS/internal/platform/version"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/auth"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// UserHandler 用户 HTTP 处理器
type UserHandler struct {
	svc         *service.UserService
	challenges  *service.ChallengeService
	sessions    *service.SessionService
	recovery    *service.RecoveryService
	adminAccess *service.AdminAccessService
	mfa         *service.MFAService
	sessionHTTP SessionHTTPConfig
}

const (
	refreshCookieName = "campusos_refresh"
	csrfCookieName    = "campusos_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

// SessionHTTPConfig controls transport behavior only. It contains no signing,
// refresh, or credential material.
type SessionHTTPConfig struct {
	RefreshBodyCompat bool
	CookieSecure      bool
}

// NewUserHandler 创建用户处理器
func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

func (h *UserHandler) SetChallengeService(challenges *service.ChallengeService) {
	h.challenges = challenges
}

func (h *UserHandler) SetSessionService(sessions *service.SessionService, config SessionHTTPConfig) {
	h.sessions = sessions
	h.sessionHTTP = config
}

func (h *UserHandler) SetRecoveryService(recovery *service.RecoveryService) {
	h.recovery = recovery
}

func (h *UserHandler) SetAdminAccessService(adminAccess *service.AdminAccessService) {
	h.adminAccess = adminAccess
}

func (h *UserHandler) SetMFAService(mfa *service.MFAService) {
	h.mfa = mfa
}

// RequestRegistrationChallenge begins the verified registration flow without
// revealing whether a future email address is already associated with an
// account. Duplicate registration is resolved only by the final command.
// POST /api/v1/auth/registration/challenge
func (h *UserHandler) RequestRegistrationChallenge(c *gin.Context) {
	if h.challenges == nil {
		response.WriteError(c, unavailableError(apperror.IdentityChallengeUnavailable))
		return
	}
	var req domain.RegistrationChallengeRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	receipt, err := h.challenges.Request(c.Request.Context(), domain.ChallengeRequest{
		Purpose:  domain.ChallengePurposeRegistration,
		Email:    req.Email,
		ClientIP: c.ClientIP(),
	})
	if err != nil {
		response.WriteError(c, unavailableIfInternal(challengeErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, receipt)
}

// VerifyRegistrationChallenge validates a code and returns the short-lived
// Ticket that the final registration command consumes exactly once.
// POST /api/v1/auth/registration/verify
func (h *UserHandler) VerifyRegistrationChallenge(c *gin.Context) {
	if h.challenges == nil {
		response.WriteError(c, unavailableError(apperror.IdentityChallengeUnavailable))
		return
	}
	var req domain.RegistrationChallengeVerificationRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	ticket, err := h.challenges.Verify(c.Request.Context(), domain.ChallengeVerificationRequest{
		PublicID: req.ChallengeID,
		Purpose:  domain.ChallengePurposeRegistration,
		Code:     req.Code,
	})
	if err != nil {
		response.WriteError(c, unavailableIfInternal(challengeErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, ticket)
}

// RequestPasswordReset always returns the same accepted envelope for an
// unknown, legacy, suspended, or eligible account. The opaque challenge ID is
// usable only when a matching code was delivered to a verified email address.
// POST /api/v1/auth/password-reset/challenge
func (h *UserHandler) RequestPasswordReset(c *gin.Context) {
	if h.recovery == nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "account recovery is unavailable")
		return
	}
	var req domain.PasswordResetChallengeRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request")
		return
	}
	result, err := h.recovery.RequestPasswordReset(c.Request.Context(), req.Email, c.ClientIP())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "account recovery is temporarily unavailable")
		return
	}
	response.Success(c, result)
}

// VerifyPasswordReset validates an emailed code and exposes only the short
// lived opaque Ticket required by the next, atomic completion command.
// POST /api/v1/auth/password-reset/verify
func (h *UserHandler) VerifyPasswordReset(c *gin.Context) {
	if h.recovery == nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "account recovery is unavailable")
		return
	}
	var req domain.PasswordResetVerificationRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request")
		return
	}
	ticket, err := h.recovery.VerifyPasswordReset(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10009, "account recovery verification is invalid or expired")
		return
	}
	response.Success(c, ticket)
}

// CompletePasswordReset changes the credential, invalidates every existing
// login, and consumes the Ticket in a single reliable command.
// POST /api/v1/auth/password-reset/complete
func (h *UserHandler) CompletePasswordReset(c *gin.Context) {
	if h.recovery == nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "account recovery is unavailable")
		return
	}
	var req domain.PasswordResetCompletionRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request")
		return
	}
	if err := h.recovery.CompletePasswordReset(c.Request.Context(), req); err != nil {
		response.Error(c, http.StatusBadRequest, 10009, "account recovery is invalid or expired")
		return
	}
	h.clearSessionCookies(c)
	response.Success(c, gin.H{"reset": true, "relogin_required": true})
}

// RequestEmailBinding starts a verified personal-email transition for the
// currently authenticated user. System-managed accounts are intentionally
// excluded from this public path.
// POST /api/v1/auth/email-binding/challenge
func (h *UserHandler) RequestEmailBinding(c *gin.Context) {
	if h.recovery == nil || !h.requireCSRF(c) {
		response.Error(c, http.StatusForbidden, 20004, "csrf validation failed")
		return
	}
	userID, _, ok := currentSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req domain.EmailBindingChallengeRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request")
		return
	}
	receipt, err := h.recovery.RequestEmailBinding(c.Request.Context(), userID, req, c.ClientIP())
	if errors.Is(err, service.ErrChallengeRateLimited) {
		response.Error(c, http.StatusTooManyRequests, 10010, "email verification request is temporarily limited")
		return
	}
	if err != nil {
		response.Error(c, http.StatusConflict, 10004, "email cannot be bound to this account")
		return
	}
	response.Success(c, receipt)
}

// VerifyEmailBinding validates the binding code while the user remains
// authenticated. Completing the next step will revoke all current sessions.
// POST /api/v1/auth/email-binding/verify
func (h *UserHandler) VerifyEmailBinding(c *gin.Context) {
	if h.recovery == nil || !h.requireCSRF(c) {
		response.Error(c, http.StatusForbidden, 20004, "csrf validation failed")
		return
	}
	if _, _, ok := currentSession(c); !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req domain.EmailBindingVerificationRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request")
		return
	}
	ticket, err := h.recovery.VerifyEmailBinding(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10009, "email verification is invalid or expired")
		return
	}
	response.Success(c, ticket)
}

// CompleteEmailBinding commits the authoritative account email and its
// compatibility projection, then forces a fresh login under the new identity.
// POST /api/v1/auth/email-binding/complete
func (h *UserHandler) CompleteEmailBinding(c *gin.Context) {
	if h.recovery == nil || !h.requireCSRF(c) {
		response.Error(c, http.StatusForbidden, 20004, "csrf validation failed")
		return
	}
	userID, _, ok := currentSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req domain.EmailBindingCompletionRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request")
		return
	}
	if err := h.recovery.CompleteEmailBinding(c.Request.Context(), userID, req); err != nil {
		response.Error(c, http.StatusBadRequest, 10009, "email binding is invalid or expired")
		return
	}
	h.clearSessionCookies(c)
	response.Success(c, gin.H{"bound": true, "relogin_required": true})
}

// CompleteAdminRecovery is public only because the user proves possession of
// a code delivered to the independently verified address. It reveals no Case
// details and can neither read nor set a password from the Admin surface.
// POST /api/v1/auth/recovery/complete
func (h *UserHandler) CompleteAdminRecovery(c *gin.Context) {
	if h.recovery == nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "account recovery is unavailable")
		return
	}
	var req domain.RecoveryCaseCompletionRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request")
		return
	}
	if err := h.recovery.CompleteAdminRecoveryCase(c.Request.Context(), req); err != nil {
		response.Error(c, http.StatusBadRequest, 10009, "account recovery is invalid or expired")
		return
	}
	h.clearSessionCookies(c)
	response.Success(c, gin.H{"reset": true, "relogin_required": true})
}

// ListAdminRecoveryCases exposes masked workflow metadata only.
// GET /api/v1/admin/identity/recovery-cases
func (h *UserHandler) ListAdminRecoveryCases(c *gin.Context) {
	if h.recovery == nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "account recovery is unavailable")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := h.recovery.ListAdminRecoveryCases(c.Request.Context(), limit)
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "recovery cases are unavailable")
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

// CreateAdminRecoveryCase records a proof reference, not the proof itself,
// and issues a Challenge to the newly supplied address.
// POST /api/v1/admin/identity/recovery-cases
func (h *UserHandler) CreateAdminRecoveryCase(c *gin.Context) {
	if h.recovery == nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "account recovery is unavailable")
		return
	}
	actorID, ok := currentRoleActorID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req domain.AdminRecoveryCaseCreateRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request")
		return
	}
	caseView, err := h.recovery.CreateAdminRecoveryCase(c.Request.Context(), actorID, req, c.ClientIP())
	if err != nil {
		response.Error(c, http.StatusConflict, 10004, "account recovery case cannot be created")
		return
	}
	response.Created(c, caseView)
}

// CancelAdminRecoveryCase stops a pending assisted recovery before the Ticket
// is consumed. The request body is intentionally unnecessary: the command
// audit records the actor and target without retaining subjective proof text.
// POST /api/v1/admin/identity/recovery-cases/:id/cancel
func (h *UserHandler) CancelAdminRecoveryCase(c *gin.Context) {
	if h.recovery == nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "account recovery is unavailable")
		return
	}
	actorID, ok := currentRoleActorID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if err := h.recovery.CancelAdminRecoveryCase(c.Request.Context(), actorID, c.Param("id")); err != nil {
		response.Error(c, http.StatusConflict, 10004, "account recovery case cannot be cancelled")
		return
	}
	response.Success(c, gin.H{"cancelled": true})
}

// ListAdminUserSessions returns safe device/session projections only.
// GET /api/v1/admin/identity/users/:id/sessions
func (h *UserHandler) ListAdminUserSessions(c *gin.Context) {
	if h.recovery == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	userID, ok := numericIdentityID(c.Param("id"))
	if !ok {
		response.Error(c, http.StatusBadRequest, 10001, "invalid user id")
		return
	}
	items, err := h.recovery.ListUserSessions(c.Request.Context(), userID)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

// RevokeAdminUserSessions invalidates both persisted sessions and existing
// access JWTs through the user's auth-version.
// POST /api/v1/admin/identity/users/:id/sessions/revoke-all
func (h *UserHandler) RevokeAdminUserSessions(c *gin.Context) {
	if h.recovery == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	actorID, actorOK := currentRoleActorID(c)
	userID, userOK := numericIdentityID(c.Param("id"))
	if !actorOK {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if !userOK {
		response.Error(c, http.StatusBadRequest, 10001, "invalid user id")
		return
	}
	if err := h.recovery.RevokeUserSessionsByAdmin(c.Request.Context(), actorID, userID); err != nil {
		response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, gin.H{"revoked": true})
}

// Register 用户注册
// POST /api/v1/auth/register
func (h *UserHandler) Register(c *gin.Context) {
	var req domain.RegistrationRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request")
		return
	}
	if strings.TrimSpace(req.ChallengeID) == "" || strings.TrimSpace(req.Ticket) == "" {
		response.ErrorDescriptor(c, apperror.IdentityRegistrationVerificationRequired, nil)
		return
	}

	user, err := h.svc.RegisterVerified(c.Request.Context(), req)
	if err != nil {
		response.WriteError(c, registrationErrorTranslator.Translate(err))
		return
	}

	response.Created(c, user)
}

// Login 用户登录
// POST /api/v1/auth/login
func (h *UserHandler) Login(c *gin.Context) {
	h.login(c, false)
}

// AdminLogin uses the same credential and session authority as the user
// surface, then requires an independent active management-plane account before
// issuing tokens to the Admin client.
// POST /api/v1/auth/admin/login
func (h *UserHandler) AdminLogin(c *gin.Context) {
	h.login(c, true)
}

func (h *UserHandler) login(c *gin.Context, requireAdmin bool) {
	var req domain.LoginRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}

	if h.sessions == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	user, err := h.svc.Authenticate(c.Request.Context(), req)
	if err != nil {
		// Login intentionally does not distinguish missing accounts, bad
		// passwords, inactive accounts, or unverified account state.
		response.ErrorDescriptor(c, apperror.IdentityCredentialsInvalid, nil)
		return
	}
	if requireAdmin {
		if h.adminAccess == nil {
			response.WriteError(c, unavailableError(apperror.IdentityAdminLoginUnavailable))
			return
		}
		if err := h.adminAccess.Require(c.Request.Context(), user.ID); err != nil {
			// Do not reveal whether a valid user credential lacks management-plane
			// admission or whether an administrator account is suspended.
			response.ErrorDescriptor(c, apperror.IdentityAdminCredentialsInvalid, nil)
			return
		}
	}
	if h.mfa != nil {
		audience := domain.MFAAudienceWeb
		if requireAdmin {
			audience = domain.MFAAudienceAdmin
		}
		requirement, mfaErr := h.mfa.BeginLogin(c.Request.Context(), user.ID, audience)
		if mfaErr != nil {
			response.WriteError(c, unavailableIfInternal(mfaErrorTranslator.Translate(mfaErr)))
			return
		}
		if requirement != nil && requirement.Required {
			response.Success(c, requirement)
			return
		}
	}
	if requireAdmin {
		if err := h.adminAccess.RecordAuthentication(c.Request.Context(), user.ID); err != nil {
			response.ErrorDescriptor(c, apperror.IdentityAdminCredentialsInvalid, nil)
			return
		}
	}
	tokens, err := h.sessions.Issue(c.Request.Context(), user, h.sessionMetadata(c))
	if err != nil {
		response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(err)))
		return
	}
	h.writeSessionCookies(c, tokens)

	// 获取用户角色
	roles, _ := h.svc.GetUserRoles(c.Request.Context(), user.ID)

	response.Success(c, domain.LoginResponse{
		User:         user,
		Roles:        roles,
		AccessToken:  tokens.AccessToken,
		RefreshToken: h.compatRefreshToken(tokens.RefreshToken),
		TokenType:    "Bearer",
		ExpiresIn:    tokens.ExpiresIn,
	})
}

// Refresh exchanges a one-time opaque token for a new session row and short
// access JWT. Cookie clients must prove same-site intent with double-submit
// CSRF; an explicitly enabled legacy JSON body remains a temporary bridge.
// POST /api/v1/auth/refresh
func (h *UserHandler) Refresh(c *gin.Context) {
	if h.sessions == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	rawRefresh, fromCookie := h.readRefreshCookie(c)
	if fromCookie && !h.requireCSRF(c) {
		response.ErrorDescriptor(c, apperror.IdentityCSRFInvalid, nil)
		return
	}
	if rawRefresh == "" && h.sessionHTTP.RefreshBodyCompat {
		var request domain.RefreshRequest
		if err := decodeOptionalStrictJSON(c, &request); err != nil {
			response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
			return
		}
		rawRefresh = strings.TrimSpace(request.RefreshToken)
		if rawRefresh != "" {
			h.sessions.RecordRefreshBodyCompatibility(c.Request.Context())
		}
	}
	if rawRefresh == "" {
		response.ErrorDescriptor(c, apperror.IdentityRefreshInvalid, nil)
		return
	}
	tokens, err := h.sessions.Refresh(c.Request.Context(), rawRefresh, h.sessionMetadata(c))
	if err != nil {
		// Reuse is intentionally indistinguishable to an attacker. The Session
		// service has already revoked the family when it returns this branch.
		h.clearSessionCookies(c)
		response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(err)))
		return
	}
	h.writeSessionCookies(c, tokens)
	response.Success(c, domain.RefreshResponse{
		AccessToken: tokens.AccessToken, RefreshToken: h.compatRefreshToken(tokens.RefreshToken),
		TokenType: "Bearer", ExpiresIn: tokens.ExpiresIn,
	})
}

// Logout revokes only the authenticated session and clears browser cookies.
// POST /api/v1/auth/logout
func (h *UserHandler) Logout(c *gin.Context) {
	if h.sessions == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	if !h.requireCSRF(c) {
		response.ErrorDescriptor(c, apperror.IdentityCSRFInvalid, nil)
		return
	}
	userID, sessionID, ok := currentSession(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	if err := h.sessions.RevokeCurrent(c.Request.Context(), userID, sessionID); err != nil {
		response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(err)))
		return
	}
	h.clearSessionCookies(c)
	response.Success(c, gin.H{"revoked": true})
}

// LogoutAll immediately invalidates every access JWT for the account and
// revokes all persisted sessions. The current browser cookies are cleared.
// POST /api/v1/auth/logout-all
func (h *UserHandler) LogoutAll(c *gin.Context) {
	if h.sessions == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	if !h.requireCSRF(c) {
		response.ErrorDescriptor(c, apperror.IdentityCSRFInvalid, nil)
		return
	}
	userID, _, ok := currentSession(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	if _, err := h.sessions.RevokeAll(c.Request.Context(), userID, "logout_all", "identity.session.logout_all"); err != nil {
		response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(err)))
		return
	}
	h.clearSessionCookies(c)
	response.Success(c, gin.H{"revoked": true})
}

// ListSessions returns only safe session projections for the current account.
// GET /api/v1/auth/sessions
func (h *UserHandler) ListSessions(c *gin.Context) {
	if h.sessions == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	userID, sessionID, ok := currentSession(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	items, err := h.sessions.List(c.Request.Context(), userID, sessionID)
	if err != nil {
		response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(err)))
		return
	}
	response.Success(c, gin.H{"items": items})
}

// RevokeSession revokes one selected session owned by the authenticated user.
// DELETE /api/v1/auth/sessions/:id
func (h *UserHandler) RevokeSession(c *gin.Context) {
	if h.sessions == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	if !h.requireCSRF(c) {
		response.ErrorDescriptor(c, apperror.IdentityCSRFInvalid, nil)
		return
	}
	userID, currentID, ok := currentSession(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	targetID := strings.TrimSpace(c.Param("id"))
	if err := h.sessions.RevokeSession(c.Request.Context(), userID, targetID); err != nil {
		response.WriteError(c, unavailableIfInternal(sessionErrorTranslator.Translate(err)))
		return
	}
	if targetID == currentID {
		h.clearSessionCookies(c)
	}
	response.Success(c, gin.H{"revoked": true})
}

// GetMe 获取当前用户信息
// GET /api/v1/auth/me
func (h *UserHandler) GetMe(c *gin.Context) {
	// 从 JWT 中间件注入的 context 获取用户 ID
	userID, _ := c.Get("user_id")
	if userID == nil || userID == "" {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	user, err := h.svc.GetByID(c.Request.Context(), userID.(string))
	if err != nil {
		response.Error(c, http.StatusNotFound, 30004, "user not found")
		return
	}

	response.Success(c, user)
}

// GetUser 获取用户详情
// GET /api/v1/users/:id
func (h *UserHandler) GetUser(c *gin.Context) {
	id := c.Param("id")

	user, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusNotFound, 30004, "user not found")
		return
	}

	response.Success(c, user.Public())
}

// ListUsers 获取用户列表
// GET /api/v1/users
func (h *UserHandler) ListUsers(c *gin.Context) {
	page, pageSize, ok := response.ParsePagination(c, 20, 100)
	if !ok {
		return
	}

	users, total, err := h.svc.ListUsers(c.Request.Context(), page, pageSize)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, "user list is unavailable")
		return
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	publicUsers := make([]domain.PublicUser, 0, len(users))
	for _, user := range users {
		publicUsers = append(publicUsers, user.Public())
	}
	response.List(c, publicUsers, &response.Pagination{
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
	})
}

// ListAdminUsers returns the management directory projection. The public
// /users route deliberately omits email, so Admin must use this separately
// authorized endpoint instead of weakening the public profile contract.
// GET /api/v1/admin/users
func (h *UserHandler) ListAdminUsers(c *gin.Context) {
	page, pageSize, ok := response.ParsePagination(c, 20, 100)
	if !ok {
		return
	}
	users, total, err := h.svc.ListUsers(c.Request.Context(), page, pageSize)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, "user list is unavailable")
		return
	}
	adminUsers := make([]domain.AdminUser, 0, len(users))
	for _, user := range users {
		adminUsers = append(adminUsers, user.Admin())
	}
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}
	response.List(c, adminUsers, &response.Pagination{
		Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages,
	})
}

// UpdateUser 更新用户信息
// PUT /api/v1/users/:id
func (h *UserHandler) UpdateUser(c *gin.Context) {
	id := c.Param("id")
	actorID, exists := c.Get("user_id")
	if !exists || actorID != id {
		response.ErrorWithDetails(c, http.StatusForbidden, 20004, "users may only update their own profile", gin.H{"resource_user_id": id})
		return
	}

	var req domain.UpdateUserRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request")
		return
	}

	user, err := h.svc.UpdateUser(c.Request.Context(), id, req)
	if err != nil {
		response.Error(c, http.StatusNotFound, 30004, "user not found")
		return
	}

	response.Success(c, user)
}

// SuspendUser 封禁用户
// POST /api/v1/users/:id/suspend
func (h *UserHandler) SuspendUser(c *gin.Context) {
	id := c.Param("id")
	user, err := h.svc.SuspendUser(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusNotFound, 30004, "user not found")
		return
	}
	response.Success(c, user)
}

// ActivateUser 解封用户
// POST /api/v1/users/:id/activate
func (h *UserHandler) ActivateUser(c *gin.Context) {
	id := c.Param("id")
	user, err := h.svc.ActivateUser(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusNotFound, 30004, "user not found")
		return
	}
	response.Success(c, user)
}

// ListRoles 获取角色列表
// GET /api/v1/roles
func (h *UserHandler) ListRoles(c *gin.Context) {
	response.Success(c, gin.H{"message": "roles list - use permission handler"})
}

// HealthCheck 健康检查
// GET /api/v1/health
func (h *UserHandler) HealthCheck(c *gin.Context) {
	response.Success(c, gin.H{
		"status":  "ok",
		"service": "CampusOS",
		"version": platformversion.Number,
	})
}

func (h *UserHandler) sessionMetadata(c *gin.Context) service.SessionMetadata {
	return service.SessionMetadata{
		DeviceID: c.GetHeader("X-Device-ID"), DeviceName: c.GetHeader("X-Device-Name"), DeviceType: c.GetHeader("X-Device-Type"),
		ClientIP: c.ClientIP(), UserAgent: c.GetHeader("User-Agent"),
	}
}

func (h *UserHandler) readRefreshCookie(c *gin.Context) (string, bool) {
	value, err := c.Cookie(refreshCookieName)
	if err != nil || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func (h *UserHandler) writeSessionCookies(c *gin.Context, tokens *service.SessionTokens) {
	if tokens == nil || tokens.Session == nil || tokens.RefreshToken == "" {
		return
	}
	maxAge := int(time.Until(tokens.Session.ExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	csrf, err := auth.NewOpaqueRefreshToken()
	if err != nil {
		return
	}
	base := http.Cookie{
		Path: "/api/v1/auth", MaxAge: maxAge, Expires: tokens.Session.ExpiresAt,
		Secure: h.sessionHTTP.CookieSecure, SameSite: http.SameSiteLaxMode,
	}
	refresh := base
	refresh.Name = refreshCookieName
	refresh.Value = tokens.RefreshToken
	refresh.HttpOnly = true
	http.SetCookie(c.Writer, &refresh)
	csrfCookie := base
	csrfCookie.Name = csrfCookieName
	csrfCookie.Value = csrf
	// The browser app can live at / while the API cookie is scoped to
	// /api/v1/auth, so the non-sensitive CSRF cookie needs a readable path.
	csrfCookie.Path = "/"
	csrfCookie.HttpOnly = false
	http.SetCookie(c.Writer, &csrfCookie)
}

func (h *UserHandler) clearSessionCookies(c *gin.Context) {
	for _, name := range []string{refreshCookieName, csrfCookieName} {
		path := "/api/v1/auth"
		if name == csrfCookieName {
			path = "/"
		}
		http.SetCookie(c.Writer, &http.Cookie{
			Name: name, Value: "", Path: path, MaxAge: -1, Expires: time.Unix(1, 0),
			Secure: h.sessionHTTP.CookieSecure, HttpOnly: name == refreshCookieName, SameSite: http.SameSiteLaxMode,
		})
	}
}

func (h *UserHandler) requireCSRF(c *gin.Context) bool {
	cookie, err := c.Cookie(csrfCookieName)
	header := strings.TrimSpace(c.GetHeader(csrfHeaderName))
	if err != nil || cookie == "" || header == "" || len(cookie) != len(header) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) == 1
}

func (h *UserHandler) compatRefreshToken(value string) string {
	if h.sessionHTTP.RefreshBodyCompat {
		return value
	}
	return ""
}

func currentSession(c *gin.Context) (string, string, bool) {
	userID, userExists := c.Get("user_id")
	sessionID, sessionExists := c.Get("session_id")
	user, userOK := userID.(string)
	session, sessionOK := sessionID.(string)
	return user, session, userExists && sessionExists && userOK && sessionOK && user != "" && session != ""
}

func numericIdentityID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	parsed, err := strconv.ParseInt(value, 10, 64)
	return value, err == nil && parsed > 0
}

func decodeOptionalStrictJSON(c *gin.Context, value any) error {
	if c.Request.Body == nil {
		return nil
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(value)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request body contains multiple JSON values")
	}
	return nil
}
