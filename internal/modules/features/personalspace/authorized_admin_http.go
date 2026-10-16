package space

import (
	"context"
	"errors"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/auth"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
)

func (h *Handler) adminSpaceTransportReady(c *gin.Context) bool {
	if corestorage.NilImageAdapter(h.adminAuthority) {
		writeSpaceAdminError(c, ErrSpaceFactsUnavailable)
		return false
	}
	return true
}
func bindSpaceAdminReason(c *gin.Context, dst any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := requestutil.BindJSONStrictOptional(c, dst); err != nil {
		response.Error(c, 400, 10001, "禁用请求格式无效，原因最多 500 字。")
		return false
	}
	return true
}
func spaceAdminNoBody(c *gin.Context) bool {
	if data, err := io.ReadAll(io.LimitReader(c.Request.Body, 1)); err != nil || len(data) > 0 {
		response.Error(c, 400, 10001, "此空间管理接口不接受请求正文。")
		return false
	}
	return true
}

func (h *Handler) SetAdminSessionAuthority(reader identityport.AdminSessionAuthorityReader) {
	h.adminAuthority = reader
}
func (h *Handler) adminPrincipal(c *gin.Context) identityport.SessionPrincipalResolver {
	id := c.GetString("admin_account_id")
	claims := &auth.JWTClaims{UserID: id, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		if id == "" || c.GetString("user_id") != id {
			return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
		}
		return identityport.ResolveAdminSessionPrincipal(ctx, h.adminAuthority, claims)
	}
}
func writeSpaceAdminError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identityport.ErrAdminSessionPrincipalUnavailable):
		response.Error(c, 401, 20002, "请重新登录管理端后重试。")
	case errors.Is(err, identityport.ErrPolicyFactsChanged):
		response.Error(c, 409, 10004, "空间或管理权限已变化，请重新加载。")
	case errors.Is(err, corestorage.ErrQuotaFactsChanged):
		response.Error(c, 409, 10004, "配额已变化，请重新加载后重试。")
	case errors.Is(err, ErrStorageQuotaInvalid):
		response.Error(c, 400, 10001, "请输入 1 MB 至 100 GB 的有效空间配额。")
	case errors.Is(err, ErrSpaceAdminDenied):
		response.Error(c, 403, 20003, "当前管理员无权执行此操作，或需要重新验证 MFA。")
	case errors.Is(err, ErrSpaceOwnerInvalid):
		response.Error(c, 400, 10001, "禁用原因不能超过 500 字。")
	case errors.Is(err, identityport.ErrUserNotFound), errors.Is(err, ErrSpaceNotFound):
		response.Error(c, 404, 75004, "用户空间不存在。")
	default:
		response.Error(c, 503, 10006, "暂时无法检查空间管理权限。")
	}
}
