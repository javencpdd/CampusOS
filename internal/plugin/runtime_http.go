package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	modulecatalog "github.com/campusos/CampusOS/modules"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

const (
	maxExtensionRequestBytes = int64(1 << 20)
	extensionTimeout         = 5 * time.Second
)

type PermissionChecker func(context.Context, string, string, string) (bool, error)

type RuntimeHTTPHandler struct {
	manager  *Manager
	check    PermissionChecker
	features *platformfeature.Registry
	modules  *modulecatalog.Catalog
}

func (h *RuntimeHTTPHandler) SetModuleCatalog(catalog *modulecatalog.Catalog) {
	h.modules = catalog
}

func NewRuntimeHTTPHandler(manager *Manager, check PermissionChecker, registries ...*platformfeature.Registry) *RuntimeHTTPHandler {
	handler := &RuntimeHTTPHandler{manager: manager, check: check}
	if len(registries) > 0 {
		handler.features = registries[0]
	}
	return handler
}

func (h *RuntimeHTTPHandler) RuntimeManifest(c *gin.Context) {
	userID := contextString(c, "user_id")
	plugins := h.manager.ListPlugins()
	items := make([]gin.H, 0, len(plugins))
	for _, p := range plugins {
		if p == nil || p.Manifest == nil || p.Manifest.UI.Empty() || p.FrontendState == FrontendUnloaded {
			continue
		}
		ui := cloneUI(p.Manifest.UI)
		ui.Actions = h.visibleActions(c.Request.Context(), userID, ui.Actions)
		allowedActions := map[string]bool{}
		for _, action := range ui.Actions {
			allowedActions[action.ID] = true
		}
		for index := range ui.Surfaces {
			filtered := ui.Surfaces[index].ActionIDs[:0]
			for _, id := range ui.Surfaces[index].ActionIDs {
				if allowedActions[id] {
					filtered = append(filtered, id)
				}
			}
			ui.Surfaces[index].ActionIDs = filtered
			ui.Surfaces[index].Schema = filterSchemaActions(ui.Surfaces[index].Schema, allowedActions)
		}
		state, _ := h.manager.LifecycleState(p.ID)
		items = append(items, gin.H{
			"name": p.Manifest.Name, "version": p.Manifest.Version,
			"runtime": p.Manifest.Runtime, "scope": p.Manifest.Scope,
			"lifecycle": state, "ui": ui,
		})
	}
	response.Success(c, gin.H{
		"contract_version": CurrentUIContract,
		"revision":         h.manager.UIRevision(),
		"current_theme":    h.currentTheme(),
		"plugins":          items,
		"modules":          h.moduleContributions(),
	})
}

func (h *RuntimeHTTPHandler) moduleContributions() []gin.H {
	items := make([]gin.H, 0)
	if h.modules == nil || h.features == nil {
		return items
	}
	for _, resolved := range h.modules.UIContributions() {
		descriptor := resolved.Descriptor
		if descriptor.FeatureID == "" || !h.features.Enabled(descriptor.FeatureID) {
			continue
		}
		state, _ := h.features.Get(descriptor.FeatureID)
		items = append(items, gin.H{
			"name": descriptor.Name, "module_id": descriptor.ID,
			"feature_id": descriptor.FeatureID, "version": descriptor.Version,
			"kind": descriptor.Kind, "lifecycle": moduleLifecycle(state), "ui": descriptor.UI,
		})
	}
	return items
}

func moduleLifecycle(state platformfeature.State) gin.H {
	backendState := BackendStopped
	frontendState := FrontendUnloaded
	health := HealthUnavailable
	if state.Enabled {
		backendState = BackendRunning
		frontendState = FrontendLoaded
		health = HealthHealthy
	} else if state.PendingRestart {
		backendState = BackendPendingRestart
	}
	return gin.H{
		"scope": ScopeSystem, "activation_mode": state.Mode,
		"backend_activation_mode": state.Mode, "frontend_activation_mode": ActivationHot,
		"backend_state": backendState, "frontend_state": frontendState, "health": health,
		"desired_enabled": state.DesiredEnabled, "pending_restart": state.PendingRestart,
	}
}

func (h *RuntimeHTTPHandler) currentTheme() string {
	if h.features != nil {
		root := h.features.Config("appearance")
		if config, ok := root["web_theme"].(map[string]interface{}); ok {
			value, _ := config["default_style_pack"].(string)
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func filterSchemaActions(node map[string]interface{}, allowed map[string]bool) map[string]interface{} {
	if node == nil {
		return nil
	}
	if node["component"] == "button" {
		if actionID, _ := node["action_id"].(string); actionID != "" && !allowed[actionID] {
			return nil
		}
	}
	if children, ok := node["children"].([]interface{}); ok {
		filtered := make([]interface{}, 0, len(children))
		for _, child := range children {
			if childMap, ok := child.(map[string]interface{}); ok {
				if next := filterSchemaActions(childMap, allowed); next != nil {
					filtered = append(filtered, next)
				}
			}
		}
		node["children"] = filtered
	}
	return node
}

func cloneUI(input UIContribution) UIContribution {
	data, _ := json.Marshal(input)
	var output UIContribution
	_ = json.Unmarshal(data, &output)
	return output
}

func (h *RuntimeHTTPHandler) Events(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	updates, unsubscribe := h.manager.SubscribeUI()
	defer unsubscribe()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(c.Writer, ": keepalive\n\n")
			c.Writer.Flush()
		case revision, ok := <-updates:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(c.Writer, "event: revision\ndata: %d\n\n", revision)
			c.Writer.Flush()
		}
	}
}

func (h *RuntimeHTTPHandler) Extension(c *gin.Context) {
	pluginName := strings.TrimSpace(c.Param("plugin"))
	path := "/" + strings.TrimPrefix(c.Param("path"), "/")
	p, ok := h.manager.GetPlugin(pluginName)
	if !ok || p == nil || p.Manifest == nil {
		response.Error(c, http.StatusNotFound, 60003, "plugin not found")
		return
	}
	action, ok := declaredAction(p.Manifest.UI.Actions, c.Request.Method, path)
	if !ok {
		response.Error(c, http.StatusNotFound, 60003, "extension action not declared")
		return
	}
	userID := contextString(c, "user_id")
	if allowed, err := h.allowed(c.Request.Context(), userID, action.Permission); err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, "permission check failed")
		return
	} else if !allowed {
		response.Error(c, http.StatusForbidden, 20004, "permission denied")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxExtensionRequestBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.Error(c, http.StatusRequestEntityTooLarge, 60005, "extension request body too large")
		return
	}
	traceID := contextString(c, "trace_id")
	request := &ExtensionRequest{
		Method: c.Request.Method, Path: path, Query: c.Request.URL.Query(), Body: body,
		Headers: map[string]string{"Content-Type": c.GetHeader("Content-Type"), "Accept": c.GetHeader("Accept")},
		Caller:  TrustedCallContext{UserID: userID, Username: contextString(c, "username"), TraceID: traceID},
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), extensionTimeout)
	defer cancel()
	started := time.Now()
	result, err := h.manager.DispatchExtension(ctx, pluginName, request)
	metadata := map[string]interface{}{"method": request.Method, "path": path, "user_id": userID, "trace_id": traceID, "duration_ms": time.Since(started).Milliseconds()}
	if err != nil {
		metadata["error"] = err.Error()
		h.manager.RecordPluginAudit(c.Request.Context(), pluginName, "error", "extension request failed", metadata)
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "unavailable") {
			status = http.StatusServiceUnavailable
		}
		if ctx.Err() == context.DeadlineExceeded {
			status = http.StatusGatewayTimeout
		}
		response.Error(c, status, 60004, err.Error())
		return
	}
	if result == nil {
		metadata["error"] = "runtime returned an empty response"
		h.manager.RecordPluginAudit(c.Request.Context(), pluginName, "error", "extension request failed", metadata)
		response.Error(c, http.StatusBadGateway, 60004, "extension runtime returned an empty response")
		return
	}
	metadata["status"] = result.Status
	h.manager.RecordPluginAudit(c.Request.Context(), pluginName, "info", "extension request completed", metadata)
	status := result.Status
	if status < 100 || status > 599 {
		status = http.StatusOK
	}
	contentType := result.Headers["Content-Type"]
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(status, contentType, result.Body)
}

func (h *RuntimeHTTPHandler) visibleActions(ctx context.Context, userID string, actions []UIAction) []UIAction {
	result := make([]UIAction, 0, len(actions))
	for _, action := range actions {
		allowed, err := h.allowed(ctx, userID, action.Permission)
		if err == nil && allowed {
			result = append(result, action)
		}
	}
	return result
}

func (h *RuntimeHTTPHandler) allowed(ctx context.Context, userID, permission string) (bool, error) {
	if permission == "" {
		return true, nil
	}
	parts := strings.SplitN(permission, ":", 2)
	if len(parts) != 2 || h.check == nil {
		return false, nil
	}
	return h.check(ctx, userID, parts[0], parts[1])
}

func declaredAction(actions []UIAction, method, path string) (UIAction, bool) {
	for _, action := range actions {
		if strings.EqualFold(action.Method, method) && action.Path == path {
			return action, true
		}
	}
	return UIAction{}, false
}

func contextString(c *gin.Context, key string) string {
	value, ok := c.Get(key)
	if !ok {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
