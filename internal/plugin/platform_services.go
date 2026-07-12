package plugin

import (
	"context"
	"fmt"
	"sync"
)

type CapabilityClass string

const (
	ExternalPlugin CapabilityClass = "external-plugin"
	LegacyBuiltin  CapabilityClass = "legacy-builtin"
)

var legacyBuiltinMapping = map[string]string{
	"campus-welcome":      "compat.builtin.campus-welcome",
	"category-moderation": "core.moderation", "personal-space": "feature.personal-space",
	"controlled-richtext-article": "feature.controlled-richtext-article", "personal-schedule": "feature.personal-schedule",
	"homepage-customizer": "feature.appearance", "web-theme": "feature.appearance",
}

type PluginCatalog struct {
	manager *Manager
	plugins map[string]*Plugin
}

func (c *PluginCatalog) List() []*Plugin                 { return c.manager.ListPlugins() }
func (c *PluginCatalog) Get(name string) (*Plugin, bool) { return c.manager.GetPlugin(name) }
func (c *PluginCatalog) Classify(manifest *Manifest) CapabilityClass {
	if manifest != nil && manifest.Runtime == "builtin" {
		if _, ok := legacyBuiltinMapping[manifest.Name]; ok {
			return LegacyBuiltin
		}
	}
	return ExternalPlugin
}
func (c *PluginCatalog) LegacyModule(name string) (string, bool) {
	value, ok := legacyBuiltinMapping[name]
	return value, ok
}
func (c *PluginCatalog) ListExternal() []*Plugin {
	all := c.List()
	result := make([]*Plugin, 0, len(all))
	for _, p := range all {
		if c.Classify(p.Manifest) == ExternalPlugin {
			result = append(result, p)
		}
	}
	return result
}
func (c *PluginCatalog) ListLegacyBuiltins() []*Plugin {
	all := c.List()
	result := make([]*Plugin, 0, len(all))
	for _, p := range all {
		if c.Classify(p.Manifest) == LegacyBuiltin {
			result = append(result, p)
		}
	}
	return result
}

type RuntimeRegistry struct {
	mu       sync.RWMutex
	runtimes map[string]Runtime
}

func NewRuntimeRegistry() *RuntimeRegistry { return &RuntimeRegistry{runtimes: map[string]Runtime{}} }
func (r *RuntimeRegistry) Register(kind string, runtime Runtime) error {
	if kind == "" || runtime == nil {
		return fmt.Errorf("runtime type and implementation are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runtimes[kind] = runtime
	return nil
}
func (r *RuntimeRegistry) Get(kind string) (Runtime, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.runtimes[kind]
	return value, ok
}

type LifecycleService struct{ manager *Manager }

func (s *LifecycleService) Start(name string) error          { return s.manager.start(name) }
func (s *LifecycleService) Stop(name string) error           { return s.manager.stop(name, true) }
func (s *LifecycleService) RequestEnable(name string) error  { return s.manager.requestEnable(name) }
func (s *LifecycleService) RequestDisable(name string) error { return s.manager.requestDisable(name) }

type ConfigService struct{ manager *Manager }

func (s *ConfigService) Get(name string) (map[string]interface{}, bool) {
	return s.manager.GetPluginConfig(name)
}
func (s *ConfigService) Update(name string, value map[string]interface{}) (map[string]interface{}, error) {
	return s.manager.updateConfig(name, value)
}

type UIRegistry struct {
	manager   *Manager
	revision  uint64
	listeners map[chan uint64]struct{}
}

func (r *UIRegistry) Revision() uint64                   { return r.manager.UIRevision() }
func (r *UIRegistry) Subscribe() (<-chan uint64, func()) { return r.manager.SubscribeUI() }

type EventRegistry struct {
	manager       *Manager
	subscriptions map[string][]string
}

func (r *EventRegistry) Dispatch(ctx context.Context, event *EventMessage) {
	r.manager.DispatchEvent(ctx, event)
}

type AuditLogService struct {
	manager *Manager
	repo    PluginLogRepository
}

func (s *AuditLogService) Record(ctx context.Context, plugin, level, message string, metadata map[string]interface{}) {
	s.manager.RecordPluginAudit(ctx, plugin, level, message, metadata)
}
func (s *AuditLogService) List(ctx context.Context, plugin string, limit int) ([]*PluginLogRecord, error) {
	return s.manager.ListPluginLogs(ctx, plugin, limit)
}
