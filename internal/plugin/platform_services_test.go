package plugin

import "testing"

func TestPlatformServicesOnlyClassifyExternalPlugins(t *testing.T) {
	m := NewManager()
	if m.Catalog() == nil || m.RuntimeRegistry() == nil || m.Lifecycle() == nil || m.Configs() == nil || m.UIRegistry() == nil || m.EventRegistry() == nil || m.AuditLogs() == nil {
		t.Fatal("manager facade did not initialize delegated services")
	}
	if got := m.Catalog().Classify(&Manifest{Name: "external", Runtime: "wasm"}); got != ExternalPlugin {
		t.Fatalf("class=%s", got)
	}
}
