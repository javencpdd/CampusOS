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
	list    func() []*Plugin
	get     func(string) (*Plugin, bool)
	plugins map[string]*Plugin
}

func (c *PluginCatalog) List() []*Plugin                 { return c.list() }
func (c *PluginCatalog) Get(name string) (*Plugin, bool) { return c.get(name) }
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

type LifecycleService struct {
	start          func(string) error
	stop           func(string) error
	requestEnable  func(string) error
	requestDisable func(string) error
}

func (s *LifecycleService) Start(name string) error          { return s.start(name) }
func (s *LifecycleService) Stop(name string) error           { return s.stop(name) }
func (s *LifecycleService) RequestEnable(name string) error  { return s.requestEnable(name) }
func (s *LifecycleService) RequestDisable(name string) error { return s.requestDisable(name) }

type ConfigService struct {
	get    func(string) (map[string]interface{}, bool)
	update func(string, map[string]interface{}) (map[string]interface{}, error)
}

func (s *ConfigService) Get(name string) (map[string]interface{}, bool) { return s.get(name) }
func (s *ConfigService) Update(name string, value map[string]interface{}) (map[string]interface{}, error) {
	return s.update(name, value)
}

type UIRegistry struct {
	revision  func() uint64
	subscribe func() (<-chan uint64, func())
}

func (r *UIRegistry) Revision() uint64                   { return r.revision() }
func (r *UIRegistry) Subscribe() (<-chan uint64, func()) { return r.subscribe() }

type EventRegistry struct {
	dispatch      func(context.Context, *EventMessage)
	subscriptions map[string][]string
}

func (r *EventRegistry) Dispatch(ctx context.Context, event *EventMessage) { r.dispatch(ctx, event) }

type AuditLogService struct {
	record func(context.Context, string, string, string, map[string]interface{})
	list   func(context.Context, string, int) ([]*PluginLogRecord, error)
}

func (s *AuditLogService) Record(ctx context.Context, plugin, level, message string, metadata map[string]interface{}) {
	s.record(ctx, plugin, level, message, metadata)
}
func (s *AuditLogService) List(ctx context.Context, plugin string, limit int) ([]*PluginLogRecord, error) {
	return s.list(ctx, plugin, limit)
}
