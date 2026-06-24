package handler

import (
	"net/http"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/service"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type CategoryHandler struct {
	svc *service.CategoryService
}

func NewCategoryHandler(svc *service.CategoryService) *CategoryHandler {
	return &CategoryHandler{svc: svc}
}

func (h *CategoryHandler) Create(c *gin.Context) {
	var req domain.CreateCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	cat, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusConflict, 10004, err.Error())
		return
	}
	response.Created(c, cat)
}

func (h *CategoryHandler) Get(c *gin.Context) {
	cat, err := h.svc.GetByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusNotFound, 50002, err.Error())
		return
	}
	response.Success(c, cat)
}

func (h *CategoryHandler) List(c *gin.Context) {
	cats, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.Success(c, cats)
}

func (h *CategoryHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Error(c, http.StatusNotFound, 50002, err.Error())
		return
	}
	response.NoContent(c)
}

func (h *CategoryHandler) Update(c *gin.Context) {
	var req domain.UpdateCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	cat, err := h.svc.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		response.Error(c, http.StatusNotFound, 50002, err.Error())
		return
	}
	response.Success(c, cat)
}
