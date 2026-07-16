package plugin

import (
	"context"
	"log"
)

func (m *Manager) Enable(name string) error { return m.lifecycle.Enable(name) }
func (m *Manager) ReloadUserPlugin(name string) error {
	return m.lifecycle.ReloadUserPlugin(name)
}
func (m *Manager) LifecycleState(name string) (LifecycleState, bool) {
	return m.lifecycle.LifecycleState(name)
}
func (m *Manager) StartDesiredPlugins(scope string) {
	m.lifecycle.StartDesiredPlugins(scope)
}
func (m *Manager) DispatchBeforeEvent(ctx context.Context, event *EventMessage) *PluginResponse {
	return m.events.DispatchBeforeEvent(ctx, event)
}
func (m *Manager) DispatchEvent(ctx context.Context, event *EventMessage) {
	m.events.DispatchEvent(ctx, event)
}
func (m *Manager) SetPluginRepository(repo PluginRepository) { m.packages.SetRepository(repo) }
func (m *Manager) SetOperationTracker(tracker OperationTracker) {
	m.packages.SetOperationTracker(tracker)
}
func (m *Manager) SetCompatibilityReporter(reporter CompatibilityReporter) {
	m.packages.SetCompatibilityReporter(reporter)
}
func (m *Manager) RecordCompatibility(ctx context.Context, key, kind string, detail any) {
	if m == nil || m.packages == nil || m.packages.compatibility == nil {
		return
	}
	_ = m.packages.compatibility.RecordCompatibility(ctx, key, kind, detail)
}
func (m *Manager) SetPluginLogRepository(repo PluginLogRepository) {
	m.audit.SetRepository(repo)
}
func (m *Manager) RegisterRuntime(runtimeType string, runtime Runtime) {
	if err := m.runtimes.Register(runtimeType, runtime); err != nil {
		panic(err)
	}
	log.Printf("plugin runtime registered: %s", runtimeType)
}
func (m *Manager) Install(dir string) (*Plugin, error) { return m.packages.Install(dir) }
func (m *Manager) AuthorizeHostAPI(name, token string) (*Plugin, bool) {
	return m.host.Authorize(name, token)
}
func (m *Manager) Uninstall(name string) error { return m.packages.Uninstall(name) }
func (m *Manager) StopAll()                    { m.lifecycle.StopAll() }
func (m *Manager) InstallFromPluginsDir(dir string) error {
	return m.packages.InstallFromPluginsDir(dir)
}
func (m *Manager) ImportPackage(packagePath, pluginsDir string, replace bool) (*Plugin, error) {
	return m.packages.ImportPackage(packagePath, pluginsDir, replace)
}
func (m *Manager) ImportPackageContext(ctx context.Context, packagePath, pluginsDir string, replace bool) (*Plugin, error) {
	return m.packages.ImportPackageContext(ctx, packagePath, pluginsDir, replace)
}
func (m *Manager) HealthCheck(name string) error { return m.lifecycle.HealthCheck(name) }
func (m *Manager) DispatchExtension(ctx context.Context, name string, request *ExtensionRequest) (*ExtensionResponse, error) {
	return m.host.DispatchExtension(ctx, name, request)
}
func (m *Manager) ListVersionSnapshots(name string) ([]VersionSnapshot, error) {
	return m.snapshots.ListVersionSnapshots(name)
}
func (m *Manager) RollbackVersionSnapshot(name, snapshotID, pluginsDir string) (*Plugin, error) {
	return m.snapshots.RollbackVersionSnapshot(name, snapshotID, pluginsDir)
}
func (m *Manager) ExportPackage(name, outputPath string) (*PackageInfo, error) {
	return m.packages.ExportPackage(name, outputPath)
}

func (s *LifecycleService) persistPluginStatus(ctx context.Context, name string, status PluginStatus, errorMsg string) {
	s.catalog.mu.RLock()
	repo := s.catalog.repo
	p := s.catalog.plugins[name]
	revision := s.ui.Revision()
	backendState, frontendState, healthState := "", "", ""
	if p != nil {
		backendState, frontendState, healthState = string(p.BackendState), string(p.FrontendState), string(p.Health)
	}
	s.catalog.mu.RUnlock()
	if repo == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := repo.UpdateStatus(ctx, name, string(status), errorMsg); err != nil {
		log.Printf("plugin status persistence failed: %s -> %s (%v)", name, status, err)
	}
	if stateRepo, ok := repo.(PluginRuntimeStateRepository); ok && p != nil {
		if err := stateRepo.UpdateRuntimeState(ctx, name, backendState, frontendState, healthState, int64(revision)); err != nil {
			log.Printf("plugin runtime state persistence failed: %s (%v)", name, err)
		}
	}
}

func (s *LifecycleService) logPlugin(ctx context.Context, record *PluginLogRecord) {
	s.audit.Log(ctx, record)
}

func (s *ConfigService) Get(name string) (map[string]interface{}, bool) {
	s.catalog.mu.RLock()
	defer s.catalog.mu.RUnlock()
	p, ok := s.catalog.plugins[name]
	if !ok || p == nil || p.Manifest == nil {
		return nil, false
	}
	return copyConfigMap(p.Manifest.Config), true
}

func (s *ConfigService) Update(name string, value map[string]interface{}) (map[string]interface{}, error) {
	return s.updateConfig(name, value)
}

func (s *ConfigService) logPlugin(ctx context.Context, record *PluginLogRecord) {
	s.audit.Log(ctx, record)
}

func (s *EventRegistry) markPluginError(name string, err error) {
	s.lifecycle.markPluginError(name, err)
}

func (s *EventRegistry) logPlugin(ctx context.Context, record *PluginLogRecord) {
	s.audit.Log(ctx, record)
}

func (s *AuditLogService) Log(ctx context.Context, record *PluginLogRecord) {
	if record == nil {
		return
	}
	s.mu.RLock()
	repo := s.repo
	s.mu.RUnlock()
	if repo == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if record.Metadata == nil {
		record.Metadata = map[string]interface{}{}
	}
	if err := repo.SaveLog(ctx, record); err != nil {
		log.Printf("plugin audit log persistence failed: %s (%v)", record.PluginName, err)
	}
}
