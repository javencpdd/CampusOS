package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Manager 插件管理器
type Manager struct {
	runtimes  *RuntimeRegistry
	catalog   *PluginCatalog
	lifecycle *LifecycleService
	configs   *ConfigService
	ui        *UIRegistry
	events    *EventRegistry
	audit     *AuditLogService
	packages  *PackageService
	snapshots *SnapshotService
	host      *HostAccessService
}

// NewManager 创建插件管理器
func NewManager() *Manager {
	m := &Manager{
		runtimes: NewRuntimeRegistry(),
		catalog:  NewPluginCatalog(),
		ui:       NewUIRegistry(),
	}
	m.audit = NewAuditLogService()
	m.lifecycle = NewLifecycleService(m.catalog, m.runtimes, m.ui, m.audit)
	m.configs = NewConfigService(m.catalog, m.ui, m.audit)
	m.events = NewEventRegistry(m.catalog, m.runtimes, m.audit, m.lifecycle)
	m.packages = NewPackageService(m.catalog, m.lifecycle, m.ui, m.events, m.audit)
	m.snapshots = NewSnapshotService(m.catalog, m.packages, m.audit)
	m.host = NewHostAccessService(m.catalog, m.runtimes)
	return m
}

func (m *Manager) Catalog() *PluginCatalog           { return m.catalog }
func (m *Manager) RuntimeRegistry() *RuntimeRegistry { return m.runtimes }
func (m *Manager) Lifecycle() *LifecycleService      { return m.lifecycle }
func (m *Manager) Configs() *ConfigService           { return m.configs }
func (m *Manager) UIRegistry() *UIRegistry           { return m.ui }
func (m *Manager) EventRegistry() *EventRegistry     { return m.events }
func (m *Manager) AuditLogs() *AuditLogService       { return m.audit }
func (m *Manager) Packages() *PackageService         { return m.packages }
func (m *Manager) Snapshots() *SnapshotService       { return m.snapshots }
func (m *Manager) HostAccess() *HostAccessService    { return m.host }

// SetPluginRepository 设置插件持久化仓储
func (m *PackageService) SetRepository(repo PluginRepository) {
	m.catalog.mu.Lock()
	m.catalog.repo = repo
	if logRepo, ok := repo.(PluginLogRepository); ok {
		m.audit.SetRepository(logRepo)
	}
	plugins := make([]*Plugin, 0, len(m.catalog.plugins))
	for _, p := range m.catalog.plugins {
		plugins = append(plugins, p)
	}
	m.catalog.mu.Unlock()

	for _, p := range plugins {
		if err := m.syncPluginRecord(context.Background(), p); err != nil {
			log.Printf("⚠️  同步插件仓储失败: %s (%v)", p.ID, err)
		}
	}
}

// Install 从目录安装插件
func (m *PackageService) Install(dir string) (*Plugin, error) {
	manifestPath := filepath.Join(dir, "plugin.yaml")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("load manifest from %s: %w", dir, err)
	}
	if err := ValidateExternalPluginManifest(manifest); err != nil {
		return nil, fmt.Errorf("external plugin boundary: %w", err)
	}
	if manifest.Runtime == "grpc" && m.compatibility != nil {
		_ = m.compatibility.RecordCompatibility(context.Background(), "legacy-runtime-name:grpc", "external-plugin-runtime-alias", map[string]string{
			"plugin":  manifest.Name,
			"runtime": manifest.Runtime,
		})
	}

	m.catalog.mu.Lock()

	if _, exists := m.catalog.plugins[manifest.Name]; exists {
		m.catalog.mu.Unlock()
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
	m.events.Add(manifest.Name, manifest.Events.Subscribe)
	m.ui.Bump()
	m.catalog.mu.Unlock()

	if err := m.syncPluginRecord(context.Background(), plugin); err != nil {
		return nil, err
	}

	log.Printf("🔌 插件已安装: %s v%s (%s)", manifest.Name, manifest.Version, manifest.Runtime)
	return plugin, nil
}

func (m *PackageService) syncPluginRecord(ctx context.Context, p *Plugin) error {
	m.catalog.mu.RLock()
	repo := m.catalog.repo
	m.catalog.mu.RUnlock()
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
			m.catalog.mu.Lock()
			p.Manifest.Config = config
			m.catalog.mu.Unlock()
		}
		if record.Status != "" {
			m.catalog.mu.Lock()
			p.Status = restoredRuntimeStatus(record.Status)
			p.DesiredEnabled = desiredEnabledFromRecord(record.Status, p.Manifest)
			p.BackendState = restoredBackendState(record.BackendState, p.Status)
			p.FrontendState = restoredFrontendState(record.FrontendState, p)
			p.Health = restoredHealthState(record.HealthState, p.Status)
			p.ErrorMsg = record.ErrorMsg
			p.Checksum = record.Checksum
			p.PackageSize = record.PackageSize
			p.InstalledBy = record.InstalledBy
			m.catalog.mu.Unlock()
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
	record.UIRevision = int64(m.ui.Revision())
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
func (m *LifecycleService) enable(name string) error {
	m.catalog.mu.Lock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.catalog.mu.Unlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	if p.Status != StatusInstalled && p.Status != StatusStopped {
		m.catalog.mu.Unlock()
		return fmt.Errorf("plugin '%s' cannot be enabled (status: %s)", name, p.Status)
	}
	p.Status = StatusEnabled
	p.DesiredEnabled = true
	m.catalog.mu.Unlock()

	return m.start(name)
}

// RequestEnable applies a lifecycle request from the management API. System
// plugins only persist the target state, while user plugins start immediately.
func (m *Manager) RequestEnable(name string) error {
	return m.lifecycle.RequestEnable(name)
}

func (m *LifecycleService) requestEnable(name string) error {
	m.catalog.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.catalog.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	activationMode := p.Manifest.BackendActivationMode()
	status := p.Status
	m.catalog.mu.RUnlock()

	if activationMode == ActivationRestart {
		if p.DesiredEnabled && status == StatusRunning {
			return nil
		}
		m.catalog.mu.Lock()
		p.DesiredEnabled = true
		p.BackendState = BackendPendingRestart
		if !p.Manifest.UI.Empty() {
			p.FrontendState = FrontendLoaded
		}
		m.ui.Bump()
		m.catalog.mu.Unlock()
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
		m.catalog.mu.Lock()
		p.Status = StatusInstalled
		p.ErrorMsg = ""
		m.catalog.mu.Unlock()
	}
	return m.enable(name)
}

// RequestDisable applies a lifecycle request from the management API. System
// plugins remain in their current process state until the next server restart.
func (m *Manager) RequestDisable(name string) error {
	return m.lifecycle.RequestDisable(name)
}

func (m *LifecycleService) requestDisable(name string) error {
	m.catalog.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.catalog.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	activationMode := p.Manifest.BackendActivationMode()
	status := p.Status
	desiredEnabled := p.DesiredEnabled
	m.catalog.mu.RUnlock()

	if activationMode != ActivationRestart {
		if !desiredEnabled && (status == StatusStopped || status == StatusInstalled) {
			return nil
		}
		return m.stop(name, true)
	}
	if !desiredEnabled && status != StatusRunning {
		return nil
	}
	m.catalog.mu.Lock()
	p.DesiredEnabled = false
	p.BackendState = BackendPendingRestart
	p.FrontendState = FrontendUnloaded
	m.ui.Bump()
	m.catalog.mu.Unlock()
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
func (m *LifecycleService) ReloadUserPlugin(name string) error {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	m.catalog.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.catalog.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	if p.Manifest.BackendActivationMode() == ActivationRestart {
		m.catalog.mu.RUnlock()
		return fmt.Errorf("plugin '%s' requires a server restart", name)
	}
	status := p.Status
	m.catalog.mu.RUnlock()

	if status == StatusRunning {
		if err := m.stop(name, false); err != nil {
			return err
		}
	}
	return m.requestEnable(name)
}

// LifecycleState returns the current and requested lifecycle behavior for UI
// consumers without exposing mutable Plugin internals.
func (m *LifecycleService) LifecycleState(name string) (LifecycleState, bool) {
	m.catalog.mu.RLock()
	defer m.catalog.mu.RUnlock()
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
	return m.ui.Revision()
}

func (m *Manager) SubscribeUI() (<-chan uint64, func()) {
	return m.ui.Subscribe()
}

// StartDesiredPlugins starts plugins that were explicitly enabled before the
// current process started. Call this only during server bootstrap.
func (m *LifecycleService) StartDesiredPlugins(scope string) {
	m.catalog.mu.RLock()
	names := make([]string, 0, len(m.catalog.plugins))
	for name, p := range m.catalog.plugins {
		if p.Manifest == nil || p.Manifest.Scope != scope || !p.DesiredEnabled || p.Status == StatusRunning {
			continue
		}
		names = append(names, name)
	}
	m.catalog.mu.RUnlock()

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

func (m *LifecycleService) start(name string) error {
	m.catalog.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.catalog.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	runtimeType := p.Manifest.Runtime
	m.catalog.mu.RUnlock()

	m.catalog.mu.Lock()
	p.BackendState = BackendStarting
	p.Health = HealthUnknown
	if p.DesiredEnabled && !p.Manifest.UI.Empty() {
		p.FrontendState = FrontendLoaded
	}
	m.ui.Bump()
	runtime, ok := m.runtimes.Get(runtimeType)
	if !ok {
		err := fmt.Errorf("runtime '%s' not registered", runtimeType)
		p.Status = StatusError
		p.BackendState = BackendError
		p.Health = HealthUnavailable
		p.ErrorMsg = err.Error()
		m.catalog.mu.Unlock()
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
	m.catalog.mu.Unlock()

	if err := runtime.Start(context.Background(), p); err != nil {
		m.catalog.mu.Lock()
		p.Status = StatusError
		p.BackendState = BackendError
		p.Health = HealthUnavailable
		p.ErrorMsg = err.Error()
		p.HostToken = ""
		p.HostTokenExpiresAt = time.Time{}
		m.ui.Bump()
		m.catalog.mu.Unlock()
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

	m.catalog.mu.Lock()
	p.Status = StatusRunning
	p.BackendState = BackendRunning
	p.Health = HealthHealthy
	if !p.Manifest.UI.Empty() {
		p.FrontendState = FrontendLoaded
	}
	p.DesiredEnabled = true
	p.ErrorMsg = ""
	m.ui.Bump()
	m.catalog.mu.Unlock()
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

func (m *LifecycleService) stop(name string, persist bool) error {
	m.catalog.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.catalog.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	runtimeType := p.Manifest.Runtime
	m.catalog.mu.RUnlock()

	m.catalog.mu.Lock()
	p.BackendState = BackendStopping
	p.Health = HealthDegraded
	m.ui.Bump()
	runtime, ok := m.runtimes.Get(runtimeType)
	if !ok {
		m.catalog.mu.Unlock()
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
	m.catalog.mu.Unlock()

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

	m.catalog.mu.Lock()
	p.Status = StatusStopped
	p.BackendState = BackendStopped
	p.Health = HealthUnavailable
	p.HostToken = ""
	p.HostTokenExpiresAt = time.Time{}
	if persist {
		p.DesiredEnabled = false
		p.FrontendState = FrontendUnloaded
	}
	m.ui.Bump()
	m.catalog.mu.Unlock()
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
func (m *HostAccessService) Authorize(name, token string) (*Plugin, bool) {
	m.catalog.mu.RLock()
	defer m.catalog.mu.RUnlock()
	p, ok := m.catalog.plugins[name]
	if !ok || p == nil || p.Manifest == nil || p.Status != StatusRunning {
		return nil, false
	}
	if token == "" || p.HostToken == "" || token != p.HostToken || time.Now().After(p.HostTokenExpiresAt) {
		return nil, false
	}
	return clonePlugin(p), true
}

// DispatchBeforeEvent 分发 .before 事件（同步，可被插件拦截）
func (m *EventRegistry) DispatchBeforeEvent(ctx context.Context, event *EventMessage) *PluginResponse {
	beforeEvent := &EventMessage{
		Type:    event.Type + ".before",
		Source:  event.Source,
		Subject: event.Subject,
		Data:    event.Data,
	}

	pluginNames := m.Subscribers(event.Type)
	m.catalog.mu.RLock()
	plugins := make([]*Plugin, 0, len(pluginNames))
	for _, name := range pluginNames {
		if p, ok := m.catalog.plugins[name]; ok && p.Status == StatusRunning {
			plugins = append(plugins, clonePlugin(p))
		}
	}
	m.catalog.mu.RUnlock()

	for _, p := range plugins {
		runtimeType := p.Manifest.Runtime
		runtime, ok := m.runtimes.Get(runtimeType)
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
func (m *EventRegistry) DispatchEvent(ctx context.Context, event *EventMessage) {
	pluginNames := m.Subscribers(event.Type)
	m.catalog.mu.RLock()
	plugins := make([]*Plugin, 0, len(pluginNames))
	for _, name := range pluginNames {
		if p, ok := m.catalog.plugins[name]; ok && p.Status == StatusRunning {
			plugins = append(plugins, clonePlugin(p))
		}
	}
	m.catalog.mu.RUnlock()

	for _, p := range plugins {
		go func(pl *Plugin) {
			runtimeType := pl.Manifest.Runtime
			runtime, ok := m.runtimes.Get(runtimeType)
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
	return m.catalog.Get(name)
}

// GetPluginConfig returns a detached config snapshot so built-in services can
// read hot-updated settings without racing Manager.UpdateConfig.
func (m *Manager) GetPluginConfig(name string) (map[string]interface{}, bool) {
	return m.configs.Get(name)
}

func (m *Manager) IsPluginRunning(name string) bool {
	return m.catalog.IsRunning(name)
}

func (m *Manager) UpdateConfig(name string, config map[string]interface{}) (map[string]interface{}, error) {
	return m.configs.Update(name, config)
}

func (m *ConfigService) updateConfig(name string, config map[string]interface{}) (map[string]interface{}, error) {
	m.catalog.mu.Lock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.catalog.mu.Unlock()
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}
	normalized, err := normalizePluginConfig(p.Manifest, config)
	if err != nil {
		m.catalog.mu.Unlock()
		return nil, err
	}
	repo := m.catalog.repo
	if repo != nil {
		if err := repo.UpdateConfig(context.Background(), name, normalized); err != nil {
			m.catalog.mu.Unlock()
			return nil, err
		}
	}
	p.Manifest.Config = normalized
	m.ui.Bump()
	m.catalog.mu.Unlock()

	m.logPlugin(context.Background(), &PluginLogRecord{
		PluginName: name,
		Level:      "info",
		Message:    "plugin config updated by admin",
	})
	return copyConfigMap(normalized), nil
}

func (m *LifecycleService) markPluginError(name string, err error) {
	m.catalog.mu.Lock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.catalog.mu.Unlock()
		return
	}
	p.Status = StatusError
	p.BackendState = BackendError
	p.Health = HealthUnavailable
	p.ErrorMsg = err.Error()
	m.ui.Bump()
	m.catalog.mu.Unlock()
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

func (m *Manager) ListPluginLogs(ctx context.Context, pluginName string, limit int) ([]*PluginLogRecord, error) {
	return m.audit.List(ctx, pluginName, limit)
}

func (m *Manager) RecordPluginAudit(ctx context.Context, pluginName, level, message string, metadata map[string]interface{}) {
	m.audit.Record(ctx, pluginName, level, message, metadata)
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
	return m.catalog.List()
}

// Uninstall 卸载插件
func (m *PackageService) Uninstall(name string) error {
	ctx := context.Background()
	if m.tracker != nil {
		return m.tracker.Track(ctx, OperationRequest{
			Kind: "plugin.package.uninstall", SubjectType: "plugin", SubjectID: name,
		}, func(operationCtx context.Context) error {
			return m.uninstall(operationCtx, name)
		})
	}
	return m.uninstall(ctx, name)
}

func (m *PackageService) uninstall(ctx context.Context, name string) error {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	m.catalog.mu.Lock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.catalog.mu.Unlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	if p.Manifest != nil && p.Manifest.IsSystemLevel() {
		m.catalog.mu.Unlock()
		return fmt.Errorf("system-level plugin '%s' cannot be uninstalled at runtime", name)
	}

	// 停止运行中的插件
	if p.Status == StatusRunning {
		m.catalog.mu.Unlock()
		if err := m.lifecycle.Stop(name); err != nil {
			log.Printf("⚠️  停止插件 %s 失败: %v", name, err)
		}
		m.catalog.mu.Lock()
	}

	// 从注册表移除
	m.events.Remove(name)

	delete(m.catalog.plugins, name)
	m.ui.Bump()
	repo := m.catalog.repo
	m.catalog.mu.Unlock()

	if repo != nil {
		if err := repo.Delete(ctx, name); err != nil {
			return err
		}
	}

	log.Printf("🗑️  插件已卸载: %s", name)
	return nil
}

// StopAll 停止所有插件（服务关闭时调用）
func (m *LifecycleService) StopAll() {
	m.catalog.mu.RLock()
	names := make([]string, 0, len(m.catalog.plugins))
	for name, p := range m.catalog.plugins {
		if p.Status == StatusRunning {
			names = append(names, name)
		}
	}
	m.catalog.mu.RUnlock()

	for _, name := range names {
		if err := m.stop(name, false); err != nil {
			log.Printf("⚠️  停止插件 %s 失败: %v", name, err)
		}
	}
}

// InstallFromPluginsDir 从插件目录批量安装
func (m *PackageService) InstallFromPluginsDir(dir string) error {
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

func (m *PackageService) ImportPackage(packagePath, pluginsDir string, replace bool) (*Plugin, error) {
	return m.ImportPackageContext(context.Background(), packagePath, pluginsDir, replace)
}

func (m *PackageService) ImportPackageContext(ctx context.Context, packagePath, pluginsDir string, replace bool) (*Plugin, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m.tracker == nil {
		return m.importPackage(ctx, packagePath, pluginsDir, replace)
	}
	var installed *Plugin
	err := m.tracker.Track(ctx, OperationRequest{
		Kind: "plugin.package.import", SubjectType: "plugin_package", SubjectID: filepath.Base(packagePath),
	}, func(operationCtx context.Context) error {
		var installErr error
		installed, installErr = m.importPackage(operationCtx, packagePath, pluginsDir, replace)
		return installErr
	})
	return installed, err
}

func (m *PackageService) importPackage(ctx context.Context, packagePath, pluginsDir string, replace bool) (*Plugin, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
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

	existing, loaded := m.catalog.Get(manifest.Name)
	wasEnabled := false
	var snapshot *VersionSnapshot
	snapshotDir := snapshotDataDir(pluginsDir)
	if loaded {
		if existing.Manifest != nil && existing.Manifest.IsSystemLevel() {
			return nil, fmt.Errorf("system-level plugin '%s' cannot be updated at runtime", manifest.Name)
		}
		if !replace {
			return nil, fmt.Errorf("plugin '%s' already installed; use replace to overwrite", manifest.Name)
		}
		wasEnabled = existing.DesiredEnabled || existing.Status == StatusRunning
		snapshot, err = CreatePluginSnapshot(existing.Directory, snapshotDir, "pre-update")
		if err != nil {
			return nil, fmt.Errorf("create pre-update snapshot: %w", err)
		}
		snapshot.PackagePath = filepath.Join(snapshotDir, manifest.Name, "version-snapshots", snapshot.ID, snapshot.PackagePath)
		if _, err := m.detachForReplacement(manifest.Name); err != nil {
			return nil, err
		}
	}

	info, err := InstallPluginPackage(packagePath, pluginsDir, replace)
	if err != nil {
		if loaded {
			return nil, m.restoreFailedReplacement(ctx, manifest.Name, snapshot, pluginsDir, wasEnabled, err)
		}
		return nil, err
	}
	installed, err := m.Install(info.PluginDir)
	if err != nil {
		if loaded {
			return nil, m.restoreFailedReplacement(ctx, manifest.Name, snapshot, pluginsDir, wasEnabled, err)
		}
		_ = os.RemoveAll(info.PluginDir)
		return nil, err
	}
	m.catalog.mu.Lock()
	installed.Checksum = info.Checksum
	installed.PackageSize = info.PackageSize
	m.catalog.mu.Unlock()
	if err := m.syncPluginRecord(ctx, installed); err != nil {
		if loaded {
			return nil, m.restoreFailedReplacement(ctx, manifest.Name, snapshot, pluginsDir, wasEnabled, err)
		}
		_, _ = m.detachForReplacement(installed.Manifest.Name)
		_ = os.RemoveAll(info.PluginDir)
		return nil, err
	}
	if wasEnabled {
		if err := m.lifecycle.RequestEnable(installed.Manifest.Name); err != nil {
			return nil, m.restoreFailedReplacement(ctx, manifest.Name, snapshot, pluginsDir, wasEnabled, err)
		}
		if err := m.lifecycle.HealthCheck(installed.Manifest.Name); err != nil {
			m.lifecycle.markPluginError(installed.Manifest.Name, fmt.Errorf("post-install health check failed: %w", err))
			return nil, m.restoreFailedReplacement(ctx, manifest.Name, snapshot, pluginsDir, wasEnabled, fmt.Errorf("post-install health check failed: %w", err))
		}
	}
	return installed, nil
}

// detachForReplacement removes only the in-process catalog entry. It leaves
// the persistent record intact until a replacement has been installed and
// verified, so an interrupted upgrade has an explicit restore path.
func (m *PackageService) detachForReplacement(name string) (*Plugin, error) {
	current, ok := m.catalog.Get(name)
	if !ok || current == nil {
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}
	if current.Manifest != nil && current.Manifest.IsSystemLevel() {
		return nil, fmt.Errorf("system-level plugin '%s' cannot be updated at runtime", name)
	}
	if current.Status == StatusRunning {
		if err := m.lifecycle.Stop(name); err != nil {
			return nil, fmt.Errorf("stop plugin before replacement: %w", err)
		}
	}
	m.catalog.mu.Lock()
	defer m.catalog.mu.Unlock()
	current, ok = m.catalog.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin '%s' disappeared during replacement", name)
	}
	m.events.Remove(name)
	delete(m.catalog.plugins, name)
	m.ui.Bump()
	return clonePlugin(current), nil
}

func (m *PackageService) restoreFailedReplacement(ctx context.Context, name string, snapshot *VersionSnapshot, pluginsDir string, wasEnabled bool, cause error) error {
	if snapshot == nil || snapshot.PackagePath == "" {
		return cause
	}
	// Any partially registered replacement is detached before the archived
	// version is restored over the plugin directory. The restore package was
	// created before the original runtime was stopped.
	if _, ok := m.catalog.Get(name); ok {
		_, _ = m.detachForReplacement(name)
	}
	info, restoreErr := InstallPluginPackage(snapshot.PackagePath, pluginsDir, true)
	if restoreErr == nil {
		var restored *Plugin
		restored, restoreErr = m.Install(info.PluginDir)
		if restoreErr == nil {
			m.catalog.mu.Lock()
			restored.Checksum = info.Checksum
			restored.PackageSize = info.PackageSize
			m.catalog.mu.Unlock()
			restoreErr = m.syncPluginRecord(ctx, restored)
			if restoreErr == nil && wasEnabled {
				restoreErr = m.lifecycle.RequestEnable(restored.Manifest.Name)
			}
		}
	}
	if restoreErr != nil {
		return fmt.Errorf("plugin replacement failed: %w; automatic restore from snapshot %s also failed: %v", cause, snapshot.ID, restoreErr)
	}
	m.audit.Record(ctx, name, "warn", "plugin replacement automatically restored the prior snapshot", map[string]interface{}{
		"snapshot_id": snapshot.ID,
		"cause":       cause.Error(),
	})
	return fmt.Errorf("plugin replacement failed and the prior version was restored: %w", cause)
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

func (m *LifecycleService) HealthCheck(name string) error {
	m.catalog.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok || p == nil || p.Manifest == nil {
		m.catalog.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	runtime, _ := m.runtimes.Get(p.Manifest.Runtime)
	status := p.Status
	m.catalog.mu.RUnlock()
	if status != StatusRunning {
		return fmt.Errorf("plugin '%s' is not running (status=%s)", name, status)
	}
	if runtime == nil {
		return fmt.Errorf("runtime '%s' is not registered", p.Manifest.Runtime)
	}
	err := runtime.HealthCheck(context.Background(), name)
	m.catalog.mu.Lock()
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
			m.ui.Bump()
			changed = true
		}
		currentStatus = current.Status
		currentError = current.ErrorMsg
	}
	m.catalog.mu.Unlock()
	if changed {
		m.persistPluginStatus(context.Background(), name, currentStatus, currentError)
	}
	return err
}

func (m *HostAccessService) DispatchExtension(ctx context.Context, name string, request *ExtensionRequest) (*ExtensionResponse, error) {
	m.catalog.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok || p == nil || p.Manifest == nil {
		m.catalog.mu.RUnlock()
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}
	if p.BackendState != BackendRunning || p.Health == HealthUnavailable {
		m.catalog.mu.RUnlock()
		return nil, fmt.Errorf("plugin '%s' is unavailable", name)
	}
	runtime, _ := m.runtimes.Get(p.Manifest.Runtime)
	m.catalog.mu.RUnlock()
	dispatcher, ok := runtime.(ExtensionRuntime)
	if !ok {
		return nil, fmt.Errorf("runtime '%s' does not support extension dispatch", p.Manifest.Runtime)
	}
	return dispatcher.DispatchExtension(ctx, name, request)
}

func (m *SnapshotService) ListVersionSnapshots(name string) ([]VersionSnapshot, error) {
	if _, ok := m.catalog.Get(name); !ok {
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}
	return ListPluginSnapshots(name, PluginDataDirFromEnv())
}

func (m *SnapshotService) RollbackVersionSnapshot(name, snapshotID, pluginsDir string) (*Plugin, error) {
	current, ok := m.catalog.Get(name)
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
	installed, err := m.packages.ImportPackage(snapshot.PackagePath, pluginsDir, true)
	if err != nil {
		return nil, err
	}
	m.audit.Record(context.Background(), name, "warn", "plugin version snapshot restored", map[string]interface{}{
		"snapshot_id":      snapshot.ID,
		"restored_version": snapshot.Version,
		"checksum":         snapshot.Checksum,
	})
	return installed, nil
}

func (m *PackageService) ExportPackage(name, outputPath string) (*PackageInfo, error) {
	m.catalog.mu.RLock()
	p, ok := m.catalog.plugins[name]
	if !ok {
		m.catalog.mu.RUnlock()
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}
	pluginDir := p.Directory
	m.catalog.mu.RUnlock()

	if pluginDir == "" {
		return nil, fmt.Errorf("plugin '%s' has no plugin directory", name)
	}
	return PackagePlugin(pluginDir, outputPath)
}
