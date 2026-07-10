package richtext

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

func (h *Handler) Status(c *gin.Context) {
	response.Success(c, h.svc.Status())
}

func (h *Handler) CreateDraft(c *gin.Context) {
	userID, username, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req SaveArticleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	result, err := h.svc.CreateDraft(c.Request.Context(), userID, username, req)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Created(c, result)
}

func (h *Handler) UpdateDraft(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req SaveArticleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	result, err := h.svc.UpdateDraft(c.Request.Context(), c.Param("id"), userID, req)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Publish(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	result, err := h.svc.Publish(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Offline(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	result, err := h.svc.Offline(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Delete(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if err := h.svc.Delete(c.Request.Context(), c.Param("id"), userID); err != nil {
		writeRichTextError(c, err)
		return
	}
	response.NoContent(c)
}

func (h *Handler) AdminOffline(c *gin.Context) {
	adminID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	result, err := h.svc.AdminOffline(c.Request.Context(), c.Param("id"), adminID)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) AdminRestore(c *gin.Context) {
	adminID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	result, err := h.svc.AdminRestore(c.Request.Context(), c.Param("id"), adminID)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) AdminDelete(c *gin.Context) {
	adminID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if err := h.svc.AdminDelete(c.Request.Context(), c.Param("id"), adminID); err != nil {
		writeRichTextError(c, err)
		return
	}
	response.NoContent(c)
}

func (h *Handler) Preview(c *gin.Context) {
	var req PreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	result, err := h.svc.Preview(c.Request.Context(), req.ContentHTML)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) GetPublished(c *gin.Context) {
	article, err := h.svc.GetArticle(c.Request.Context(), c.Param("id"), "")
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, article)
}

func (h *Handler) GetMine(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	article, err := h.svc.GetArticle(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, article)
}

func (h *Handler) UploadAsset(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "image file is required")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	defer file.Close()

	asset, err := h.svc.UploadAsset(c.Request.Context(), userID, fileHeader.Filename, file, c.PostForm("thread_id"), c.PostForm("article_content_id"))
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, asset)
}

func (h *Handler) ServeAsset(c *gin.Context) {
	path, err := h.svc.AssetPath(c.Param("user_id"), c.Param("filename"))
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	c.File(path)
}

func currentUser(c *gin.Context) (string, string, bool) {
	rawID, ok := c.Get("user_id")
	if !ok {
		return "", "", false
	}
	id, ok := rawID.(string)
	if !ok || id == "" {
		return "", "", false
	}
	username := "Anonymous"
	if rawName, ok := c.Get("username"); ok {
		if name, ok := rawName.(string); ok && name != "" {
			username = name
		}
	}
	return id, username, true
}

func writeRichTextError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrPluginDisabled):
		response.Error(c, http.StatusForbidden, 73001, err.Error())
	case errors.Is(err, ErrInvalidArticle), errors.Is(err, ErrAssetInvalid), errors.Is(err, ErrAssetTooLarge), errors.Is(err, ErrAssetQuotaExceeded), errors.Is(err, ErrAssetUnsupported):
		response.Error(c, http.StatusBadRequest, 73002, err.Error())
	case errors.Is(err, ErrPermissionDenied):
		response.Error(c, http.StatusForbidden, 73003, err.Error())
	case errors.Is(err, ErrArticleNotFound), errors.Is(err, ErrAssetNotFound):
		response.Error(c, http.StatusNotFound, 73004, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, 73000, err.Error())
	}
}
