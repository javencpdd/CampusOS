package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/auth"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type PostHandler struct {
	svc            *service.PostService
	ownerVerifier  identityport.SessionVerifier
	ownerStrength  identityport.SessionStrengthReader
	adminAuthority identityport.AdminSessionAuthorityReader
}

func NewPostHandler(svc *service.PostService) *PostHandler {
	return &PostHandler{svc: svc}
}

func (h *PostHandler) CreatePost(c *gin.Context) {
	threadID := c.Param("id")
	_, username, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req domain.CreatePostRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}

	post, err := h.svc.CreatePostForPrincipal(c.Request.Context(), threadID, username, req, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveOwnerPrincipal(ctx, c)
	})
	if err != nil {
		writePostAuthorError(c, err)
		return
	}
	response.Created(c, post)
}

func (h *PostHandler) UpdatePost(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	postID := c.Param("post_id")

	var req struct {
		Content string `json:"content" binding:"required,min=1"`
	}
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	post, err := h.svc.UpdatePostForPrincipal(c.Request.Context(), postID, req.Content, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveOwnerPrincipal(ctx, c)
	})
	if err != nil {
		writePostAuthorError(c, err)
		return
	}
	response.Success(c, post)
}

func (h *PostHandler) DeletePost(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	postID := c.Param("post_id")

	if err := h.svc.DeletePostForPrincipal(c.Request.Context(), postID, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveOwnerPrincipal(ctx, c)
	}); err != nil {
		writePostAuthorError(c, err)
		return
	}
	response.NoContent(c)
}

func (h *PostHandler) ListPosts(c *gin.Context) {
	threadID := c.Param("id")
	page, pageSize, ok := response.ParsePagination(c, 20, 100)
	if !ok {
		return
	}

	posts, total, err := h.svc.ListPostsForPrincipal(c.Request.Context(), threadID, page, pageSize, nil)
	if err != nil {
		writePostReadError(c, err)
		return
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	response.List(c, posts, &response.Pagination{
		Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages,
	})
}

func (h *PostHandler) ListPostsForCurrentUser(c *gin.Context) {
	threadID := c.Param("id")
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	page, pageSize, ok := response.ParsePagination(c, 20, 100)
	if !ok {
		return
	}

	posts, total, err := h.svc.ListPostsForPrincipal(c.Request.Context(), threadID, page, pageSize, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveOwnerPrincipal(ctx, c)
	})
	if err != nil {
		writePostReadError(c, err)
		return
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	response.List(c, posts, &response.Pagination{
		Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages,
	})
}

func (h *PostHandler) SetOwnerSessionProof(verifier identityport.SessionVerifier, reader identityport.SessionStrengthReader) {
	h.ownerVerifier, h.ownerStrength = verifier, reader
}
func writePostReadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable), errors.Is(err, service.ErrThreadOwnerPrincipalUnavailable):
		response.Error(c, 401, 20002, "请重新登录后读取回复。")
	case errors.Is(err, repository.ErrThreadNotFound), errors.Is(err, repository.ErrPostNotFound):
		response.Error(c, 404, 40003, "内容不存在或当前不可用。")
	case errors.Is(err, identityport.ErrPolicyFactsChanged):
		response.Error(c, 409, 10004, "内容或访问权限已变化，请重新加载。")
	case errors.Is(err, repository.ErrThreadPaginationOverflow):
		response.Error(c, 400, 10001, "分页参数无效。")
	default:
		response.Error(c, 503, 10006, "暂时无法检查回复访问权限。")
	}
}
func (h *PostHandler) resolveOwnerPrincipal(ctx context.Context, c *gin.Context) (identityport.PrincipalContext, error) {
	id, _, ok := currentUser(c)
	if !ok {
		return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	return identityport.ResolveUserSessionPrincipal(ctx, h.ownerVerifier, h.ownerStrength, &auth.JWTClaims{UserID: id, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType})
}
func writePostAuthorError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable), errors.Is(err, service.ErrThreadOwnerPrincipalUnavailable):
		response.Error(c, 401, 20002, "请重新登录后修改回复。")
	case errors.Is(err, service.ErrPostAuthorDenied), errors.Is(err, service.ErrThreadAuthorWriteDenied):
		response.Error(c, 403, 20004, "无权在当前内容下创建或修改该回复。")
	case errors.Is(err, service.ErrPostAuthorInvalid):
		response.Error(c, 400, 10001, "回复内容无效，请检查后重试。")
	case errors.Is(err, identityport.ErrPolicyFactsChanged), errors.Is(err, repository.ErrPostFactsConflict), errors.Is(err, service.ErrThreadStateConflict):
		response.Error(c, 409, 10004, "内容或访问权限已变化，请重新加载。")
	case errors.Is(err, repository.ErrPostNotFound), errors.Is(err, repository.ErrThreadNotFound):
		response.Error(c, 404, 40003, "内容不存在或当前不可用。")
	default:
		response.Error(c, 503, 10006, "暂时无法完成回复写入，请稍后重试。")
	}
}

func (h *PostHandler) SetAdminSessionAuthority(reader identityport.AdminSessionAuthorityReader) {
	h.adminAuthority = reader
}
func (h *PostHandler) AdminGovernanceDelete(c *gin.Context) {
	resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		id := c.GetString("admin_account_id")
		if id == "" || id != c.GetString("user_id") {
			return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
		}
		return identityport.ResolveAdminSessionPrincipal(ctx, h.adminAuthority, &auth.JWTClaims{UserID: id, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType})
	}
	if err := h.svc.GovernanceDeleteForPrincipal(c.Request.Context(), "", c.Param("post_id"), resolve, nil); err != nil {
		switch {
		case errors.Is(err, identityport.ErrAdminSessionPrincipalUnavailable):
			response.Error(c, 401, 20002, "请重新登录后执行回复治理。")
		case errors.Is(err, service.ErrPostGovernanceDenied):
			response.Error(c, 403, 20004, "无权删除该回复；请确认执行权限并完成当前 MFA。")
		case errors.Is(err, service.ErrPostGovernanceUnavailable):
			response.Error(c, 503, 10006, "暂时无法检查回复治理权限。")
		default:
			writePostAuthorError(c, err)
		}
		return
	}
	response.NoContent(c)
}
