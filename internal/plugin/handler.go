package plugin

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// Handler 插件管理 HTTP 处理器
type Handler struct {
	manager    *Manager
	pluginsDir string
	features   *platformfeature.Registry
}

type HandlerOption func(*Handler)

func WithPluginsDir(dir string) HandlerOption {
	return func(h *Handler) {
		if dir != "" {
			h.pluginsDir = dir
		}
	}
}

func WithFeatureRegistry(features *platformfeature.Registry) HandlerOption {
	return func(h *Handler) { h.features = features }
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
	config, err := h.manager.UpdateConfig(name, req)
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
	if featureID, ok := legacyBuiltinFeatureID(name); ok && h.features != nil {
		if err := h.updateFeatureConfig(featureID, name, config); err != nil {
			response.Error(c, http.StatusInternalServerError, 60004, err.Error())
			return
		}
	}
	response.Success(c, gin.H{"name": name, "config": config})
}

func (h *Handler) updateFeatureConfig(featureID, pluginID string, config map[string]interface{}) error {
	if featureID != "appearance" {
		return h.features.UpdateConfig(featureID, config)
	}
	root := h.features.Config(featureID)
	section := "web_theme"
	if pluginID == "homepage-customizer" {
		section = "homepage"
	}
	root[section] = config
	return h.features.UpdateConfig(featureID, root)
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
	rollback, err := h.requestFeatureState(name, true)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 60004, err.Error())
		return
	}
	if err := h.manager.RequestEnable(name); err != nil {
		rollback()
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
	rollback, err := h.requestFeatureState(name, false)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 60004, err.Error())
		return
	}
	if err := h.manager.RequestDisable(name); err != nil {
		rollback()
		response.Error(c, lifecycleErrorStatus(err), 60004, err.Error())
		return
	}
	p, _ := h.manager.GetPlugin(name)
	payload := h.pluginPayload(p)
	payload["message"] = lifecycleMessage(payload, false)
	response.Success(c, payload)
}

func (h *Handler) requestFeatureState(pluginID string, enabled bool) (func(), error) {
	if h.features == nil {
		return func() {}, nil
	}
	featureID, ok := legacyBuiltinFeatureID(pluginID)
	if !ok {
		return func() {}, nil
	}
	previous, found := h.features.Get(featureID)
	if !found {
		return func() {}, nil
	}
	if _, err := h.features.Request(featureID, enabled); err != nil {
		return nil, err
	}
	return func() { _, _ = h.features.Request(featureID, previous.DesiredEnabled) }, nil
}

func legacyBuiltinFeatureID(pluginID string) (string, bool) {
	switch pluginID {
	case "personal-space", "controlled-richtext-article", "personal-schedule":
		return pluginID, true
	case "web-theme", "homepage-customizer":
		return "appearance", true
	default:
		return "", false
	}
}

// ReloadUserPlugin reloads changed user-level plugin code without restarting
// the CampusOS API process.
// POST /api/v1/plugins/:name/reload
func (h *Handler) ReloadUserPlugin(c *gin.Context) {
	name := c.Param("name")
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
	if err := h.manager.Uninstall(name); err != nil {
		response.Error(c, lifecycleErrorStatus(err), 60004, err.Error())
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
	precheck, err := PrecheckPluginPackage(packagePath, h.pluginsDir)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 60004, err.Error())
		return
	}
	pluginName := "unknown"
	if precheck.Manifest != nil {
		pluginName = precheck.Manifest.Name
	}
	actorID, actorName := currentActor(c)
	auditMetadata := map[string]interface{}{
		"actor_id":          actorID,
		"actor_name":        actorName,
		"checksum":          precheck.Checksum,
		"package_size":      precheck.PackageSize,
		"risk_level":        precheck.RiskLevel,
		"risk_score":        precheck.RiskScore,
		"risk_reasons":      precheck.RiskReasons,
		"replace":           replace,
		"conflict":          precheck.Conflict,
		"existing_version":  precheck.ExistingVersion,
		"import_version":    precheck.ImportVersion,
		"version_change":    precheck.VersionChange,
		"signature_status":  precheck.SignatureStatus,
		"precheck_warnings": precheck.Warnings,
	}
	if precheck.Manifest != nil {
		auditMetadata["scope"] = precheck.Manifest.Scope
	}
	if !precheck.Allowed {
		auditMetadata["outcome"] = "precheck_failed"
		auditMetadata["errors"] = precheck.Errors
		h.manager.RecordPluginAudit(c.Request.Context(), pluginName, "warn", "plugin package import rejected by precheck", auditMetadata)
		response.Error(c, http.StatusBadRequest, 60005, "plugin package precheck failed: "+strings.Join(precheck.Errors, "; "))
		return
	}
	if precheck.Conflict && !replace {
		auditMetadata["outcome"] = "conflict_without_replace"
		h.manager.RecordPluginAudit(c.Request.Context(), pluginName, "warn", "plugin package import blocked by version conflict", auditMetadata)
		response.Error(c, http.StatusConflict, 60004, "plugin with the same name already exists; enable replace to overwrite")
		return
	}
	installed, err := h.manager.ImportPackage(packagePath, h.pluginsDir, replace)
	if err != nil {
		auditMetadata["outcome"] = "failed"
		auditMetadata["error"] = err.Error()
		h.manager.RecordPluginAudit(c.Request.Context(), pluginName, "error", "plugin package import failed", auditMetadata)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "already installed") || strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		response.Error(c, status, 60004, err.Error())
		return
	}
	auditMetadata["outcome"] = "imported"
	hotReloaded := replace && installed.Manifest.IsUserLevel() && installed.Status == StatusRunning
	auditMetadata["hot_reloaded"] = hotReloaded
	h.manager.RecordPluginAudit(c.Request.Context(), installed.Manifest.Name, "info", "plugin package imported by admin", auditMetadata)

	payload := h.pluginPayload(installed)
	payload["hot_reloaded"] = hotReloaded
	response.Success(c, payload)
}

// PrecheckPluginPackage 校验插件包并预览权限、文件和冲突
// POST /api/v1/plugin-packages/precheck
func (h *Handler) PrecheckPluginPackage(c *gin.Context) {
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

	tempDir, err := os.MkdirTemp("", "campusos-plugin-precheck-upload-*")
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

	precheck, err := PrecheckPluginPackage(packagePath, h.pluginsDir)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 60004, err.Error())
		return
	}
	response.Success(c, precheck)
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
	name := c.Param("name")
	var req struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil || req.SnapshotID == "" {
		response.Error(c, http.StatusBadRequest, 60005, "snapshot_id is required")
		return
	}
	installed, err := h.manager.RollbackVersionSnapshot(name, req.SnapshotID, h.pluginsDir)
	if err != nil {
		response.Error(c, lifecycleErrorStatus(err), 60004, err.Error())
		return
	}
	actorID, actorName := currentActor(c)
	h.manager.RecordPluginAudit(c.Request.Context(), name, "warn", "plugin rollback requested by admin", map[string]interface{}{
		"actor_id": actorID, "actor_name": actorName, "snapshot_id": req.SnapshotID,
	})
	response.Success(c, h.pluginPayload(installed))
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
	if classification == LegacyBuiltin {
		payload["lifecycle_owner"] = "builtin-feature-registry"
		if h.features != nil {
			if featureID, ok := legacyBuiltinFeatureID(p.Manifest.Name); ok {
				if state, found := h.features.Get(featureID); found {
					payload["feature_state"] = state
				}
			}
		}
	} else {
		payload["lifecycle_owner"] = "plugin-platform"
	}
	payload["capability_class"] = classification
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
