package plugin

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// Handler 插件管理 HTTP 处理器
type Handler struct {
	manager    *Manager
	pluginsDir string
}

type HandlerOption func(*Handler)

func WithPluginsDir(dir string) HandlerOption {
	return func(h *Handler) {
		if dir != "" {
			h.pluginsDir = dir
		}
	}
}

// NewHandler 创建插件处理器
func NewHandler(manager *Manager, options ...HandlerOption) *Handler {
	h := &Handler{
		manager:    manager,
		pluginsDir: PluginsDirFromEnv(),
	}
	for _, option := range options {
		option(h)
	}
	return h
}

// ListPlugins 获取插件列表
// GET /api/v1/plugins
func (h *Handler) ListPlugins(c *gin.Context) {
	plugins := h.manager.ListPlugins()
	items := make([]map[string]interface{}, 0, len(plugins))
	for _, p := range plugins {
		items = append(items, map[string]interface{}{
			"id":           p.ID,
			"name":         p.Manifest.Name,
			"display_name": p.Manifest.DisplayName,
			"version":      p.Manifest.Version,
			"description":  p.Manifest.Description,
			"author":       p.Manifest.Author,
			"runtime":      p.Manifest.Runtime,
			"status":       p.Status,
			"error":        p.ErrorMsg,
			"events":       p.Manifest.Events.Subscribe,
		})
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

// GetPlugin 获取插件详情
// GET /api/v1/plugins/:name
func (h *Handler) GetPlugin(c *gin.Context) {
	name := c.Param("name")
	p, ok := h.manager.GetPlugin(name)
	if !ok {
		response.Error(c, http.StatusNotFound, 60003, "plugin not found")
		return
	}
	response.Success(c, gin.H{
		"id":           p.ID,
		"name":         p.Manifest.Name,
		"display_name": p.Manifest.DisplayName,
		"version":      p.Manifest.Version,
		"description":  p.Manifest.Description,
		"author":       p.Manifest.Author,
		"runtime":      p.Manifest.Runtime,
		"status":       p.Status,
		"error":        p.ErrorMsg,
		"events":       p.Manifest.Events.Subscribe,
		"permissions":  p.Manifest.Permissions,
		"storage":      p.Manifest.Storage,
	})
}

// ListPluginLogs 获取插件运行日志
// GET /api/v1/plugins/:name/logs?limit=100
func (h *Handler) ListPluginLogs(c *gin.Context) {
	name := c.Param("name")
	if _, ok := h.manager.GetPlugin(name); !ok {
		response.Error(c, http.StatusNotFound, 60003, "plugin not found")
		return
	}

	limit := 100
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	logs, err := h.manager.ListPluginLogs(c.Request.Context(), name, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 60004, err.Error())
		return
	}
	response.Success(c, gin.H{"items": logs, "total": len(logs)})
}

// EnablePlugin 启用插件
// POST /api/v1/plugins/:name/enable
func (h *Handler) EnablePlugin(c *gin.Context) {
	name := c.Param("name")
	if err := h.manager.Enable(name); err != nil {
		response.Error(c, http.StatusInternalServerError, 60004, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "plugin enabled", "name": name})
}

// DisablePlugin 禁用插件
// POST /api/v1/plugins/:name/disable
func (h *Handler) DisablePlugin(c *gin.Context) {
	name := c.Param("name")
	if err := h.manager.Stop(name); err != nil {
		response.Error(c, http.StatusInternalServerError, 60004, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "plugin disabled", "name": name})
}

// UninstallPlugin 卸载插件
// DELETE /api/v1/plugins/:name
func (h *Handler) UninstallPlugin(c *gin.Context) {
	name := c.Param("name")
	if err := h.manager.Uninstall(name); err != nil {
		response.Error(c, http.StatusInternalServerError, 60004, err.Error())
		return
	}
	response.NoContent(c)
}

// ExportPlugin 导出标准插件包
// GET /api/v1/plugins/:name/export
func (h *Handler) ExportPlugin(c *gin.Context) {
	name := c.Param("name")
	tempFile, err := os.CreateTemp("", "campusos-plugin-export-*.tar.gz")
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 60004, err.Error())
		return
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	info, err := h.manager.ExportPackage(name, tempPath)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		response.Error(c, status, 60004, err.Error())
		return
	}

	filename := info.Manifest.Name + "-" + info.Manifest.Version + PluginPackageExtension
	c.Header("Content-Type", "application/gzip")
	c.FileAttachment(tempPath, filename)
}

// ImportPluginPackage 导入标准插件包
// POST /api/v1/plugin-packages/import
func (h *Handler) ImportPluginPackage(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 60005, "plugin package file is required")
		return
	}
	filename := strings.ToLower(fileHeader.Filename)
	if !strings.HasSuffix(filename, ".tar.gz") && !strings.HasSuffix(filename, PluginPackageExtension) {
		response.Error(c, http.StatusBadRequest, 60005, "plugin package must be a .tar.gz file")
		return
	}

	tempDir, err := os.MkdirTemp("", "campusos-plugin-upload-*")
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 60004, err.Error())
		return
	}
	defer os.RemoveAll(tempDir)

	packagePath := filepath.Join(tempDir, "upload.tar.gz")
	if err := c.SaveUploadedFile(fileHeader, packagePath); err != nil {
		response.Error(c, http.StatusInternalServerError, 60004, err.Error())
		return
	}

	replace := c.PostForm("replace") == "true" || c.Query("replace") == "true"
	installed, err := h.manager.ImportPackage(packagePath, h.pluginsDir, replace)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "already installed") || strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		response.Error(c, status, 60004, err.Error())
		return
	}

	response.Success(c, gin.H{
		"name":         installed.Manifest.Name,
		"display_name": installed.Manifest.DisplayName,
		"version":      installed.Manifest.Version,
		"runtime":      installed.Manifest.Runtime,
		"status":       installed.Status,
	})
}
