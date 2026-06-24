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
	userID, _ := c.Get("user_id")
	username, _ := c.Get("username")

	var req domain.CreatePostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}

	post, err := h.svc.CreatePost(c.Request.Context(), threadID, userID.(string), username.(string), req)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.Created(c, post)
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
