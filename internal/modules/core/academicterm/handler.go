package academicterm

import (
	"context"
	"strings"

	"github.com/campusos/CampusOS/pkg/apperror"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) ListOpen(c *gin.Context) {
	items, err := h.service.ListOpen(c.Request.Context())
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items})
}

func (h *Handler) ListAdmin(c *gin.Context) {
	items, err := h.service.ListAll(c.Request.Context())
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items})
}

func (h *Handler) Create(c *gin.Context) {
	actorID, ok := actorID(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	var req CreateRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, gin.H{"field": "request", "reason": "请求体格式不正确"})
		return
	}
	item, err := h.service.Create(c.Request.Context(), actorID, req)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.Created(c, item)
}

func (h *Handler) UpdateFirstWeek(c *gin.Context) {
	actorID, id, ok := h.requireActorAndID(c)
	if !ok {
		return
	}
	var req UpdateRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, gin.H{"field": "request", "reason": "请求体格式不正确"})
		return
	}
	item, err := h.service.UpdateFirstWeek(c.Request.Context(), actorID, id, req)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.Success(c, item)
}

func (h *Handler) Close(c *gin.Context)      { h.transition(c, h.service.Close) }
func (h *Handler) Open(c *gin.Context)       { h.transition(c, h.service.Open) }
func (h *Handler) SetDefault(c *gin.Context) { h.transition(c, h.service.SetDefault) }

func (h *Handler) Delete(c *gin.Context) {
	actorID, id, ok := h.requireActorAndID(c)
	if !ok {
		return
	}
	var req TransitionRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, gin.H{"field": "request", "reason": "请求体格式不正确"})
		return
	}
	if err := h.service.Delete(c.Request.Context(), actorID, id, req); err != nil {
		response.WriteError(c, err)
		return
	}
	response.NoContent(c)
}

func (h *Handler) transition(c *gin.Context, operation func(context.Context, string, string, TransitionRequest) (Term, error)) {
	actorID, id, ok := h.requireActorAndID(c)
	if !ok {
		return
	}
	var req TransitionRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, gin.H{"field": "request", "reason": "请求体格式不正确"})
		return
	}
	item, err := operation(c.Request.Context(), actorID, id, req)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.Success(c, item)
}

func (h *Handler) requireActorAndID(c *gin.Context) (string, string, bool) {
	actor, ok := actorID(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return "", "", false
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, gin.H{"field": "id", "reason": "学期 ID 不能为空"})
		return "", "", false
	}
	return actor, id, true
}

func actorID(c *gin.Context) (string, bool) {
	value, ok := c.Get("user_id")
	userID, valid := value.(string)
	return strings.TrimSpace(userID), ok && valid && strings.TrimSpace(userID) != ""
}
