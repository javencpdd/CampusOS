package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Manager 插件管理器
type Manager struct {
	mu       sync.RWMutex
	plugins  map[string]*Plugin  // name -> plugin
	runtimes map[string]Runtime  // runtime type -> runtime impl
	registry map[string][]string // event type -> plugin names
	repo     PluginRepository    // 可选：插件持久化仓储
	logRepo  PluginLogRepository // 可选：插件运行日志仓储
}

// NewManager 创建插件管理器
func NewManager() *Manager {
	return &Manager{
		plugins:  make(map[string]*Plugin),
		runtimes: make(map[string]Runtime),
		registry: make(map[string][]string),
	}
}

// SetPluginRepository 设置插件持久化仓储
func (m *Manager) SetPluginRepository(repo PluginRepository) {
	m.mu.Lock()
	m.repo = repo
	if logRepo, ok := repo.(PluginLogRepository); ok {
		m.logRepo = logRepo
	}
	plugins := make([]*Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
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
	m.logRepo = repo
}

// RegisterRuntime 注册运行时实现
func (m *Manager) RegisterRuntime(runtimeType string, runtime Runtime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtimes[runtimeType] = runtime
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

	if _, exists := m.plugins[manifest.Name]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("plugin '%s' already installed", manifest.Name)
	}

	plugin := &Plugin{
		ID:        manifest.Name,
		Manifest:  manifest,
		Status:    StatusInstalled,
		Directory: dir,
	}
	m.plugins[manifest.Name] = plugin

	// 注册事件订阅
	for _, eventType := range manifest.Events.Subscribe {
		m.registry[eventType] = append(m.registry[eventType], manifest.Name)
	}
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
	record.Status = string(p.Status)
	record.Config = string(configJSON)
	record.ErrorMsg = p.ErrorMsg
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

// Enable 启用插件
func (m *Manager) Enable(name string) error {
	m.mu.Lock()
	p, ok := m.plugins[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	if p.Status != StatusInstalled && p.Status != StatusStopped {
		m.mu.Unlock()
		return fmt.Errorf("plugin '%s' cannot be enabled (status: %s)", name, p.Status)
	}
	p.Status = StatusEnabled
	m.mu.Unlock()

	return m.Start(name)
}

// Start 启动插件
func (m *Manager) Start(name string) error {
	m.mu.RLock()
	p, ok := m.plugins[name]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	runtimeType := p.Manifest.Runtime
	m.mu.RUnlock()

	m.mu.Lock()
	runtime, ok := m.runtimes[runtimeType]
	if !ok {
		err := fmt.Errorf("runtime '%s' not registered", runtimeType)
		p.Status = StatusError
		p.ErrorMsg = err.Error()
		m.mu.Unlock()
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
	m.mu.Unlock()

	if err := runtime.Start(context.Background(), p); err != nil {
		m.mu.Lock()
		p.Status = StatusError
		p.ErrorMsg = err.Error()
		m.mu.Unlock()
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
	p.ErrorMsg = ""
	m.mu.Unlock()

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
	m.mu.RLock()
	p, ok := m.plugins[name]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("plugin '%s' not found", name)
	}
	runtimeType := p.Manifest.Runtime
	m.mu.RUnlock()

	m.mu.Lock()
	runtime, ok := m.runtimes[runtimeType]
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
	m.mu.Unlock()

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

// DispatchBeforeEvent 分发 .before 事件（同步，可被插件拦截）
func (m *Manager) DispatchBeforeEvent(ctx context.Context, event *EventMessage) *PluginResponse {
	beforeEvent := &EventMessage{
		Type:    event.Type + ".before",
		Source:  event.Source,
		Subject: event.Subject,
		Data:    event.Data,
	}

	m.mu.RLock()
	pluginNames := m.registry[event.Type]
	plugins := make([]*Plugin, 0, len(pluginNames))
	for _, name := range pluginNames {
		if p, ok := m.plugins[name]; ok && p.Status == StatusRunning {
			plugins = append(plugins, p)
		}
	}
	m.mu.RUnlock()

	for _, p := range plugins {
		m.mu.RLock()
		runtimeType := p.Manifest.Runtime
		m.mu.RUnlock()

		m.mu.Lock()
		runtime, ok := m.runtimes[runtimeType]
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
	pluginNames := m.registry[event.Type]
	plugins := make([]*Plugin, 0, len(pluginNames))
	for _, name := range pluginNames {
		if p, ok := m.plugins[name]; ok && p.Status == StatusRunning {
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
			runtime, ok := m.runtimes[runtimeType]
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
	p, ok := m.plugins[name]
	return p, ok
}

func (m *Manager) markPluginError(name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.plugins[name]
	if !ok {
		return
	}
	p.Status = StatusError
	p.ErrorMsg = err.Error()
}

func (m *Manager) logPlugin(ctx context.Context, record *PluginLogRecord) {
	m.mu.RLock()
	logRepo := m.logRepo
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
	logRepo := m.logRepo
	m.mu.RUnlock()
	if logRepo == nil {
		return []*PluginLogRecord{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return logRepo.ListLogs(ctx, pluginName, limit)
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
	plugins := make([]*Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		plugins = append(plugins, p)
	}
	return plugins
}

// Uninstall 卸载插件
func (m *Manager) Uninstall(name string) error {
	m.mu.Lock()
	p, ok := m.plugins[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("plugin '%s' not found", name)
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
	for eventType, names := range m.registry {
		for i, n := range names {
			if n == name {
				m.registry[eventType] = append(names[:i], names[i+1:]...)
				break
			}
		}
	}

	delete(m.plugins, name)
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
	names := make([]string, 0, len(m.plugins))
	for name, p := range m.plugins {
		if p.Status == StatusRunning {
			names = append(names, name)
		}
	}
	m.mu.RUnlock()

	for _, name := range names {
		if err := m.Stop(name); err != nil {
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
	manifest, err := InspectPluginPackage(packagePath)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	_, loaded := m.plugins[manifest.Name]
	m.mu.RUnlock()
	if loaded {
		if !replace {
			return nil, fmt.Errorf("plugin '%s' already installed; use replace to overwrite", manifest.Name)
		}
		if err := m.Uninstall(manifest.Name); err != nil {
			return nil, err
		}
	}

	info, err := InstallPluginPackage(packagePath, pluginsDir, replace)
	if err != nil {
		return nil, err
	}
	return m.Install(info.PluginDir)
}

func (m *Manager) ExportPackage(name, outputPath string) (*PackageInfo, error) {
	m.mu.RLock()
	p, ok := m.plugins[name]
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
