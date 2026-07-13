package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Manager 插件管理器
type Manager struct {
	mu          sync.RWMutex
	runtimes    *RuntimeRegistry
	repo        PluginRepository // 可选：插件持久化仓储
	catalog     *PluginCatalog
	lifecycle   *LifecycleService
	configs     *ConfigService
	ui          *UIRegistry
	events      *EventRegistry
	audit       *AuditLogService
	auditRepo   PluginLogRepository
	uiRevision  uint64
	uiListeners map[chan uint64]struct{}
}

// NewManager 创建插件管理器
func NewManager() *Manager {
	m := &Manager{
		runtimes:    NewRuntimeRegistry(),
		uiRevision:  1,
		uiListeners: make(map[chan uint64]struct{}),
	}
	m.catalog = &PluginCatalog{list: m.ListPlugins, get: m.GetPlugin, plugins: make(map[string]*Plugin)}
	m.lifecycle = &LifecycleService{start: m.start, stop: func(name string) error { return m.stop(name, true) }, requestEnable: m.requestEnable, requestDisable: m.requestDisable}
	m.configs = &ConfigService{get: m.GetPluginConfig, update: m.updateConfig}
	m.ui = &UIRegistry{revision: m.UIRevision, subscribe: m.SubscribeUI}
	m.events = &EventRegistry{dispatch: m.DispatchEvent, subscriptions: make(map[string][]string)}
	m.audit = &AuditLogService{record: m.RecordPluginAudit, list: m.ListPluginLogs}
	return m
}

func (m *Manager) Catalog() *PluginCatalog           { return m.catalog }
func (m *Manager) RuntimeRegistry() *RuntimeRegistry { return m.runtimes }
func (m *Manager) Lifecycle() *LifecycleService      { return m.lifecycle }
func (m *Manager) Configs() *ConfigService           { return m.configs }
func (m *Manager) UIRegistry() *UIRegistry           { return m.ui }
func (m *Manager) EventRegistry() *EventRegistry     { return m.events }
func (m *Manager) AuditLogs() *AuditLogService       { return m.audit }

// SetPluginRepository 设置插件持久化仓储
func (m *Manager) SetPluginRepository(repo PluginRepository) {
	m.mu.Lock()
	m.repo = repo
	if logRepo, ok := repo.(PluginLogRepository); ok {
		m.auditRepo = logRepo
	}
	plugins := make([]*Plugin, 0, len(m.catalog.plugins))
	for _, p := range m.catalog.plugins {
		plugins = append(plugins, p)
	}
	m.mu.Unlock()

	for _, p := range plugins {
		if err := m.syncPluginRecord(context.Background(), p); err != nil {
			log.Printf("⚠️  同步插件仓储失败: %s (%v)", p.ID, err)
		}
	}
}

// SetPluginLogRepository 设置插件运行日志仓储
func (m *Manager) SetPluginLogRepository(repo PluginLogRepository) {
	m.auditRepo = repo
}

// RegisterRuntime 注册运行时实现
func (m *Manager) RegisterRuntime(runtimeType string, runtime Runtime) {
	if err := m.runtimes.Register(runtimeType, runtime); err != nil {
		panic(err)
	}
	log.Printf("🔌 已注册插件运行时: %s", runtimeType)
}

// Install 从目录安装插件
func (m *Manager) Install(dir string) (*Plugin, error) {
	manifestPath := filepath.Join(dir, "plugin.yaml")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("load manifest from %s: %w", dir, err)
	}

	m.mu.Lock()

	if _, exists := m.catalog.plugins[manifest.Name]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("plugin '%s' already installed", manifest.Name)
	}

	plugin := &Plugin{
		ID:             manifest.Name,
		Manifest:       manifest,
		Status:         StatusInstalled,
		BackendState:   BackendInstalled,
		FrontendState:  FrontendUnloaded,
		Health:         HealthUnknown,
		DesiredEnabled: manifest.IsSystemLevel(),
		Directory:      dir,
	}
	if plugin.DesiredEnabled && !manifest.UI.Empty() {
		plugin.FrontendState = FrontendLoaded
	}
	m.catalog.plugins[manifest.Name] = plugin

	// 注册事件订阅
	for _, eventType := range manifest.Events.Subscribe {
		m.events.subscriptions[eventType] = append(m.events.subscriptions[eventType], manifest.Name)
	}
	m.bumpUIRevisionLocked()
	m.mu.Unlock()

	if err := m.syncPluginRecord(context.Background(), plugin); err != nil {
		return nil, err
	}

	log.Printf("🔌 插件已安装: %s v%s (%s)", manifest.Name, manifest.Version, manifest.Runtime)
	return plugin, nil
}

func (m *Manager) syncPluginRecord(ctx context.Context, p *Plugin) error {
	m.mu.RLock()
	repo := m.repo
	m.mu.RUnlock()
	if repo == nil || p == nil || p.Manifest == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	record, err := repo.GetByName(ctx, p.Manifest.Name)
	if err == nil {
		config := map[string]interface{}{}
		if record.Config != "" {
			if err := json.Unmarshal([]byte(record.Config), &config); err != nil {
				return fmt.Errorf("decode persisted config for plugin %s: %w", p.Manifest.Name, err)
			}
		}
		if len(config) > 0 {
			m.mu.Lock()
			p.Manifest.Config = config
			m.mu.Unlock()
		}
		if record.Status != "" {
			m.mu.Lock()
			p.Status = restoredRuntimeStatus(record.Status)
			p.DesiredEnabled = desiredEnabledFromRecord(record.Status, p.Manifest)
			p.BackendState = restoredBackendState(record.BackendState, p.Status)
			p.FrontendState = restoredFrontendState(record.FrontendState, p)
			p.Health = restoredHealthState(record.HealthState, p.Status)
			p.ErrorMsg = record.ErrorMsg
			p.Checksum = record.Checksum
			p.PackageSize = record.PackageSize
			p.InstalledBy = record.InstalledBy
			m.mu.Unlock()
		}
	} else if !errors.Is(err, ErrAPIKeyNotFound) {
		return err
	} else {
		record = &PluginRecord{}
	}

	configJSON, err := json.Marshal(p.Manifest.Config)
	if err != nil {
		return err
	}
	now := time.Now()
	record.Name = p.Manifest.Name
	record.DisplayName = p.Manifest.DisplayName
	record.Version = p.Manifest.Version
	record.Description = p.Manifest.Description
	record.Author = p.Manifest.Author
	record.Runtime = p.Manifest.Runtime
	record.Status = string(persistedPluginStatus(p))
	record.BackendState = string(p.BackendState)
	record.FrontendState = string(p.FrontendState)
	record.HealthState = string(p.Health)
	record.UIRevision = int64(m.UIRevision())
	record.Config = string(configJSON)
	record.ErrorMsg = p.ErrorMsg
	record.Checksum = p.Checksum
	record.PackageSize = p.PackageSize
	record.InstalledBy = p.InstalledBy
	record.UpdatedAt = now
	if record.InstalledAt.IsZero() {
		record.InstalledAt = now
	}
	if record.DisplayName == "" {
		record.DisplayName = record.Name
	}
	if record.Version == "" {
		record.Version = "0.0.0"
	}
	if record.Runtime == "" {
		record.Runtime = "grpc"
	}
	if record.InstalledBy == "" {
		record.InstalledBy = "system"
	}
	return repo.Save(ctx, record)
}

func restoredBackendState(value string, status PluginStatus) BackendState {
	switch BackendState(value) {
	case BackendInstalled, BackendStarting, BackendRunning, BackendRestarting, BackendStopping, BackendStopped, BackendPendingRestart, BackendError:
		return BackendState(value)
	}
	switch status {
	case StatusRunning:
		return BackendRunning
	case StatusStopped:
		return BackendStopped
	case StatusError:
		return BackendError
	default:
		return BackendInstalled
	}
}

func restoredFrontendState(value string, p *Plugin) FrontendState {
	if p != nil && p.Manifest != nil && p.DesiredEnabled && !p.Manifest.UI.Empty() {
		if FrontendState(value) == FrontendIncompatible || FrontendState(value) == FrontendError {
			return FrontendState(value)
		}
		return FrontendLoaded
	}
	return FrontendUnloaded
}

func restoredHealthState(value string, status PluginStatus) HealthState {
	switch HealthState(value) {
	case HealthHealthy, HealthDegraded, HealthUnavailable, HealthUnknown:
		return HealthState(value)
	}
	if status == StatusRunning {
		return HealthHealthy
	}
	if status == StatusError || status == StatusStopped {
		return HealthUnavailable
	}
	return HealthUnknown
}

// Enable 启用插件
func (m *Manager) Enable(name string) error {
	m.mu.Lock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	if p.Status != StatusInstalled && p.Status != StatusStopped {
		m.mu.Unlock()
		return fmt.Errorf("plugin '%s' cannot be enabled (status: %s)", name, p.Status)
	}
	p.Status = StatusEnabled
	p.DesiredEnabled = true
	m.mu.Unlock()

	return m.Start(name)
}

// RequestEnable applies a lifecycle request from the management API. System
// plugins only persist the target state, while user plugins start immediately.
func (m *Manager) RequestEnable(name string) error {
	return m.lifecycle.RequestEnable(name)
}

func (m *Manager) requestEnable(name string) error {
	m.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	activationMode := p.Manifest.BackendActivationMode()
	status := p.Status
	m.mu.RUnlock()

	if activationMode == ActivationRestart {
		if p.DesiredEnabled && status == StatusRunning {
			return nil
		}
		m.mu.Lock()
		p.DesiredEnabled = true
		p.BackendState = BackendPendingRestart
		if !p.Manifest.UI.Empty() {
			p.FrontendState = FrontendLoaded
		}
		m.bumpUIRevisionLocked()
		m.mu.Unlock()
		m.persistPluginStatus(context.Background(), name, StatusRunning, "")
		m.logPlugin(context.Background(), &PluginLogRecord{
			PluginName: name,
			Level:      "info",
			Message:    "system plugin enable staged for restart",
			Metadata: map[string]interface{}{
				"activation_mode": activationMode,
				"desired_enabled": true,
			},
		})
		return nil
	}
	if status == StatusRunning {
		return nil
	}
	if status == StatusError {
		m.mu.Lock()
		p.Status = StatusInstalled
		p.ErrorMsg = ""
		m.mu.Unlock()
	}
	return m.Enable(name)
}

// RequestDisable applies a lifecycle request from the management API. System
// plugins remain in their current process state until the next server restart.
func (m *Manager) RequestDisable(name string) error {
	return m.lifecycle.RequestDisable(name)
}

func (m *Manager) requestDisable(name string) error {
	m.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	activationMode := p.Manifest.BackendActivationMode()
	status := p.Status
	desiredEnabled := p.DesiredEnabled
	m.mu.RUnlock()

	if activationMode != ActivationRestart {
		if !desiredEnabled && (status == StatusStopped || status == StatusInstalled) {
			return nil
		}
		return m.Stop(name)
	}
	if !desiredEnabled && status != StatusRunning {
		return nil
	}
	m.mu.Lock()
	p.DesiredEnabled = false
	p.BackendState = BackendPendingRestart
	p.FrontendState = FrontendUnloaded
	m.bumpUIRevisionLocked()
	m.mu.Unlock()
	m.persistPluginStatus(context.Background(), name, StatusStopped, "")
	m.logPlugin(context.Background(), &PluginLogRecord{
		PluginName: name,
		Level:      "info",
		Message:    "system plugin disable staged for restart",
		Metadata: map[string]interface{}{
			"activation_mode": activationMode,
			"desired_enabled": false,
		},
	})
	return nil
}

// ReloadUserPlugin preserves the legacy API name while using activation_mode.
func (m *Manager) ReloadUserPlugin(name string) error {
	m.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	if p.Manifest.BackendActivationMode() == ActivationRestart {
		m.mu.RUnlock()
		return fmt.Errorf("plugin '%s' requires a server restart", name)
	}
	status := p.Status
	m.mu.RUnlock()

	if status == StatusRunning {
		if err := m.stop(name, false); err != nil {
			return err
		}
	}
	return m.RequestEnable(name)
}

// LifecycleState returns the current and requested lifecycle behavior for UI
// consumers without exposing mutable Plugin internals.
func (m *Manager) LifecycleState(name string) (LifecycleState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.catalog.plugins[name]
	if !ok || p.Manifest == nil {
		return LifecycleState{}, false
	}
	running := p.Status == StatusRunning
	state := LifecycleState{
		Scope:                  p.Manifest.Scope,
		ActivationMode:         p.Manifest.BackendActivationMode(),
		BackendActivationMode:  p.Manifest.BackendActivationMode(),
		FrontendActivationMode: p.Manifest.FrontendActivationMode(),
		BackendState:           p.BackendState,
		FrontendState:          p.FrontendState,
		Health:                 p.Health,
		DesiredEnabled:         p.DesiredEnabled,
		PendingRestart:         p.Manifest.BackendActivationMode() == ActivationRestart && p.DesiredEnabled != running,
	}
	return state, true
}

func (m *Manager) UIRevision() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.uiRevision
}

func (m *Manager) SubscribeUI() (<-chan uint64, func()) {
	ch := make(chan uint64, 1)
	m.mu.Lock()
	m.uiListeners[ch] = struct{}{}
	revision := m.uiRevision
	m.mu.Unlock()
	ch <- revision
	return ch, func() {
		m.mu.Lock()
		if _, ok := m.uiListeners[ch]; ok {
			delete(m.uiListeners, ch)
			close(ch)
		}
		m.mu.Unlock()
	}
}

func (m *Manager) bumpUIRevisionLocked() {
	m.uiRevision++
	for listener := range m.uiListeners {
		select {
		case listener <- m.uiRevision:
		default:
		}
	}
}

// StartDesiredPlugins starts plugins that were explicitly enabled before the
// current process started. Call this only during server bootstrap.
func (m *Manager) StartDesiredPlugins(scope string) {
	m.mu.RLock()
	names := make([]string, 0, len(m.catalog.plugins))
	for name, p := range m.catalog.plugins {
		if p.Manifest == nil || p.Manifest.Scope != scope || !p.DesiredEnabled || p.Status == StatusRunning {
			continue
		}
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		if err := m.Start(name); err != nil {
			log.Printf("⚠️  插件启动失败: %s (%v)", name, err)
		}
	}
}

// Start 启动插件
func (m *Manager) Start(name string) error {
	return m.lifecycle.Start(name)
}

func (m *Manager) start(name string) error {
	m.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	runtimeType := p.Manifest.Runtime
	m.mu.RUnlock()

	m.mu.Lock()
	p.BackendState = BackendStarting
	p.Health = HealthUnknown
	if p.DesiredEnabled && !p.Manifest.UI.Empty() {
		p.FrontendState = FrontendLoaded
	}
	m.bumpUIRevisionLocked()
	runtime, ok := m.runtimes.Get(runtimeType)
	if !ok {
		err := fmt.Errorf("runtime '%s' not registered", runtimeType)
		p.Status = StatusError
		p.BackendState = BackendError
		p.Health = HealthUnavailable
		p.ErrorMsg = err.Error()
		m.mu.Unlock()
		m.persistPluginStatus(context.Background(), name, StatusError, err.Error())
		m.logPlugin(context.Background(), &PluginLogRecord{
			PluginName: name,
			Level:      "error",
			Message:    "plugin start failed",
			Metadata: map[string]interface{}{
				"runtime": runtimeType,
				"error":   err.Error(),
			},
		})
		return err
	}
	p.HostToken = GenerateAPIKey("cos_plugin")
	p.HostTokenExpiresAt = time.Now().Add(30 * 24 * time.Hour)
	m.mu.Unlock()

	if err := runtime.Start(context.Background(), p); err != nil {
		m.mu.Lock()
		p.Status = StatusError
		p.BackendState = BackendError
		p.Health = HealthUnavailable
		p.ErrorMsg = err.Error()
		p.HostToken = ""
		p.HostTokenExpiresAt = time.Time{}
		m.bumpUIRevisionLocked()
		m.mu.Unlock()
		m.persistPluginStatus(context.Background(), name, StatusError, err.Error())
		m.logPlugin(context.Background(), &PluginLogRecord{
			PluginName: name,
			Level:      "error",
			Message:    "plugin start failed",
			Metadata: map[string]interface{}{
				"runtime": runtimeType,
				"error":   err.Error(),
			},
		})
		return fmt.Errorf("start plugin '%s': %w", name, err)
	}

	m.mu.Lock()
	p.Status = StatusRunning
	p.BackendState = BackendRunning
	p.Health = HealthHealthy
	if !p.Manifest.UI.Empty() {
		p.FrontendState = FrontendLoaded
	}
	p.DesiredEnabled = true
	p.ErrorMsg = ""
	m.bumpUIRevisionLocked()
	m.mu.Unlock()
	m.persistPluginStatus(context.Background(), name, StatusRunning, "")

	log.Printf("🟢 插件已启动: %s", name)
	m.logPlugin(context.Background(), &PluginLogRecord{
		PluginName: name,
		Level:      "info",
		Message:    "plugin started",
		Metadata: map[string]interface{}{
			"runtime": runtimeType,
		},
	})
	return nil
}

// Stop 停止插件
func (m *Manager) Stop(name string) error {
	return m.lifecycle.Stop(name)
}

func (m *Manager) stop(name string, persist bool) error {
	m.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	runtimeType := p.Manifest.Runtime
	m.mu.RUnlock()

	m.mu.Lock()
	p.BackendState = BackendStopping
	p.Health = HealthDegraded
	m.bumpUIRevisionLocked()
	runtime, ok := m.runtimes.Get(runtimeType)
	if !ok {
		m.mu.Unlock()
		m.logPlugin(context.Background(), &PluginLogRecord{
			PluginName: name,
			Level:      "error",
			Message:    "plugin stop failed",
			Metadata: map[string]interface{}{
				"runtime": runtimeType,
				"error":   fmt.Sprintf("runtime '%s' not registered", runtimeType),
			},
		})
		return fmt.Errorf("runtime '%s' not registered", runtimeType)
	}
	m.mu.Unlock()

	if err := runtime.Stop(context.Background(), name); err != nil {
		m.logPlugin(context.Background(), &PluginLogRecord{
			PluginName: name,
			Level:      "error",
			Message:    "plugin stop failed",
			Metadata: map[string]interface{}{
				"runtime": runtimeType,
				"error":   err.Error(),
			},
		})
		return fmt.Errorf("stop plugin '%s': %w", name, err)
	}

	m.mu.Lock()
	p.Status = StatusStopped
	p.BackendState = BackendStopped
	p.Health = HealthUnavailable
	p.HostToken = ""
	p.HostTokenExpiresAt = time.Time{}
	if persist {
		p.DesiredEnabled = false
		p.FrontendState = FrontendUnloaded
	}
	m.bumpUIRevisionLocked()
	m.mu.Unlock()
	if persist {
		m.persistPluginStatus(context.Background(), name, StatusStopped, "")
	}

	log.Printf("🔴 插件已停止: %s", name)
	m.logPlugin(context.Background(), &PluginLogRecord{
		PluginName: name,
		Level:      "info",
		Message:    "plugin stopped",
		Metadata: map[string]interface{}{
			"runtime": runtimeType,
		},
	})
	return nil
}

// AuthorizeHostAPI authenticates a running plugin without exposing its token.
// Tokens rotate on every start/reload and are revoked on stop.
func (m *Manager) AuthorizeHostAPI(name, token string) (*Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.catalog.plugins[name]
	if !ok || p == nil || p.Manifest == nil || p.Status != StatusRunning {
		return nil, false
	}
	if token == "" || p.HostToken == "" || token != p.HostToken || time.Now().After(p.HostTokenExpiresAt) {
		return nil, false
	}
	return p, true
}

// DispatchBeforeEvent 分发 .before 事件（同步，可被插件拦截）
func (m *Manager) DispatchBeforeEvent(ctx context.Context, event *EventMessage) *PluginResponse {
	beforeEvent := &EventMessage{
		Type:    event.Type + ".before",
		Source:  event.Source,
		Subject: event.Subject,
		Data:    event.Data,
	}

	m.mu.RLock()
	pluginNames := m.events.subscriptions[event.Type]
	plugins := make([]*Plugin, 0, len(pluginNames))
	for _, name := range pluginNames {
		if p, ok := m.catalog.plugins[name]; ok && p.Status == StatusRunning {
			plugins = append(plugins, p)
		}
	}
	m.mu.RUnlock()

	for _, p := range plugins {
		m.mu.RLock()
		runtimeType := p.Manifest.Runtime
		m.mu.RUnlock()

		m.mu.Lock()
		runtime, ok := m.runtimes.Get(runtimeType)
		m.mu.Unlock()
		if !ok {
			continue
		}

		resp, err := runtime.SendEvent(ctx, p.ID, beforeEvent)
		if err != nil {
			log.Printf("⚠️  插件 %s 处理 .before 事件 %s 失败: %v", p.ID, event.Type, err)
			m.markPluginError(p.ID, err)
			m.logPlugin(ctx, &PluginLogRecord{
				PluginName: p.ID,
				Level:      "error",
				Message:    "plugin before-event failed",
				EventType:  beforeEvent.Type,
				Metadata:   eventLogMetadata(beforeEvent, err),
			})
			continue
		}
		if resp != nil && !resp.Allowed {
			log.Printf("🚫 插件 %s 阻止了事件 %s: %s", p.ID, event.Type, resp.Message)
			m.logPlugin(ctx, &PluginLogRecord{
				PluginName: p.ID,
				Level:      "warn",
				Message:    "plugin blocked before-event",
				EventType:  beforeEvent.Type,
				Metadata:   eventLogMetadata(beforeEvent, nil),
			})
			return resp
		}
		m.logPlugin(ctx, &PluginLogRecord{
			PluginName: p.ID,
			Level:      "info",
			Message:    "plugin handled before-event",
			EventType:  beforeEvent.Type,
			Metadata:   eventLogMetadata(beforeEvent, nil),
		})
	}
	return nil
}

// DispatchEvent 分发 .after 事件到所有订阅的插件（异步）
func (m *Manager) DispatchEvent(ctx context.Context, event *EventMessage) {
	m.mu.RLock()
	pluginNames := m.events.subscriptions[event.Type]
	plugins := make([]*Plugin, 0, len(pluginNames))
	for _, name := range pluginNames {
		if p, ok := m.catalog.plugins[name]; ok && p.Status == StatusRunning {
			plugins = append(plugins, p)
		}
	}
	m.mu.RUnlock()

	for _, p := range plugins {
		go func(pl *Plugin) {
			m.mu.RLock()
			runtimeType := pl.Manifest.Runtime
			m.mu.RUnlock()

			m.mu.Lock()
			runtime, ok := m.runtimes.Get(runtimeType)
			m.mu.Unlock()
			if !ok {
				return
			}

			resp, err := runtime.SendEvent(ctx, pl.ID, event)
			if err != nil {
				log.Printf("⚠️  插件 %s 处理事件 %s 失败: %v", pl.ID, event.Type, err)
				m.markPluginError(pl.ID, err)
				m.logPlugin(ctx, &PluginLogRecord{
					PluginName: pl.ID,
					Level:      "error",
					Message:    "plugin event failed",
					EventType:  event.Type,
					Metadata:   eventLogMetadata(event, err),
				})
				return
			}
			if resp != nil && !resp.Allowed {
				log.Printf("🚫 插件 %s 拒绝事件 %s: %s", pl.ID, event.Type, resp.Message)
				m.logPlugin(ctx, &PluginLogRecord{
					PluginName: pl.ID,
					Level:      "warn",
					Message:    "plugin rejected event",
					EventType:  event.Type,
					Metadata:   eventLogMetadata(event, nil),
				})
				return
			}
			m.logPlugin(ctx, &PluginLogRecord{
				PluginName: pl.ID,
				Level:      "info",
				Message:    "plugin handled event",
				EventType:  event.Type,
				Metadata:   eventLogMetadata(event, nil),
			})
		}(p)
	}
}

// GetPlugin 获取插件信息
func (m *Manager) GetPlugin(name string) (*Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.catalog.plugins[name]
	return p, ok
}

// GetPluginConfig returns a detached config snapshot so built-in services can
// read hot-updated settings without racing Manager.UpdateConfig.
func (m *Manager) GetPluginConfig(name string) (map[string]interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.catalog.plugins[name]
	if !ok || p == nil || p.Manifest == nil {
		return nil, false
	}
	return copyConfigMap(p.Manifest.Config), true
}

func (m *Manager) IsPluginRunning(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.catalog.plugins[name]
	return ok && p.Status == StatusRunning
}

func (m *Manager) UpdateConfig(name string, config map[string]interface{}) (map[string]interface{}, error) {
	return m.configs.Update(name, config)
}

func (m *Manager) updateConfig(name string, config map[string]interface{}) (map[string]interface{}, error) {
	m.mu.Lock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}
	normalized, err := normalizePluginConfig(p.Manifest, config)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	preservePluginInternalConfig(name, normalized, config, p.Manifest.Config)
	if err := validatePluginSpecificConfig(name, normalized); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	repo := m.repo
	if repo != nil {
		if err := repo.UpdateConfig(context.Background(), name, normalized); err != nil {
			m.mu.Unlock()
			return nil, err
		}
	}
	p.Manifest.Config = normalized
	m.bumpUIRevisionLocked()
	m.mu.Unlock()

	m.logPlugin(context.Background(), &PluginLogRecord{
		PluginName: name,
		Level:      "info",
		Message:    "plugin config updated by admin",
	})
	return copyConfigMap(normalized), nil
}

func (m *Manager) markPluginError(name string, err error) {
	m.mu.Lock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.mu.Unlock()
		return
	}
	p.Status = StatusError
	p.BackendState = BackendError
	p.Health = HealthUnavailable
	p.ErrorMsg = err.Error()
	m.bumpUIRevisionLocked()
	m.mu.Unlock()
	m.persistPluginStatus(context.Background(), name, StatusError, err.Error())
}

func restoredRuntimeStatus(status string) PluginStatus {
	switch PluginStatus(status) {
	case StatusStopped:
		return StatusStopped
	case StatusError:
		return StatusError
	default:
		return StatusInstalled
	}
}

func desiredEnabledFromRecord(status string, manifest *Manifest) bool {
	switch PluginStatus(status) {
	case StatusStopped:
		return false
	case StatusInstalled:
		return manifest != nil && manifest.IsSystemLevel()
	default:
		return true
	}
}

func persistedPluginStatus(p *Plugin) PluginStatus {
	if p == nil {
		return StatusInstalled
	}
	if p.Manifest != nil && p.Manifest.IsSystemLevel() {
		if p.DesiredEnabled {
			return StatusRunning
		}
		return StatusStopped
	}
	if p.Status == StatusInstalled && p.DesiredEnabled {
		return StatusRunning
	}
	return p.Status
}

func (m *Manager) persistPluginStatus(ctx context.Context, name string, status PluginStatus, errorMsg string) {
	m.mu.RLock()
	repo := m.repo
	p := m.catalog.plugins[name]
	revision := m.uiRevision
	backendState, frontendState, healthState := "", "", ""
	if p != nil {
		backendState, frontendState, healthState = string(p.BackendState), string(p.FrontendState), string(p.Health)
	}
	m.mu.RUnlock()
	if repo == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := repo.UpdateStatus(ctx, name, string(status), errorMsg); err != nil {
		log.Printf("⚠️  插件状态持久化失败: %s -> %s (%v)", name, status, err)
	}
	if stateRepo, ok := repo.(PluginRuntimeStateRepository); ok && p != nil {
		if err := stateRepo.UpdateRuntimeState(ctx, name, backendState, frontendState, healthState, int64(revision)); err != nil {
			log.Printf("⚠️  插件运行时状态持久化失败: %s (%v)", name, err)
		}
	}
}

func (m *Manager) logPlugin(ctx context.Context, record *PluginLogRecord) {
	m.mu.RLock()
	logRepo := m.auditRepo
	m.mu.RUnlock()
	if logRepo == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if record.Metadata == nil {
		record.Metadata = map[string]interface{}{}
	}
	if err := logRepo.SaveLog(ctx, record); err != nil {
		log.Printf("⚠️  插件日志写入失败: %s (%v)", record.PluginName, err)
	}
}

func (m *Manager) ListPluginLogs(ctx context.Context, pluginName string, limit int) ([]*PluginLogRecord, error) {
	m.mu.RLock()
	logRepo := m.auditRepo
	m.mu.RUnlock()
	if logRepo == nil {
		return []*PluginLogRecord{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return logRepo.ListLogs(ctx, pluginName, limit)
}

func (m *Manager) RecordPluginAudit(ctx context.Context, pluginName, level, message string, metadata map[string]interface{}) {
	if strings.TrimSpace(pluginName) == "" {
		pluginName = "unknown"
	}
	if strings.TrimSpace(level) == "" {
		level = "info"
	}
	if strings.TrimSpace(message) == "" {
		message = "plugin audit"
	}
	m.logPlugin(ctx, &PluginLogRecord{
		PluginName: pluginName,
		Level:      level,
		Message:    message,
		EventType:  "plugin.package",
		Metadata:   metadata,
	})
}

func eventLogMetadata(event *EventMessage, err error) map[string]interface{} {
	metadata := map[string]interface{}{}
	if event != nil {
		metadata["source"] = event.Source
		metadata["subject"] = event.Subject
	}
	if err != nil {
		metadata["error"] = err.Error()
	}
	return metadata
}

// ListPlugins 列出所有插件
func (m *Manager) ListPlugins() []*Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	plugins := make([]*Plugin, 0, len(m.catalog.plugins))
	for _, p := range m.catalog.plugins {
		copyPlugin := *p
		if p.Manifest != nil {
			copyManifest := *p.Manifest
			copyPlugin.Manifest = &copyManifest
		}
		plugins = append(plugins, &copyPlugin)
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].ID < plugins[j].ID })
	return plugins
}

// Uninstall 卸载插件
func (m *Manager) Uninstall(name string) error {
	m.mu.Lock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	if p.Manifest != nil && p.Manifest.IsSystemLevel() {
		m.mu.Unlock()
		return fmt.Errorf("system-level plugin '%s' cannot be uninstalled at runtime", name)
	}

	// 停止运行中的插件
	if p.Status == StatusRunning {
		m.mu.Unlock()
		if err := m.Stop(name); err != nil {
			log.Printf("⚠️  停止插件 %s 失败: %v", name, err)
		}
		m.mu.Lock()
	}

	// 从注册表移除
	for eventType, names := range m.events.subscriptions {
		for i, n := range names {
			if n == name {
				m.events.subscriptions[eventType] = append(names[:i], names[i+1:]...)
				break
			}
		}
	}

	delete(m.catalog.plugins, name)
	m.bumpUIRevisionLocked()
	repo := m.repo
	m.mu.Unlock()

	if repo != nil {
		if err := repo.Delete(context.Background(), name); err != nil {
			return err
		}
	}

	log.Printf("🗑️  插件已卸载: %s", name)
	return nil
}

// StopAll 停止所有插件（服务关闭时调用）
func (m *Manager) StopAll() {
	m.mu.RLock()
	names := make([]string, 0, len(m.catalog.plugins))
	for name, p := range m.catalog.plugins {
		if p.Status == StatusRunning {
			names = append(names, name)
		}
	}
	m.mu.RUnlock()

	for _, name := range names {
		if err := m.stop(name, false); err != nil {
			log.Printf("⚠️  停止插件 %s 失败: %v", name, err)
		}
	}
}

// InstallFromPluginsDir 从插件目录批量安装
func (m *Manager) InstallFromPluginsDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("📁 插件目录不存在，跳过: %s", dir)
			return nil
		}
		return fmt.Errorf("read plugins dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginDir := filepath.Join(dir, entry.Name())
		manifestPath := filepath.Join(pluginDir, "plugin.yaml")
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			continue
		}
		if _, err := m.Install(pluginDir); err != nil {
			log.Printf("⚠️  安装插件 %s 失败: %v", entry.Name(), err)
		}
	}
	return nil
}

func (m *Manager) ImportPackage(packagePath, pluginsDir string, replace bool) (*Plugin, error) {
	precheck, err := PrecheckPluginPackage(packagePath, pluginsDir)
	if err != nil {
		return nil, err
	}
	if !precheck.Allowed || precheck.Manifest == nil {
		return nil, fmt.Errorf("plugin package precheck failed: %s", strings.Join(precheck.Errors, "; "))
	}
	manifest := precheck.Manifest
	if manifest.IsSystemLevel() {
		return nil, fmt.Errorf("system-level plugins must be deployed with server code and take effect after restart")
	}

	m.mu.RLock()
	existing, loaded := m.catalog.plugins[manifest.Name]
	m.mu.RUnlock()
	wasEnabled := false
	if loaded {
		if existing.Manifest != nil && existing.Manifest.IsSystemLevel() {
			return nil, fmt.Errorf("system-level plugin '%s' cannot be updated at runtime", manifest.Name)
		}
		if !replace {
			return nil, fmt.Errorf("plugin '%s' already installed; use replace to overwrite", manifest.Name)
		}
		wasEnabled = existing.DesiredEnabled
		if _, err := CreatePluginSnapshot(existing.Directory, snapshotDataDir(pluginsDir), "pre-update"); err != nil {
			return nil, fmt.Errorf("create pre-update snapshot: %w", err)
		}
		if err := m.Uninstall(manifest.Name); err != nil {
			return nil, err
		}
	}

	info, err := InstallPluginPackage(packagePath, pluginsDir, replace)
	if err != nil {
		return nil, err
	}
	installed, err := m.Install(info.PluginDir)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	installed.Checksum = info.Checksum
	installed.PackageSize = info.PackageSize
	m.mu.Unlock()
	if err := m.syncPluginRecord(context.Background(), installed); err != nil {
		return nil, err
	}
	if wasEnabled {
		if err := m.RequestEnable(installed.Manifest.Name); err != nil {
			return nil, err
		}
		if err := m.HealthCheck(installed.Manifest.Name); err != nil {
			m.markPluginError(installed.Manifest.Name, fmt.Errorf("post-install health check failed: %w", err))
			return nil, fmt.Errorf("post-install health check failed for %s: %w; use a version snapshot to roll back", installed.Manifest.Name, err)
		}
	}
	return installed, nil
}

func snapshotDataDir(pluginsDir string) string {
	if configured := os.Getenv("PLUGIN_DATA_DIR"); configured != "" {
		return configured
	}
	if filepath.Clean(pluginsDir) == filepath.Clean(DefaultPluginsDir) {
		return DefaultPluginDataDir
	}
	return filepath.Join(pluginsDir, ".plugin-data")
}

func (m *Manager) HealthCheck(name string) error {
	m.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok || p == nil || p.Manifest == nil {
		m.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	runtime, _ := m.runtimes.Get(p.Manifest.Runtime)
	status := p.Status
	m.mu.RUnlock()
	if status != StatusRunning {
		return fmt.Errorf("plugin '%s' is not running (status=%s)", name, status)
	}
	if runtime == nil {
		return fmt.Errorf("runtime '%s' is not registered", p.Manifest.Runtime)
	}
	err := runtime.HealthCheck(context.Background(), name)
	m.mu.Lock()
	changed := false
	currentStatus := status
	currentError := ""
	if current, ok := m.catalog.plugins[name]; ok {
		previous := current.Health
		if err != nil {
			current.Health = HealthUnavailable
		} else {
			current.Health = HealthHealthy
		}
		if current.Health != previous {
			m.bumpUIRevisionLocked()
			changed = true
		}
		currentStatus = current.Status
		currentError = current.ErrorMsg
	}
	m.mu.Unlock()
	if changed {
		m.persistPluginStatus(context.Background(), name, currentStatus, currentError)
	}
	return err
}

func (m *Manager) DispatchExtension(ctx context.Context, name string, request *ExtensionRequest) (*ExtensionResponse, error) {
	m.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok || p == nil || p.Manifest == nil {
		m.mu.RUnlock()
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}
	if p.BackendState != BackendRunning || p.Health == HealthUnavailable {
		m.mu.RUnlock()
		return nil, fmt.Errorf("plugin '%s' is unavailable", name)
	}
	runtime, _ := m.runtimes.Get(p.Manifest.Runtime)
	m.mu.RUnlock()
	dispatcher, ok := runtime.(ExtensionRuntime)
	if !ok {
		return nil, fmt.Errorf("runtime '%s' does not support extension dispatch", p.Manifest.Runtime)
	}
	return dispatcher.DispatchExtension(ctx, name, request)
}

func (m *Manager) ListVersionSnapshots(name string) ([]VersionSnapshot, error) {
	if _, ok := m.GetPlugin(name); !ok {
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}
	return ListPluginSnapshots(name, PluginDataDirFromEnv())
}

func (m *Manager) RollbackVersionSnapshot(name, snapshotID, pluginsDir string) (*Plugin, error) {
	current, ok := m.GetPlugin(name)
	if !ok || current.Manifest == nil {
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}
	if current.Manifest.IsSystemLevel() {
		return nil, fmt.Errorf("system-level plugin rollback requires source deployment and server restart")
	}
	snapshot, err := FindPluginSnapshot(name, snapshotID, PluginDataDirFromEnv())
	if err != nil {
		return nil, err
	}
	installed, err := m.ImportPackage(snapshot.PackagePath, pluginsDir, true)
	if err != nil {
		return nil, err
	}
	m.RecordPluginAudit(context.Background(), name, "warn", "plugin version snapshot restored", map[string]interface{}{
		"snapshot_id":      snapshot.ID,
		"restored_version": snapshot.Version,
		"checksum":         snapshot.Checksum,
	})
	return installed, nil
}

func (m *Manager) ExportPackage(name, outputPath string) (*PackageInfo, error) {
	m.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.mu.RUnlock()
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}
	pluginDir := p.Directory
	m.mu.RUnlock()

	if pluginDir == "" {
		return nil, fmt.Errorf("plugin '%s' has no plugin directory", name)
	}
	return PackagePlugin(pluginDir, outputPath)
}
