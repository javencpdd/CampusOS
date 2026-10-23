package plugin

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// Handler 插件管理 HTTP 处理器
type Handler struct {
	manager       *Manager
	pluginsDir    string
	builtins      BuiltinFeatureCompatibility
	market        *MarketService
	authorization *AuthorizationService
	secrets       *SecretService
}

// BuiltinFeatureCompatibility is a deprecated transport adapter only. The
// external Plugin Manager never receives module descriptors or lifecycle.
type BuiltinFeatureCompatibility interface {
	CompatibilityGet(string) (map[string]interface{}, bool)
	CompatibilityUpdateConfig(string, map[string]interface{}) (map[string]interface{}, error)
	CompatibilityRequest(string, bool) (map[string]interface{}, error)
	CompatibilityKnown(string) bool
}

type HandlerOption func(*Handler)

func WithPluginsDir(dir string) HandlerOption {
	return func(h *Handler) {
		if dir != "" {
			h.pluginsDir = dir
		}
	}
}

func WithBuiltinFeatureCompatibility(features BuiltinFeatureCompatibility) HandlerOption {
	return func(h *Handler) { h.builtins = features }
}

func WithMarketService(market *MarketService) HandlerOption {
	return func(h *Handler) { h.market = market }
}

func WithAuthorizationService(service *AuthorizationService) HandlerOption {
	return func(h *Handler) { h.authorization = service }
}

func WithSecretService(service *SecretService) HandlerOption {
	return func(h *Handler) { h.secrets = service }
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
	items := make([]gin.H, 0, len(plugins))
	for _, p := range plugins {
		items = append(items, h.pluginPayload(p))
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

// GetPlugin 获取插件详情
// GET /api/v1/plugins/:name
func (h *Handler) GetPlugin(c *gin.Context) {
	name := c.Param("name")
	p, ok := h.manager.GetPlugin(name)
	if !ok {
		if payload, compatible := h.compatibilityGet(name); compatible {
			h.manager.RecordCompatibility(c.Request.Context(), "legacy-builtin-api:"+name, "deprecated-plugin-feature-read", map[string]string{"feature": name, "method": c.Request.Method})
			response.Success(c, payload)
			return
		}
		response.Error(c, http.StatusNotFound, 60003, "plugin not found")
		return
	}
	payload := h.pluginPayload(p)
	payload["permissions"] = p.Manifest.Permissions
	payload["storage"] = p.Manifest.Storage
	payload["config"] = p.Manifest.Config
	payload["config_schema"] = p.Manifest.ConfigSchema
	response.Success(c, payload)
}

// UpdatePluginConfig 更新插件配置
// PUT /api/v1/plugins/:name/config
func (h *Handler) UpdatePluginConfig(c *gin.Context) {
	name := c.Param("name")
	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 60005, "invalid config: "+err.Error())
		return
	}
	config, err := h.updateConfig(c.Request.Context(), name, req)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "config field") {
			status = http.StatusBadRequest
		}
		response.Error(c, status, 60004, err.Error())
		return
	}
	response.Success(c, gin.H{"name": name, "config": config})
}

func (h *Handler) updateConfig(ctx context.Context, name string, input map[string]interface{}) (map[string]interface{}, error) {
	if h.builtins != nil && h.builtins.CompatibilityKnown(name) {
		h.manager.RecordCompatibility(ctx, "legacy-builtin-api:"+name, "deprecated-plugin-feature-config", map[string]string{"feature": name})
		return h.builtins.CompatibilityUpdateConfig(name, input)
	}
	return h.manager.UpdateConfig(name, input)
}

func (h *Handler) compatibilityGet(name string) (map[string]interface{}, bool) {
	if h.builtins == nil {
		return nil, false
	}
	return h.builtins.CompatibilityGet(name)
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
	if payload, handled, err := h.requestFeatureState(c.Request.Context(), name, true); handled {
		if err != nil {
			response.Error(c, lifecycleErrorStatus(err), 60004, err.Error())
			return
		}
		response.Success(c, payload)
		return
	}
	if err := h.manager.RequestEnable(name); err != nil {
		response.Error(c, lifecycleErrorStatus(err), 60004, err.Error())
		return
	}
	p, _ := h.manager.GetPlugin(name)
	payload := h.pluginPayload(p)
	payload["message"] = lifecycleMessage(payload, true)
	response.Success(c, payload)
}

// DisablePlugin 禁用插件
// POST /api/v1/plugins/:name/disable
func (h *Handler) DisablePlugin(c *gin.Context) {
	name := c.Param("name")
	if payload, handled, err := h.requestFeatureState(c.Request.Context(), name, false); handled {
		if err != nil {
			response.Error(c, lifecycleErrorStatus(err), 60004, err.Error())
			return
		}
		response.Success(c, payload)
		return
	}
	if err := h.manager.RequestDisable(name); err != nil {
		response.Error(c, lifecycleErrorStatus(err), 60004, err.Error())
		return
	}
	p, _ := h.manager.GetPlugin(name)
	payload := h.pluginPayload(p)
	payload["message"] = lifecycleMessage(payload, false)
	response.Success(c, payload)
}

func (h *Handler) requestFeatureState(ctx context.Context, identifier string, enabled bool) (map[string]interface{}, bool, error) {
	if h.builtins == nil || !h.builtins.CompatibilityKnown(identifier) {
		return nil, false, nil
	}
	h.manager.RecordCompatibility(ctx, "legacy-builtin-api:"+identifier, "deprecated-plugin-feature-lifecycle", map[string]string{"feature": identifier})
	payload, err := h.builtins.CompatibilityRequest(identifier, enabled)
	return payload, true, err
}

// ReloadUserPlugin reloads changed user-level plugin code without restarting
// the CampusOS API process.
// POST /api/v1/plugins/:name/reload
func (h *Handler) ReloadUserPlugin(c *gin.Context) {
	name := c.Param("name")
	if h.builtins != nil && h.builtins.CompatibilityKnown(name) {
		response.Error(c, http.StatusBadRequest, 60004, "core modules and built-in features are not reloadable plugins")
		return
	}
	if err := h.manager.ReloadUserPlugin(name); err != nil {
		response.Error(c, lifecycleErrorStatus(err), 60004, err.Error())
		return
	}
	p, _ := h.manager.GetPlugin(name)
	payload := h.pluginPayload(p)
	payload["message"] = "user-level plugin loaded or reloaded"
	response.Success(c, payload)
}

// UninstallPlugin 卸载插件
// DELETE /api/v1/plugins/:name
func (h *Handler) UninstallPlugin(c *gin.Context) {
	name := c.Param("name")
	if h.builtins != nil && h.builtins.CompatibilityKnown(name) {
		response.Error(c, http.StatusBadRequest, 60004, "core modules and built-in features cannot be uninstalled")
		return
	}
	if err := h.manager.Uninstall(name); err != nil {
		response.Error(c, lifecycleErrorStatus(err), 60004, err.Error())
		return
	}
	response.NoContent(c)
}

// ExportPlugin 导出标准插件包
// GET /api/v1/plugins/:name/export
func (h *Handler) ExportPlugin(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 60005, "插件安装与回退暂不可用，请稍后重试。")
}

// ImportPluginPackage 导入标准插件包
// POST /api/v1/plugin-packages/import
func (h *Handler) ImportPluginPackage(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 60005, "插件安装与回退暂不可用，请稍后重试。")
}

// PrecheckPluginPackage 校验插件包并预览权限、文件和冲突
// POST /api/v1/plugin-packages/precheck
func (h *Handler) PrecheckPluginPackage(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 60005, "插件安装与回退暂不可用，请稍后重试。")
}

// ListVersionSnapshots returns verified pre-update snapshots.
func (h *Handler) ListVersionSnapshots(c *gin.Context) {
	name := c.Param("name")
	snapshots, err := h.manager.ListVersionSnapshots(name)
	if err != nil {
		response.Error(c, lifecycleErrorStatus(err), 60004, err.Error())
		return
	}
	response.Success(c, gin.H{"items": snapshots, "total": len(snapshots)})
}

// RollbackVersionSnapshot restores one verified user-plugin package and hot reloads it when needed.
func (h *Handler) RollbackVersionSnapshot(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 60005, "插件安装与回退暂不可用，请稍后重试。")
}

func currentActor(c *gin.Context) (string, string) {
	id := ""
	name := ""
	if raw, ok := c.Get("user_id"); ok {
		if value, ok := raw.(string); ok {
			id = value
		}
	}
	if raw, ok := c.Get("username"); ok {
		if value, ok := raw.(string); ok {
			name = value
		}
	}
	return id, name
}

func (h *Handler) pluginPayload(p *Plugin) gin.H {
	if p == nil || p.Manifest == nil {
		return gin.H{}
	}
	state, _ := h.manager.LifecycleState(p.ID)
	classification := h.manager.Catalog().Classify(p.Manifest)
	payload := gin.H{
		"id":                       p.ID,
		"name":                     p.Manifest.Name,
		"display_name":             p.Manifest.DisplayName,
		"version":                  p.Manifest.Version,
		"description":              p.Manifest.Description,
		"author":                   p.Manifest.Author,
		"runtime":                  p.Manifest.Runtime,
		"scope":                    state.Scope,
		"activation_mode":          state.ActivationMode,
		"backend_activation_mode":  state.BackendActivationMode,
		"frontend_activation_mode": state.FrontendActivationMode,
		"backend_state":            state.BackendState,
		"frontend_state":           state.FrontendState,
		"health":                   state.Health,
		"ui_revision":              h.manager.UIRevision(),
		"desired_enabled":          state.DesiredEnabled,
		"pending_restart":          state.PendingRestart,
		"status":                   p.Status,
		"error":                    p.ErrorMsg,
		"events":                   p.Manifest.Events.Subscribe,
		"checksum":                 p.Checksum,
		"package_size":             p.PackageSize,
	}
	payload["lifecycle_owner"] = "plugin-platform"
	payload["capability_class"] = classification
	payload["ui_contract_version"] = p.Manifest.UI.ContractVersion
	surfaces := make([]gin.H, 0, len(p.Manifest.UI.Surfaces))
	for _, surface := range p.Manifest.UI.Surfaces {
		surfaces = append(surfaces, gin.H{"id": surface.ID, "version": surface.Version, "type": surface.Type, "presentations": surface.Presentations})
	}
	payload["ui_surfaces"] = surfaces
	return payload
}

func lifecycleMessage(payload gin.H, enabled bool) string {
	if payload["activation_mode"] == "restart" {
		if enabled {
			return "system plugin enable staged; restart the API server to apply"
		}
		return "system plugin disable staged; restart the API server to apply"
	}
	if enabled {
		return "user-level plugin loaded"
	}
	return "user-level plugin stopped"
}

func lifecycleErrorStatus(err error) int {
	if strings.Contains(err.Error(), "not found") {
		return http.StatusNotFound
	}
	if strings.Contains(err.Error(), "requires a server restart") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
