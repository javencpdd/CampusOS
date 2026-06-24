package plugin

import (
	"net/http"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// Handler 插件管理 HTTP 处理器
type Handler struct {
	manager *Manager
}

// NewHandler 创建插件处理器
func NewHandler(manager *Manager) *Handler {
	return &Handler{manager: manager}
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
