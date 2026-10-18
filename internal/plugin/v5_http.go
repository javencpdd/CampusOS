package plugin

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"strconv"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type V5HTTPHandler struct {
	Platform      *V5Platform
	AdminSessions identity.AdminSessionAuthorityReader
	UserSessions  identity.SessionVerifier
	UserStrength  identity.SessionStrengthReader
}

func (h *V5HTTPHandler) resolver(c *gin.Context, admin bool) identity.SessionPrincipalResolver {
	id := c.GetString("user_id")
	if admin {
		id = c.GetString("admin_account_id")
	}
	claims := &auth.JWTClaims{UserID: id, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identity.PrincipalContext, error) {
		if h == nil {
			return identity.PrincipalContext{}, ErrV5PlatformUnavailable
		}
		if admin {
			return identity.ResolveAdminSessionPrincipal(ctx, h.AdminSessions, claims)
		}
		return identity.ResolveUserSessionPrincipal(ctx, h.UserSessions, h.UserStrength, claims)
	}
}
func v5HTTPError(c *gin.Context, err error) {
	descriptor := apperror.PluginV5Unavailable
	switch {
	case errors.Is(err, ErrV5Invalid), errors.Is(err, ErrPluginV5PackageInvalid), errors.Is(err, ErrPluginV5ManifestInvalid):
		descriptor = apperror.PluginV5Invalid
	case errors.Is(err, ErrV5NotFound):
		descriptor = apperror.PluginV5NotFound
	case errors.Is(err, ErrV5Conflict), errors.Is(err, identity.ErrPolicyFactsChanged):
		descriptor = apperror.PluginV5Conflict
	case errors.Is(err, ErrV5Rollback):
		descriptor = apperror.PluginV5Rollback
	case errors.Is(err, ErrV5Denied), errors.Is(err, identity.ErrAdminSessionPrincipalUnavailable), errors.Is(err, identity.ErrUserSessionPrincipalUnavailable):
		descriptor = apperror.PluginV5Denied
	case errors.Is(err, ErrPluginV5PackageConflict):
		descriptor = apperror.PluginV5PackageConflict
	case errors.Is(err, ErrPluginV5PackageSignature):
		descriptor = apperror.PluginV5PackageSignature
	case errors.Is(err, ErrPluginV5PackageDigest):
		descriptor = apperror.PluginV5PackageDigest
	}
	response.ErrorDescriptor(c, descriptor, nil)
}

func v5CSRF(c *gin.Context, admin bool) bool {
	cookie, header, refresh := "campusos_csrf", "X-CSRF-Token", "campusos_refresh"
	if admin {
		cookie, header, refresh = "campusos_admin_csrf", "X-Admin-CSRF-Token", "campusos_admin_refresh"
	}
	_, cookieErr := c.Cookie(refresh)
	browser := c.GetHeader("X-CampusOS-Client") == "browser" || c.GetHeader("Origin") != "" || c.GetHeader("Sec-Fetch-Site") != "" || cookieErr == nil
	if !browser {
		return true
	}
	a, e := c.Cookie(cookie)
	b := c.GetHeader(header)
	if e != nil || len(a) < 16 || len(a) > 256 || subtle.ConstantTimeCompare([]byte(a), []byte(b)) != 1 {
		v5HTTPError(c, ErrV5Denied)
		return false
	}
	return true
}
func v5HTTPStart(c *gin.Context) bool {
	c.Header("Cache-Control", "private, no-store")
	if len(c.Request.URL.RawQuery) > 1024 {
		v5HTTPError(c, ErrV5Invalid)
		return false
	}
	return true
}
func (h *V5HTTPHandler) Inventory(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !v5HTTPStart(c) {
			return
		}
		if h == nil || h.Platform == nil {
			v5HTTPError(c, ErrV5PlatformUnavailable)
			return
		}
		limit := 25
		if raw := c.Query("limit"); raw != "" {
			var err error
			limit, err = strconv.Atoi(raw)
			if err != nil {
				v5HTTPError(c, ErrV5Invalid)
				return
			}
		}
		out, err := h.Platform.Inventory(c.Request.Context(), admin, c.Query("after"), limit, h.resolver(c, admin))
		if err != nil {
			v5HTTPError(c, err)
			return
		}
		response.Success(c, out)
	}
}
func (h *V5HTTPHandler) Detail(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !v5HTTPStart(c) {
			return
		}
		if h == nil || h.Platform == nil {
			v5HTTPError(c, ErrV5PlatformUnavailable)
			return
		}
		out, err := h.Platform.Detail(c.Request.Context(), V5Coordinate{c.Param("publisher"), c.Param("key"), c.Param("version")}, admin, h.resolver(c, admin))
		if err != nil {
			v5HTTPError(c, err)
			return
		}
		response.Success(c, out)
	}
}
func (h *V5HTTPHandler) Operation(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !v5HTTPStart(c) {
			return
		}
		if h == nil || h.Platform == nil {
			v5HTTPError(c, ErrV5PlatformUnavailable)
			return
		}
		out, err := h.Platform.Operation(c.Request.Context(), c.Param("id"), admin, h.resolver(c, admin))
		if err != nil {
			v5HTTPError(c, err)
			return
		}
		response.Success(c, out)
	}
}
func (h *V5HTTPHandler) Mutate(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !v5HTTPStart(c) || !v5CSRF(c, admin) {
			return
		}
		if h == nil || h.Platform == nil {
			v5HTTPError(c, ErrV5PlatformUnavailable)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(c.Request.Body, PluginV5ManifestMaxBytes+1))
		if err != nil {
			v5HTTPError(c, ErrV5Invalid)
			return
		}
		parsed, ok := v5JSON(raw)
		allowed := map[string]bool{}
		for _, key := range []string{"kind", "version", "expected_generation", "expected_revision", "idempotency_key", "approved", "consumes", "host_resources", "accepted", "purpose_hash", "values", "secret_refs", "purge_data", "operation_id"} {
			allowed[key] = true
		}
		object, isObject := parsed.(map[string]any)
		if !isObject {
			ok = false
		}
		for key := range object {
			if !allowed[key] {
				ok = false
			}
		}
		for _, key := range []string{"expected_generation", "expected_revision"} {
			if value, exists := object[key]; exists {
				wire, isString := value.(string)
				if !isString || !v5String(1, 19, `^(0|[1-9][0-9]*)$`)(wire) {
					ok = false
				}
			}
		}
		if !ok {
			v5HTTPError(c, ErrV5Invalid)
			return
		}
		var request V5Mutation
		if strictPluginV5JSON(raw, &request) != nil {
			v5HTTPError(c, ErrV5Invalid)
			return
		}
		op, err := h.Platform.Mutate(c.Request.Context(), c.Param("publisher"), c.Param("key"), request, admin, h.resolver(c, admin))
		if err != nil {
			v5HTTPError(c, err)
			return
		}
		response.Success(c, V5PublicOperation(op))
	}
}
func (h *V5HTTPHandler) Import(c *gin.Context) {
	if !v5HTTPStart(c) || !v5CSRF(c, true) {
		return
	}
	if h == nil || h.Platform == nil {
		v5HTTPError(c, ErrV5PlatformUnavailable)
		return
	}
	// Authorize before consuming a large upload. MultipartReader never spills to disk.
	if _, err := h.Platform.actor(c.Request.Context(), true, "import", h.resolver(c, true), false); err != nil {
		v5HTTPError(c, err)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, PluginV5MaxPackageBytes+PluginV5ManifestMaxBytes+(64<<10))
	reader, err := c.Request.MultipartReader()
	if err != nil {
		v5HTTPError(c, ErrV5Invalid)
		return
	}
	parts := map[string][]byte{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			v5HTTPError(c, ErrV5Invalid)
			return
		}
		name := part.FormName()
		max := int64(PluginV5ManifestMaxBytes)
		if name == "package" {
			max = PluginV5MaxPackageBytes
		} else if name != "release" && name != "idempotency_key" {
			v5HTTPError(c, ErrV5Invalid)
			return
		}
		if _, exists := parts[name]; exists {
			v5HTTPError(c, ErrV5Invalid)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(part, max+1))
		part.Close()
		if err != nil || int64(len(raw)) > max {
			v5HTTPError(c, ErrV5Invalid)
			return
		}
		parts[name] = raw
	}
	op, err := h.Platform.Import(c.Request.Context(), parts["package"], parts["release"], string(parts["idempotency_key"]), h.resolver(c, true))
	if err != nil {
		v5HTTPError(c, err)
		return
	}
	response.Success(c, V5PublicOperation(op))
}

// Retired endpoints return a stable closed boundary; they cannot mutate the
// legacy Manager or import old grants into the authoritative v5 model.
func V5RetiredHTTP(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	response.ErrorDescriptor(c, apperror.PluginV5LegacyRetired, nil)
}
