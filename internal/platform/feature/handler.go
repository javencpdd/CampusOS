package feature

import (
	"fmt"
	"net/http"
	"strings"

	modulecatalog "github.com/campusos/CampusOS/modules"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

const (
	errorFeatureNotFound = 63003
	errorFeatureInvalid  = 63005
	errorFeatureFailed   = 63004
)

// Handler is the independent management plane for Core configuration and
// Built-in Feature lifecycle. It never installs, exports, or runs plugins.
type Handler struct {
	registry *Registry
	catalog  *modulecatalog.Catalog
}

func NewHandler(registry *Registry, catalog *modulecatalog.Catalog) *Handler {
	return &Handler{registry: registry, catalog: catalog}
}

func (h *Handler) List(c *gin.Context) {
	items := make([]gin.H, 0)
	for _, descriptor := range h.catalog.FeatureDescriptors() {
		resolved, _ := h.catalog.Resolve(descriptor.FeatureID)
		items = append(items, h.payload(resolved, false))
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) Get(c *gin.Context) {
	resolved, ok := h.resolve(c.Param("id"))
	if !ok {
		response.Error(c, http.StatusNotFound, errorFeatureNotFound, "built-in feature or core module not found")
		return
	}
	response.Success(c, h.payload(resolved, true))
}

func (h *Handler) Enable(c *gin.Context)  { h.request(c, true) }
func (h *Handler) Disable(c *gin.Context) { h.request(c, false) }

func (h *Handler) request(c *gin.Context, enabled bool) {
	resolved, ok := h.resolve(c.Param("id"))
	if !ok {
		response.Error(c, http.StatusNotFound, errorFeatureNotFound, "built-in feature or core module not found")
		return
	}
	state, err := h.registry.Request(resolved.Descriptor.FeatureID, enabled)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "always-on") {
			status = http.StatusConflict
		}
		response.Error(c, status, errorFeatureFailed, err.Error())
		return
	}
	payload := h.payload(resolved, false)
	payload["state"] = state
	payload["message"] = featureMessage(state, enabled)
	response.Success(c, payload)
}

func (h *Handler) UpdateConfig(c *gin.Context) {
	resolved, ok := h.resolve(c.Param("id"))
	if !ok {
		response.Error(c, http.StatusNotFound, errorFeatureNotFound, "built-in feature or core module not found")
		return
	}
	var input map[string]interface{}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, errorFeatureInvalid, "invalid config: "+err.Error())
		return
	}
	projected, err := h.updateConfig(resolved, input)
	if err != nil {
		response.Error(c, http.StatusBadRequest, errorFeatureInvalid, err.Error())
		return
	}
	response.Success(c, gin.H{
		"id":             resolved.Descriptor.FeatureID,
		"module_id":      resolved.Descriptor.ID,
		"config_source":  configSourceID(resolved),
		"config_section": resolved.ConfigSection,
		"config":         projected,
	})
}

func (h *Handler) updateConfig(resolved modulecatalog.Resolved, input map[string]interface{}) (map[string]interface{}, error) {
	root, projected, err := modulecatalog.NormalizeConfig(resolved, input, h.registry.Config(resolved.Descriptor.FeatureID))
	if err != nil {
		return nil, err
	}
	if err := h.registry.UpdateConfig(resolved.Descriptor.FeatureID, root); err != nil {
		return nil, err
	}
	return projected, nil
}

func (h *Handler) resolve(identifier string) (modulecatalog.Resolved, bool) {
	resolved, ok := h.catalog.Resolve(identifier)
	if !ok || resolved.Descriptor.FeatureID == "" {
		return modulecatalog.Resolved{}, false
	}
	if _, registered := h.registry.Get(resolved.Descriptor.FeatureID); !registered {
		return modulecatalog.Resolved{}, false
	}
	return resolved, true
}

func (h *Handler) payload(resolved modulecatalog.Resolved, detail bool) gin.H {
	descriptor := resolved.Descriptor
	state, _ := h.registry.Get(descriptor.FeatureID)
	status := "stopped"
	if state.Enabled {
		status = "running"
	}
	payload := gin.H{
		"id":                       descriptor.FeatureID,
		"module_id":                descriptor.ID,
		"name":                     descriptor.Name,
		"display_name":             descriptor.DisplayName,
		"description":              descriptor.Description,
		"version":                  descriptor.Version,
		"kind":                     descriptor.Kind,
		"lifecycle_owner":          "builtin-feature-registry",
		"activation_mode":          state.Mode,
		"backend_activation_mode":  state.Mode,
		"frontend_activation_mode": HotGated,
		"enabled":                  state.Enabled,
		"desired_enabled":          state.DesiredEnabled,
		"pending_restart":          state.PendingRestart,
		"status":                   status,
		"dependencies":             descriptor.Dependencies,
		"config_sources":           configSources(descriptor),
		"capability_class":         descriptor.Kind,
	}
	if descriptor.Kind == modulecatalog.KindCore {
		payload["lifecycle_owner"] = "core-module"
	}
	if detail {
		payload["config_source"] = configSourceID(resolved)
		payload["config_section"] = resolved.ConfigSection
		payload["config"] = modulecatalog.ConfigView(resolved, h.registry.Config(descriptor.FeatureID))
		payload["config_schema"] = modulecatalog.ConfigSchemaFor(resolved)
		payload["config_sections"] = descriptor.ConfigSections
	}
	return payload
}

func configSources(descriptor modulecatalog.Descriptor) []gin.H {
	items := make([]gin.H, 0)
	for _, alias := range descriptor.CompatibilityAliases {
		label := descriptor.DisplayName
		if section, ok := descriptor.ConfigSections[alias.ConfigSection]; ok && section.DisplayName != "" {
			label = section.DisplayName
		}
		items = append(items, gin.H{"id": alias.Name, "label": label, "section": alias.ConfigSection})
	}
	if len(items) == 0 && descriptor.ConfigSchema != nil {
		items = append(items, gin.H{"id": descriptor.FeatureID, "label": descriptor.DisplayName})
	}
	return items
}

func configSourceID(resolved modulecatalog.Resolved) string {
	if resolved.Alias != "" {
		return resolved.Alias
	}
	return resolved.Descriptor.FeatureID
}

func featureMessage(state State, enabled bool) string {
	if state.Mode == AlwaysOn {
		return "core module remains enabled; its policy configuration was not changed"
	}
	if state.Mode == Restart {
		if enabled {
			return "built-in feature enable staged; restart the API server to apply"
		}
		return "built-in feature disable staged; restart the API server to apply"
	}
	if enabled {
		return "built-in feature enabled"
	}
	return "built-in feature disabled"
}

// Compatibility methods support deprecated /plugins/<legacy-builtin> routes
// without registering those modules in the external Plugin Manager.
func (h *Handler) CompatibilityDescriptor(identifier string) (modulecatalog.Resolved, bool) {
	return h.resolve(identifier)
}

func (h *Handler) CompatibilityState(identifier string, enabled bool) (State, error) {
	resolved, ok := h.resolve(identifier)
	if !ok {
		return State{}, fmt.Errorf("built-in feature %q not found", identifier)
	}
	return h.registry.Request(resolved.Descriptor.FeatureID, enabled)
}

// CompatibilityGet powers deprecated /plugins/<builtin-name> reads while the
// external plugin list remains free of module entries.
func (h *Handler) CompatibilityGet(identifier string) (map[string]interface{}, bool) {
	resolved, ok := h.resolve(identifier)
	if !ok || resolved.Alias == "" {
		return nil, false
	}
	return map[string]interface{}(h.payload(resolved, true)), true
}

func (h *Handler) CompatibilityUpdateConfig(identifier string, input map[string]interface{}) (map[string]interface{}, error) {
	resolved, ok := h.resolve(identifier)
	if !ok || resolved.Alias == "" {
		return nil, fmt.Errorf("built-in feature %q not found", identifier)
	}
	return h.updateConfig(resolved, input)
}

func (h *Handler) CompatibilityRequest(identifier string, enabled bool) (map[string]interface{}, error) {
	resolved, ok := h.resolve(identifier)
	if !ok || resolved.Alias == "" {
		return nil, fmt.Errorf("built-in feature %q not found", identifier)
	}
	state, err := h.registry.Request(resolved.Descriptor.FeatureID, enabled)
	if err != nil {
		return nil, err
	}
	payload := h.payload(resolved, false)
	payload["state"] = state
	payload["message"] = featureMessage(state, enabled)
	return map[string]interface{}(payload), nil
}

func (h *Handler) CompatibilityKnown(identifier string) bool {
	resolved, ok := h.resolve(identifier)
	return ok && resolved.Alias != ""
}
