package storage

import (
	"errors"
	"net/http"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	images *ContentImageStore
}

func NewHandler(images *ContentImageStore) *Handler { return &Handler{images: images} }

func (h *Handler) UploadContentImage(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	userIDValue, ok := userID.(string)
	if !ok || userIDValue == "" {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	// Enforce the upload limit before multipart parsing can spill an arbitrarily
	// large request to a temporary file. The small allowance covers form headers.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.images.MaxBytes()+contentImageFormSlack)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeContentImageError(c, ErrImageTooLarge)
			return
		}
		response.Error(c, http.StatusBadRequest, 10001, "image file is required")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "image file cannot be opened")
		return
	}
	defer file.Close()
	asset, err := h.images.Save(userIDValue, fileHeader.Filename, file)
	if err != nil {
		writeContentImageError(c, err)
		return
	}
	response.Success(c, asset)
}

func (h *Handler) ServeContentImage(c *gin.Context) {
	path, err := h.images.Path(c.Param("user_id"), c.Param("filename"))
	if err != nil {
		writeContentImageError(c, err)
		return
	}
	c.File(path)
}

func writeContentImageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrImageNotFound):
		response.Error(c, http.StatusNotFound, 74004, "content image was not found")
	case errors.Is(err, ErrImageTooLarge):
		response.Error(c, http.StatusRequestEntityTooLarge, 74002, err.Error())
	case errors.Is(err, ErrImageUnsupported), errors.Is(err, ErrUnsafePath):
		response.Error(c, http.StatusBadRequest, 74002, err.Error())
	case errors.Is(err, ErrQuotaExceeded):
		response.Error(c, http.StatusBadRequest, 74003, "personal storage quota exceeded")
	default:
		response.Error(c, http.StatusInternalServerError, 10006, "content image operation failed")
	}
}
