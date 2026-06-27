package handler

import (
	"net/http"
	"strconv"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/service"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type PostHandler struct {
	svc *service.PostService
}

func NewPostHandler(svc *service.PostService) *PostHandler {
	return &PostHandler{svc: svc}
}

func (h *PostHandler) CreatePost(c *gin.Context) {
	threadID := c.Param("id")
	userID, username, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req domain.CreatePostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}

	post, err := h.svc.CreatePost(c.Request.Context(), threadID, userID, username, req)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.Created(c, post)
}

func (h *PostHandler) UpdatePost(c *gin.Context) {
	authorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	postID := c.Param("post_id")

	var req struct {
		Content string `json:"content" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	post, err := h.svc.UpdatePost(c.Request.Context(), postID, authorID, req.Content)
	if err != nil {
		response.Error(c, http.StatusForbidden, 20004, err.Error())
		return
	}
	response.Success(c, post)
}

func (h *PostHandler) DeletePost(c *gin.Context) {
	authorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	postID := c.Param("post_id")

	if err := h.svc.DeletePost(c.Request.Context(), postID, authorID); err != nil {
		response.Error(c, http.StatusForbidden, 20004, err.Error())
		return
	}
	response.NoContent(c)
}

func (h *PostHandler) ListPosts(c *gin.Context) {
	threadID := c.Param("id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	posts, total, err := h.svc.ListByThread(c.Request.Context(), threadID, page, pageSize)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
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
