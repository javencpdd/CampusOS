package handler

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

const adminRefreshCookieName = "campusos_admin_refresh"
const adminCSRFCookieName = "campusos_admin_csrf"
const adminCSRFHeaderName = "X-Admin-CSRF-Token"
const adminCookiePath = "/api/v1/admin/identity"

func (h *AdminDomainHandler) SetSessionHTTPConfig(config SessionHTTPConfig) { h.sessionHTTP = config }
func (h *AdminDomainHandler) browserSession(c *gin.Context) bool {
	_, err := c.Cookie(adminRefreshCookieName)
	return c.GetHeader("X-CampusOS-Client") == "browser" || c.GetHeader("Origin") != "" || c.GetHeader("Sec-Fetch-Site") != "" || err == nil
}
func (h *AdminDomainHandler) requireBrowserCSRF(c *gin.Context) bool {
	if !h.browserSession(c) {
		return true
	}
	cookie, err := c.Cookie(adminCSRFCookieName)
	header := strings.TrimSpace(c.GetHeader(adminCSRFHeaderName))
	if err != nil || cookie == "" || header == "" || len(cookie) != len(header) || subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) != 1 {
		response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
		return false
	}
	return true
}
func (h *AdminDomainHandler) clearBrowserCookies(c *gin.Context) {
	for _, name := range []string{adminRefreshCookieName, adminCSRFCookieName} {
		path := adminCookiePath
		if name == adminCSRFCookieName {
			path = "/"
		}
		http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1, Expires: time.Unix(1, 0), Secure: h.sessionHTTP.CookieSecure, HttpOnly: name == adminRefreshCookieName, SameSite: http.SameSiteLaxMode})
	}
}
func (h *AdminDomainHandler) writeBrowserTokens(c *gin.Context, tokens *service.AdminSessionTokens) {
	if tokens == nil || tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.RefreshExpiresAt.IsZero() {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	csrf, err := auth.NewOpaqueRefreshToken()
	if err != nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	maxAge := int(time.Until(tokens.RefreshExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	base := http.Cookie{Path: adminCookiePath, MaxAge: maxAge, Expires: tokens.RefreshExpiresAt, Secure: h.sessionHTTP.CookieSecure, SameSite: http.SameSiteLaxMode}
	refresh := base
	refresh.Name = adminRefreshCookieName
	refresh.Value = tokens.RefreshToken
	refresh.HttpOnly = true
	http.SetCookie(c.Writer, &refresh)
	csrfCookie := base
	csrfCookie.Name = adminCSRFCookieName
	csrfCookie.Value = csrf
	csrfCookie.Path = "/"
	http.SetCookie(c.Writer, &csrfCookie)
	value := adminTokenResponse{AccessToken: tokens.AccessToken, TokenType: "Bearer", ExpiresAt: tokens.ExpiresAt}
	if h.sessionHTTP.RefreshBodyCompat && !h.browserSession(c) {
		value.RefreshToken = tokens.RefreshToken
	}
	response.Success(c, value)
}
func (h *AdminDomainHandler) currentAdminResolver(c *gin.Context) identityport.SessionPrincipalResolver {
	// Host context is written only by independent Admin JWT/session ingress.
	// Query strings, user_id aliases, headers and body actor fields are unused.
	claims := &auth.JWTClaims{UserID: c.GetString("admin_account_id"), SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, h.sessions, claims)
	}
}
func (h *AdminDomainHandler) Me(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1)); err != nil || len(body) > 0 {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	if h.currentSelf == nil {
		response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		return
	}
	profile, err := h.currentSelf.CurrentProfile(c.Request.Context(), h.currentAdminResolver(c))
	if err != nil {
		switch {
		case errors.Is(err, identityport.ErrPolicyFactsChanged):
			response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
		case errors.Is(err, service.ErrAdminSelfUnavailable):
			response.WriteError(c, unavailableError(apperror.IdentitySessionUnavailable))
		default:
			response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		}
		return
	}
	response.Success(c, profile)
}
func (h *AdminDomainHandler) currentMFAActor(c *gin.Context) (service.AdminActor, bool) {
	c.Header("Cache-Control", "private, no-store")
	if !h.requireBrowserCSRF(c) {
		return service.AdminActor{}, false
	}
	if h.mfa == nil {
		response.WriteError(c, unavailableError(apperror.IdentityMFAUnavailable))
		return service.AdminActor{}, false
	}
	p, err := h.currentAdminResolver(c)(c.Request.Context())
	if err != nil {
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		return service.AdminActor{}, false
	}
	return service.AdminActor{ID: p.Actor.ID, CredentialID: p.CredentialID, MFAAuthenticated: p.AuthenticationStrength == "mfa"}, true
}

func writeAdminSelfError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAdminMFAActor):
		response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
	case errors.Is(err, service.ErrAdminMFANotEnabled):
		response.ErrorDescriptor(c, apperror.IdentityMFANotEnabled, nil)
	case errors.Is(err, service.ErrAdminMFAReplay):
		response.ErrorDescriptor(c, apperror.IdentityMFAReplay, nil)
	case errors.Is(err, service.ErrAdminMFACodeInvalid):
		response.ErrorDescriptor(c, apperror.IdentityMFAInvalid, nil)
	case errors.Is(err, identityport.ErrPolicyFactsChanged):
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
	case errors.Is(err, service.ErrAdminSessionInvalid), errors.Is(err, identityport.ErrAdminSessionPrincipalUnavailable):
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
	default:
		response.WriteError(c, unavailableError(apperror.IdentityMFAUnavailable))
	}
}
