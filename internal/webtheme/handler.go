package webtheme

import (
	"errors"
	"net/http"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Catalog(c *gin.Context) {
	catalog, err := h.svc.Catalog()
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, catalog)
}

func (h *Handler) Package(c *gin.Context) {
	pack, err := h.svc.Package(c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, pack)
}

func (h *Handler) Asset(c *gin.Context) {
	data, contentType, err := h.svc.Asset(c.Param("name"), c.Param("path"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(http.StatusOK, contentType, data)
}

func writeError(c *gin.Context, err error) {
	if errors.Is(err, ErrDisabled) {
		response.Error(c, http.StatusServiceUnavailable, 10006, err.Error())
		return
	}
	if errors.Is(err, ErrNotFound) {
		response.Error(c, http.StatusNotFound, 10004, "web theme not found")
		return
	}
	response.Error(c, http.StatusInternalServerError, 10006, err.Error())
}
