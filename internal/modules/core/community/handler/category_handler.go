package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	requestutil "github.com/campusos/CampusOS/pkg/request"
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
	actorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req domain.CreateCategoryRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	cat, err := h.svc.CreateForActor(c.Request.Context(), actorID, req)
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Created(c, cat)
}

func (h *CategoryHandler) Get(c *gin.Context) {
	cat, err := h.svc.GetByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, cat)
}

func (h *CategoryHandler) GetAdmin(c *gin.Context) {
	cat, err := h.svc.GetAdminByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, cat)
}

func (h *CategoryHandler) List(c *gin.Context) {
	cats, err := h.svc.List(c.Request.Context())
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, cats)
}

func (h *CategoryHandler) ListTree(c *gin.Context) {
	cats, err := h.svc.ListTree(c.Request.Context(), false)
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, cats)
}

func (h *CategoryHandler) ListAdmin(c *gin.Context) {
	cats, err := h.svc.ListAdmin(c.Request.Context())
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, cats)
}

func (h *CategoryHandler) ListAdminTree(c *gin.Context) {
	cats, err := h.svc.ListTree(c.Request.Context(), true)
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, cats)
}

func (h *CategoryHandler) Update(c *gin.Context) {
	actorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req domain.UpdateCategoryRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	cat, err := h.svc.UpdateForActor(c.Request.Context(), actorID, c.Param("id"), req)
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, cat)
}

func (h *CategoryHandler) Move(c *gin.Context) {
	actorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req domain.MoveCategoryRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	cat, err := h.svc.MoveForActor(c.Request.Context(), actorID, c.Param("id"), req)
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, cat)
}

func (h *CategoryHandler) ArchiveImpact(c *gin.Context) {
	impact, err := h.svc.ArchiveImpact(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, impact)
}

func (h *CategoryHandler) ListThreadTypePolicies(c *gin.Context) {
	policies, err := h.svc.ListThreadTypePolicies(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, gin.H{"category_id": c.Param("id"), "items": policies})
}

func (h *CategoryHandler) UpdateThreadTypePolicies(c *gin.Context) {
	actorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req domain.UpdateCategoryThreadTypePolicyRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	cat, policies, err := h.svc.UpdateThreadTypePoliciesForActor(c.Request.Context(), actorID, c.Param("id"), req)
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, gin.H{"category": cat, "items": policies})
}

func (h *CategoryHandler) Archive(c *gin.Context) {
	actorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	version, ok := requiredCategoryVersion(c)
	if !ok {
		return
	}
	cat, err := h.svc.ArchiveForActor(c.Request.Context(), actorID, c.Param("id"), version)
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, cat)
}

func (h *CategoryHandler) Restore(c *gin.Context) {
	actorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	version, ok := requiredCategoryVersion(c)
	if !ok {
		return
	}
	cat, err := h.svc.RestoreForActor(c.Request.Context(), actorID, c.Param("id"), version)
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	response.Success(c, cat)
}

// Delete maps the historical physical-delete endpoint to archive. It remains
// intentionally versioned so old callers cannot silently override a move.
func (h *CategoryHandler) Delete(c *gin.Context) {
	actorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	version, ok := requiredCategoryVersion(c)
	if !ok {
		return
	}
	if _, err := h.svc.ArchiveLegacyForActor(c.Request.Context(), actorID, c.Param("id"), version); err != nil {
		writeCategoryError(c, err)
		return
	}
	response.NoContent(c)
}

func requiredCategoryVersion(c *gin.Context) (int64, bool) {
	value := strings.TrimSpace(c.Query("version"))
	if value == "" {
		response.Error(c, http.StatusBadRequest, 10001, "version query parameter is required")
		return 0, false
	}
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version < 1 {
		response.Error(c, http.StatusBadRequest, 10001, "version query parameter must be a positive integer")
		return 0, false
	}
	return version, true
}

func writeCategoryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repository.ErrCategoryNotFound):
		response.Error(c, http.StatusNotFound, 50002, "category not found")
	case errors.Is(err, repository.ErrCategoryVersionConflict):
		response.Error(c, http.StatusConflict, 10004, "category version conflict; reload and retry")
	case errors.Is(err, service.ErrCategoryHierarchy), errors.Is(err, service.ErrCategoryArchived), errors.Is(err, service.ErrCategoryPostingUnavailable):
		response.Error(c, http.StatusConflict, 10004, err.Error())
	case errors.Is(err, service.ErrCategoryVersionRequired):
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
	default:
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
	}
}
