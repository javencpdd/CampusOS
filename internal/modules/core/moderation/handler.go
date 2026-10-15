package moderation

import (
	"context"
	"errors"
	"io"
	"net/http"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identitydelegation "github.com/campusos/CampusOS/internal/modules/core/identity/delegation"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/auth"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service        *Service
	strengthReader identityport.SessionStrengthReader
	userVerifier   identityport.SessionVerifier
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// SetSessionStrengthReader wires the identity session fact source used to
// build the delegation actor proof for admin grant routes.
func (h *Handler) SetSessionStrengthReader(reader identityport.SessionStrengthReader) {
	h.strengthReader = reader
}

func (h *Handler) Status(c *gin.Context) {
	response.Success(c, h.service.Status())
}

func (h *Handler) MyAccess(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	preview, err := h.service.PreviewForPrincipal(c.Request.Context(), c.Query("thread_id"), nil, h.currentPreviewPrincipal(c, userID))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"can_moderate": preview.Pin || preview.Lock, "actions": gin.H{"pin": preview.Pin, "lock": preview.Lock, "delete_post": false}, "thread_id": preview.ThreadID})
}

func (h *Handler) currentPreviewPrincipal(c *gin.Context, user string) identityport.SessionPrincipalResolver {
	claims := &auth.JWTClaims{UserID: user, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, h.userVerifier, h.strengthReader, claims)
	}
}

func (h *Handler) PreviewBatch(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var request struct {
		PostIDs []string `json:"post_ids"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	if err := requestutil.BindJSONStrict(c, &request); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "权限预览请求参数无效。")
		return
	}
	preview, err := h.service.PreviewForPrincipal(c.Request.Context(), c.Param("id"), request.PostIDs, h.currentPreviewPrincipal(c, userID))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, preview)
}

func (h *Handler) ListModerators(c *gin.Context) {
	items, err := h.service.ListModerators(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items), "status": h.service.Status()})
}

func (h *Handler) GetModerator(c *gin.Context) {
	item, err := h.service.GetModerator(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, item)
}

type SetModeratorRequest struct {
	CategoryIDs []string `json:"category_ids"`
}

func (h *Handler) SetModerator(c *gin.Context) {
	actorID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req SetModeratorRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "请求参数错误")
		return
	}
	item, err := h.service.SetModeratorCategories(c.Request.Context(), actorID, c.Param("id"), req.CategoryIDs, h.operationContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"message": "版主管理范围已保存", "assignment": item})
}

func (h *Handler) Pin(c *gin.Context) {
	h.setPinned(c, true)
}

func (h *Handler) Unpin(c *gin.Context) {
	h.setPinned(c, false)
}

func (h *Handler) setPinned(c *gin.Context, pinned bool) {
	operation := "unpin"
	if pinned {
		operation = "pin"
	}
	h.setCurrentThreadFlag(c, operation)
}

func (h *Handler) Lock(c *gin.Context) {
	h.setLocked(c, true)
}

func (h *Handler) Unlock(c *gin.Context) {
	h.setLocked(c, false)
}

func (h *Handler) setLocked(c *gin.Context, locked bool) {
	operation := "unlock"
	if locked {
		operation = "lock"
	}
	h.setCurrentThreadFlag(c, operation)
}

func (h *Handler) setCurrentThreadFlag(c *gin.Context, operation string) {
	actorID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if c.Request.Body != nil {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1))
		if err != nil || len(body) != 0 {
			response.Error(c, http.StatusBadRequest, 10001, "此操作不接受请求正文。")
			return
		}
	}
	thread, err := h.service.SetThreadFlagForPrincipal(c.Request.Context(), c.Param("id"), operation, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, h.userVerifier, h.strengthReader, &auth.JWTClaims{UserID: actorID, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType})
	}, h.operationContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, thread)
}

func (h *Handler) DeletePost(c *gin.Context) {
	actorID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if err := h.service.DeletePostForPrincipal(c.Request.Context(), c.Param("id"), c.Param("post_id"), func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, h.userVerifier, h.strengthReader, &auth.JWTClaims{UserID: actorID, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType})
	}, h.operationContext(c)); err != nil {
		writeError(c, err)
		return
	}
	response.NoContent(c)
}

func currentUserID(c *gin.Context) (string, bool) {
	value, ok := c.Get("user_id")
	if !ok {
		return "", false
	}
	userID, ok := value.(string)
	return userID, ok && userID != ""
}

func (h *Handler) operationContext(c *gin.Context) OperationContext {
	traceID, _ := c.Get("trace_id")
	traceIDText, _ := traceID.(string)
	operation := OperationContext{TraceID: traceIDText, IPAddress: c.ClientIP()}
	if sessionID := c.GetString("session_id"); sessionID != "" && h.strengthReader != nil {
		// The session record is the strength fact source; a lookup failure
		// leaves the proof empty and the delegation chain fails closed.
		if strength, err := h.strengthReader.AuthenticationStrengthForSession(c.Request.Context(), sessionID); err == nil {
			operation.AuthenticationStrength = strength
			operation.CredentialID = sessionID
		}
	}
	return operation
}

func writeError(c *gin.Context, err error) {
	var delegationDenied *identitydelegation.DelegationDeniedError
	switch {
	case errors.Is(err, communityport.ErrModerationPreviewInvalid):
		response.Error(c, http.StatusBadRequest, 10001, "权限预览只接受当前主题下最多50条不同的回复。")
	case errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable):
		response.Error(c, 401, 20002, "请重新登录后执行治理操作。")
	case errors.Is(err, identityport.ErrPolicyFactsChanged):
		response.Error(c, 409, 10004, "内容或执行授权已变化，请重新加载。")
	case errors.Is(err, communityport.ErrPostGovernanceDenied):
		response.Error(c, 403, 20004, "当前账号没有该版块的有效回复删除执行授权。")
	case errors.Is(err, communityport.ErrPostGovernanceUnavailable), errors.Is(err, ErrPostGovernanceUnavailable):
		response.Error(c, 503, 71000, "暂时无法检查回复治理权限。")
	case errors.Is(err, communityport.ErrGovernanceCommandDenied):
		response.Error(c, 403, 20004, "当前账号没有该版块的有效执行授权。")
	case errors.Is(err, communityport.ErrGovernanceCommandConflict):
		response.Error(c, 409, 10004, "内容状态已变化，请重新加载。")
	case errors.Is(err, ErrPluginDisabled):
		response.Error(c, http.StatusServiceUnavailable, 71001, "版主插件当前未运行；系统级插件启停需要重启 API 后生效")
	case errors.Is(err, ErrActionDisabled):
		response.Error(c, http.StatusForbidden, 71002, "该版主操作已被管理员关闭")
	case errors.Is(err, communityport.ErrGovernanceCommandUnavailable), errors.Is(err, identityport.ErrGovernanceAuthorityUnavailable):
		response.Error(c, 503, 71000, "暂时无法检查版主执行权限。")
	case errors.Is(err, ErrForbidden):
		response.Error(c, http.StatusForbidden, 20004, "你不是该板块的版主，不能执行此操作")
	case errors.Is(err, ErrInvalidScope), errors.Is(err, identityport.ErrInvalidScope):
		response.Error(c, http.StatusBadRequest, 71003, "板块范围参数无效")
	case errors.As(err, &delegationDenied):
		response.Error(c, http.StatusForbidden, 20004, "授予未通过委托上限校验："+delegationDenied.Reason)
	case errors.Is(err, identityport.ErrUserNotFound):
		response.Error(c, http.StatusNotFound, 30004, "目标用户不存在")
	case errors.Is(err, communityport.ErrCategoryNotFound):
		response.Error(c, http.StatusNotFound, 50004, "目标板块不存在")
	case errors.Is(err, communityport.ErrThreadNotFound):
		response.Error(c, http.StatusNotFound, 40003, "目标帖子不存在")
	case errors.Is(err, communityport.ErrPostNotFound):
		response.Error(c, http.StatusNotFound, 40004, "目标回复不存在")
	default:
		response.Error(c, http.StatusInternalServerError, 71000, "版主管理操作失败")
	}
}

func (h *Handler) SetUserSessionVerifier(verifier identityport.SessionVerifier) {
	h.userVerifier = verifier
}
