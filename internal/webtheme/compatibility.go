package webtheme

import "github.com/campusos/CampusOS/internal/plugin"

// PluginLookup is retained only for callers on the legacy plugin-config
// boundary. New code supplies ConfigSource directly.
type PluginLookup func(name string) (*plugin.Plugin, bool)

type legacyPluginConfigSource struct{ lookup PluginLookup }

func configSourceFrom(source interface{}) ConfigSource {
	switch value := source.(type) {
	case ConfigSource:
		return value
	case PluginLookup:
		return legacyPluginConfigSource{lookup: value}
	case func(string) (*plugin.Plugin, bool):
		return legacyPluginConfigSource{lookup: PluginLookup(value)}
	default:
		return nil
	}
}

func (s legacyPluginConfigSource) plugin() (*plugin.Plugin, bool) {
	if s.lookup == nil {
		return nil, false
	}
	p, ok := s.lookup(PluginName)
	return p, ok && p != nil && p.Manifest != nil
}

func (s legacyPluginConfigSource) Enabled() bool {
	p, ok := s.plugin()
	return ok && p.Status == plugin.StatusRunning
}

func (s legacyPluginConfigSource) Config() map[string]interface{} {
	p, ok := s.plugin()
	if !ok {
		return map[string]interface{}{}
	}
	return copyPluginConfig(p.Manifest.Config)
}

func copyPluginConfig(config map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{}, len(config))
	for key, value := range config {
		copy[key] = value
	}
	return copy
}
