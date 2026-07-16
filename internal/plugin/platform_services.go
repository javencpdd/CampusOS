package plugin

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type CapabilityClass string

const (
	ExternalPlugin CapabilityClass = "external-plugin"
)

// OperationTracker is a narrow platform callback for file-backed plugin
// imports. It records staged work and compensation evidence without giving the
// Plugin Platform a Core database handle.
type OperationTracker interface {
	Track(context.Context, OperationRequest, func(context.Context) error) error
}

// CompatibilityReporter records actual use of a temporary compatibility path.
// It is best effort and deliberately exposes no Core database capability to
// the Plugin Platform.
type CompatibilityReporter interface {
	RecordCompatibility(context.Context, string, string, any) error
}

type OperationRequest struct {
	Kind           string
	SubjectType    string
	SubjectID      string
	ActorID        string
	IdempotencyKey string
}

type PluginCatalog struct {
	mu      sync.RWMutex
	plugins map[string]*Plugin
	repo    PluginRepository
}

func NewPluginCatalog() *PluginCatalog { return &PluginCatalog{plugins: map[string]*Plugin{}} }

func (c *PluginCatalog) List() []*Plugin {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]*Plugin, 0, len(c.plugins))
	for _, item := range c.plugins {
		result = append(result, clonePlugin(item))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (c *PluginCatalog) Get(name string) (*Plugin, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, ok := c.plugins[name]
	return clonePlugin(item), ok
}

func (c *PluginCatalog) IsRunning(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, ok := c.plugins[name]
	return ok && item.Status == StatusRunning
}

func clonePlugin(item *Plugin) *Plugin {
	if item == nil {
		return nil
	}
	copyPlugin := *item
	if item.Manifest != nil {
		copyManifest := *item.Manifest
		copyManifest.Config = copyConfigMap(item.Manifest.Config)
		copyPlugin.Manifest = &copyManifest
	}
	return &copyPlugin
}
func (c *PluginCatalog) Classify(manifest *Manifest) CapabilityClass {
	return ExternalPlugin
}
func (c *PluginCatalog) ListExternal() []*Plugin {
	return c.List()
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
	operationMu sync.Mutex
	catalog     *PluginCatalog
	runtimes    *RuntimeRegistry
	ui          *UIRegistry
	audit       *AuditLogService
}

func NewLifecycleService(catalog *PluginCatalog, runtimes *RuntimeRegistry, ui *UIRegistry, audit *AuditLogService) *LifecycleService {
	return &LifecycleService{catalog: catalog, runtimes: runtimes, ui: ui, audit: audit}
}
func (s *LifecycleService) Enable(name string) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	return s.enable(name)
}
func (s *LifecycleService) Start(name string) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	return s.start(name)
}
func (s *LifecycleService) Stop(name string) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	return s.stop(name, true)
}
func (s *LifecycleService) RequestEnable(name string) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	return s.requestEnable(name)
}
func (s *LifecycleService) RequestDisable(name string) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	return s.requestDisable(name)
}

type ConfigService struct {
	catalog *PluginCatalog
	ui      *UIRegistry
	audit   *AuditLogService
}

func NewConfigService(catalog *PluginCatalog, ui *UIRegistry, audit *AuditLogService) *ConfigService {
	return &ConfigService{catalog: catalog, ui: ui, audit: audit}
}

type UIRegistry struct {
	mu        sync.RWMutex
	revision  uint64
	listeners map[chan uint64]struct{}
}

func NewUIRegistry() *UIRegistry {
	return &UIRegistry{revision: 1, listeners: map[chan uint64]struct{}{}}
}
func (r *UIRegistry) Revision() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.revision
}
func (r *UIRegistry) Subscribe() (<-chan uint64, func()) {
	ch := make(chan uint64, 1)
	r.mu.Lock()
	r.listeners[ch] = struct{}{}
	revision := r.revision
	r.mu.Unlock()
	ch <- revision
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.listeners[ch]; ok {
			delete(r.listeners, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}
func (r *UIRegistry) Bump() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revision++
	for listener := range r.listeners {
		select {
		case listener <- r.revision:
		default:
		}
	}
	return r.revision
}

type EventRegistry struct {
	mu            sync.RWMutex
	subscriptions map[string][]string
	catalog       *PluginCatalog
	runtimes      *RuntimeRegistry
	audit         *AuditLogService
	lifecycle     *LifecycleService
}

func NewEventRegistry(catalog *PluginCatalog, runtimes *RuntimeRegistry, audit *AuditLogService, lifecycle *LifecycleService) *EventRegistry {
	return &EventRegistry{subscriptions: map[string][]string{}, catalog: catalog, runtimes: runtimes, audit: audit, lifecycle: lifecycle}
}
func (r *EventRegistry) Add(plugin string, eventTypes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, eventType := range eventTypes {
		r.subscriptions[eventType] = append(r.subscriptions[eventType], plugin)
	}
}
func (r *EventRegistry) Remove(plugin string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for eventType, names := range r.subscriptions {
		filtered := names[:0]
		for _, name := range names {
			if name != plugin {
				filtered = append(filtered, name)
			}
		}
		r.subscriptions[eventType] = filtered
	}
}
func (r *EventRegistry) Subscribers(eventType string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.subscriptions[eventType]...)
}

type AuditLogService struct {
	mu   sync.RWMutex
	repo PluginLogRepository
}

func NewAuditLogService() *AuditLogService { return &AuditLogService{} }
func (s *AuditLogService) SetRepository(repo PluginLogRepository) {
	s.mu.Lock()
	s.repo = repo
	s.mu.Unlock()
}
func (s *AuditLogService) Record(ctx context.Context, plugin, level, message string, metadata map[string]interface{}) {
	if plugin == "" {
		plugin = "unknown"
	}
	if level == "" {
		level = "info"
	}
	if message == "" {
		message = "plugin audit"
	}
	s.Log(ctx, &PluginLogRecord{PluginName: plugin, Level: level, Message: message, EventType: "plugin.package", Metadata: metadata})
}
func (s *AuditLogService) List(ctx context.Context, plugin string, limit int) ([]*PluginLogRecord, error) {
	s.mu.RLock()
	repo := s.repo
	s.mu.RUnlock()
	if repo == nil {
		return []*PluginLogRecord{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return repo.ListLogs(ctx, plugin, limit)
}

// PackageService owns package discovery, installation, replacement and export.
// PluginCatalog remains the only owner of installed-plugin state.
type PackageService struct {
	operationMu   sync.Mutex
	catalog       *PluginCatalog
	lifecycle     *LifecycleService
	ui            *UIRegistry
	events        *EventRegistry
	audit         *AuditLogService
	tracker       OperationTracker
	compatibility CompatibilityReporter
}

func NewPackageService(catalog *PluginCatalog, lifecycle *LifecycleService, ui *UIRegistry, events *EventRegistry, audit *AuditLogService) *PackageService {
	return &PackageService{catalog: catalog, lifecycle: lifecycle, ui: ui, events: events, audit: audit}
}

func (s *PackageService) SetOperationTracker(tracker OperationTracker) {
	s.tracker = tracker
}

func (s *PackageService) SetCompatibilityReporter(reporter CompatibilityReporter) {
	s.compatibility = reporter
}

// SnapshotService owns version-snapshot lookup and rollback orchestration.
type SnapshotService struct {
	catalog  *PluginCatalog
	packages *PackageService
	audit    *AuditLogService
}

func NewSnapshotService(catalog *PluginCatalog, packages *PackageService, audit *AuditLogService) *SnapshotService {
	return &SnapshotService{catalog: catalog, packages: packages, audit: audit}
}

// HostAccessService is the only plugin-platform service that authorizes Host API
// tokens or dispatches extension requests to a runtime.
type HostAccessService struct {
	catalog  *PluginCatalog
	runtimes *RuntimeRegistry
}

func NewHostAccessService(catalog *PluginCatalog, runtimes *RuntimeRegistry) *HostAccessService {
	return &HostAccessService{catalog: catalog, runtimes: runtimes}
}
