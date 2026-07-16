package plugin

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// MarketCatalog exposes only administrator-published external plugins to a
// signed-in user. Installation still remains a server-admin action.
func (h *Handler) MarketCatalog(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	items, err := market.Catalog(c.Request.Context(), true)
	if err != nil {
		h.marketError(c, err)
		return
	}
	payload := gin.H{"items": items, "total": len(items), "catalog_state": "ready"}
	if len(items) == 0 {
		// An empty user catalog is a normal governance state: only explicitly
		// published external plugins are visible here. Keep that distinction in
		// the API so clients do not imply that built-in features are missing.
		payload["catalog_state"] = "empty"
		payload["empty_reason"] = "管理员暂未发布可供用户授权的外部插件。内置功能不在插件中心安装或授权。"
		payload["request_available"] = true
	}
	response.Success(c, payload)
}

func (h *Handler) MarketMyGrants(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	items, err := market.MyGrants(c.Request.Context(), marketUserID(c))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) MarketMyUsage(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	items, err := market.MyUsage(c.Request.Context(), marketUserID(c))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) EnableMarketPlugin(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	var req struct {
		Permissions *[]string `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.marketError(c, fmt.Errorf("%w: %v", ErrMarketInvalidInput, err))
		return
	}
	if req.Permissions == nil {
		h.marketError(c, fmt.Errorf("%w: permissions must be explicitly acknowledged", ErrMarketInvalidInput))
		return
	}
	grant, err := market.Grant(c.Request.Context(), c.Param("name"), marketUserID(c), *req.Permissions)
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, grant)
}

func (h *Handler) RevokeMarketPlugin(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	grant, err := market.Revoke(c.Request.Context(), c.Param("name"), marketUserID(c))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, grant)
}

func (h *Handler) CreateMarketRecord(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	var input RecordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.marketError(c, fmt.Errorf("%w: %v", ErrMarketInvalidInput, err))
		return
	}
	record, err := market.CreateUserRecord(c.Request.Context(), c.Param("name"), marketUserID(c), c.Param("collection"), input)
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, record)
}

func (h *Handler) ListMarketRecords(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	query := RecordQuery{PluginName: c.Param("name"), OwnerID: marketUserID(c), Collection: c.Param("collection"), Keyword: strings.TrimSpace(c.Query("q")), Filters: map[string]string{}, Page: parsePositiveInt(c.Query("page"), 1), PageSize: parsePositiveInt(c.Query("page_size"), 20)}
	for key, values := range c.Request.URL.Query() {
		if strings.HasPrefix(key, "filter.") && len(values) > 0 {
			query.Filters[strings.TrimPrefix(key, "filter.")] = values[0]
		}
	}
	page, err := market.ListUserRecords(c.Request.Context(), query)
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, page)
}

func (h *Handler) GetMarketRecord(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	record, err := market.GetUserRecord(c.Request.Context(), c.Param("name"), marketUserID(c), c.Param("collection"), c.Param("key"))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, record)
}

func (h *Handler) UpdateMarketRecord(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	var input RecordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		h.marketError(c, fmt.Errorf("%w: %v", ErrMarketInvalidInput, err))
		return
	}
	record, err := market.UpdateUserRecord(c.Request.Context(), c.Param("name"), marketUserID(c), c.Param("collection"), c.Param("key"), input)
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, record)
}

func (h *Handler) DeleteMarketRecord(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	version, err := strconv.ParseInt(c.Query("version"), 10, 64)
	if err != nil {
		h.marketError(c, fmt.Errorf("%w: version query is required", ErrMarketInvalidInput))
		return
	}
	if err := market.DeleteUserRecord(c.Request.Context(), c.Param("name"), marketUserID(c), c.Param("collection"), c.Param("key"), version); err != nil {
		h.marketError(c, err)
		return
	}
	response.NoContent(c)
}

func (h *Handler) UploadMarketFile(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		h.marketError(c, fmt.Errorf("%w: file is required", ErrMarketInvalidInput))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		h.marketError(c, err)
		return
	}
	defer file.Close()
	if fileHeader.Size > 32*1024*1024 {
		h.marketError(c, ErrMarketQuotaExceeded)
		return
	}
	content, err := io.ReadAll(io.LimitReader(file, 32*1024*1024+1))
	if err != nil {
		h.marketError(c, err)
		return
	}
	if len(content) > 32*1024*1024 {
		h.marketError(c, fmt.Errorf("%w: file is too large", ErrMarketQuotaExceeded))
		return
	}
	saved, err := market.UploadUserFile(c.Request.Context(), c.Param("name"), marketUserID(c), fileHeader.Filename, fileHeader.Header.Get("Content-Type"), content)
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, saved)
}

func (h *Handler) ListMarketFiles(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	items, err := market.ListUserFiles(c.Request.Context(), c.Param("name"), marketUserID(c))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) DownloadMarketFile(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	file, path, err := market.UserFilePath(c.Request.Context(), c.Param("name"), marketUserID(c), c.Param("file_id"))
	if err != nil {
		h.marketError(c, err)
		return
	}
	c.Header("Content-Type", file.ContentType)
	c.FileAttachment(path, file.OriginalName)
}

func (h *Handler) DeleteMarketFile(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	if err := market.DeleteUserFile(c.Request.Context(), c.Param("name"), marketUserID(c), c.Param("file_id")); err != nil {
		h.marketError(c, err)
		return
	}
	response.NoContent(c)
}

func (h *Handler) ExportMarketData(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	export, err := market.ExportMyData(c.Request.Context(), c.Param("name"), marketUserID(c))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, export)
}

func (h *Handler) DeleteMarketData(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	records, files, err := market.DeleteMyData(c.Request.Context(), c.Param("name"), marketUserID(c))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, gin.H{"records_deleted": records, "files_deleted": files})
}

func (h *Handler) RequestMarketInstall(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	_ = c.ShouldBindJSON(&req)
	request, err := market.RequestInstall(c.Request.Context(), c.Param("name"), marketUserID(c), req.Message)
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, request)
}

func (h *Handler) SearchMarketRecords(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	pluginName := strings.TrimSpace(c.Query("plugin"))
	collection := strings.TrimSpace(c.Query("collection"))
	if pluginName == "" || collection == "" {
		h.marketError(c, fmt.Errorf("%w: plugin and collection are required", ErrMarketInvalidInput))
		return
	}
	page, err := market.SearchMyRecords(c.Request.Context(), pluginName, marketUserID(c), collection, c.Query("q"), parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("page_size"), 20))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, page)
}

func (h *Handler) AdminMarketOverview(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	items, err := market.Catalog(c.Request.Context(), false)
	if err != nil {
		h.marketError(c, err)
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, entry := range items {
		metrics, metricErr := market.Metrics(c.Request.Context(), entry.PluginName)
		if metricErr != nil {
			h.marketError(c, metricErr)
			return
		}
		state := gin.H{"status": "not_installed", "health": HealthUnavailable}
		systemPermissions := []APIPermission{}
		if installed, found := h.manager.GetPlugin(entry.PluginName); found && installed != nil {
			state = gin.H{"status": installed.Status, "health": installed.Health, "backend_state": installed.BackendState, "frontend_state": installed.FrontendState}
			if installed.Manifest != nil {
				systemPermissions = append(systemPermissions, installed.Manifest.Permissions.API...)
			}
		}
		result = append(result, gin.H{"catalog": entry, "metrics": metrics, "runtime_state": state, "system_permissions": systemPermissions})
	}
	response.Success(c, gin.H{"items": result, "total": len(result)})
}

func (h *Handler) AdminSetMarketVisibility(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	var req struct {
		Visibility string `json:"visibility"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.marketError(c, fmt.Errorf("%w: %v", ErrMarketInvalidInput, err))
		return
	}
	entry, err := market.SetCatalogVisibility(c.Request.Context(), c.Param("name"), req.Visibility, marketUserID(c))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, entry)
}

func (h *Handler) AdminMarketRequests(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	items, err := market.InstallRequests(c.Request.Context(), c.Query("status"))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) AdminReviewMarketRequest(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		h.marketError(c, fmt.Errorf("%w: invalid request id", ErrMarketInvalidInput))
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.marketError(c, fmt.Errorf("%w: %v", ErrMarketInvalidInput, err))
		return
	}
	item, err := market.ReviewInstallRequest(c.Request.Context(), id, marketUserID(c), req.Status)
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, item)
}

func (h *Handler) AdminMarketReleases(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	items, err := market.Releases(c.Request.Context(), c.Param("name"))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) AdminMarketAudits(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	items, err := market.Audits(c.Request.Context(), c.Query("plugin"), parsePositiveInt(c.Query("limit"), 50))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) AdminSaveMarketRelease(c *gin.Context) {
	market, ok := h.marketService(c)
	if !ok {
		return
	}
	var release PluginRelease
	if err := c.ShouldBindJSON(&release); err != nil {
		h.marketError(c, fmt.Errorf("%w: %v", ErrMarketInvalidInput, err))
		return
	}
	release.PluginName = c.Param("name")
	item, err := market.SaveRelease(c.Request.Context(), release, marketUserID(c))
	if err != nil {
		h.marketError(c, err)
		return
	}
	response.Success(c, item)
}

func (h *Handler) marketService(c *gin.Context) (*MarketService, bool) {
	if h == nil || h.market == nil || !h.market.Available() {
		response.Error(c, http.StatusServiceUnavailable, 60004, "plugin market service is unavailable")
		return nil, false
	}
	if marketUserID(c) == "" {
		response.Error(c, http.StatusUnauthorized, 20001, "authenticated user is required")
		return nil, false
	}
	return h.market, true
}

func marketUserID(c *gin.Context) string {
	value, _ := c.Get("user_id")
	userID, _ := value.(string)
	return userID
}

func (h *Handler) marketError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, 60004
	switch {
	case errors.Is(err, ErrMarketNotFound):
		status, code = http.StatusNotFound, 60003
	case errors.Is(err, ErrMarketDenied):
		status, code = http.StatusForbidden, 20004
	case errors.Is(err, ErrMarketConflict), errors.Is(err, ErrMarketVersionMismatch):
		status, code = http.StatusConflict, 60004
	case errors.Is(err, ErrMarketInvalidInput), errors.Is(err, ErrMarketUnsupported), errors.Is(err, ErrMarketQuotaExceeded):
		status, code = http.StatusBadRequest, 60005
	}
	response.Error(c, status, code, err.Error())
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
