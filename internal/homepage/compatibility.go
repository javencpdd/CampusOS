package homepage

import "github.com/campusos/CampusOS/internal/plugin"

const pluginName = "homepage-customizer"

// PluginLookup and PluginConfigUpdater preserve the v0.7 constructor shape
// for callers that have not yet moved to Appearance's ConfigSource.
type PluginLookup func(name string) (*plugin.Plugin, bool)
type PluginConfigUpdater func(name string, config map[string]interface{}) (map[string]interface{}, error)

type legacyPluginConfigSource struct {
	lookup PluginLookup
	update PluginConfigUpdater
}

func configSourceFrom(source interface{}) ConfigSource {
	switch value := source.(type) {
	case ConfigSource:
		return value
	case PluginLookup:
		return &legacyPluginConfigSource{lookup: value}
	case func(string) (*plugin.Plugin, bool):
		return &legacyPluginConfigSource{lookup: PluginLookup(value)}
	default:
		return nil
	}
}

func (s *Service) SetConfigUpdater(update PluginConfigUpdater) {
	if legacy, ok := s.config.(*legacyPluginConfigSource); ok {
		legacy.update = update
	}
}

func (s *legacyPluginConfigSource) plugin() (*plugin.Plugin, bool) {
	if s == nil || s.lookup == nil {
		return nil, false
	}
	p, ok := s.lookup(pluginName)
	return p, ok && p != nil && p.Manifest != nil
}

func (s *legacyPluginConfigSource) Enabled() bool {
	p, ok := s.plugin()
	return ok && p.Status == plugin.StatusRunning
}

func (s *legacyPluginConfigSource) Config() map[string]interface{} {
	p, ok := s.plugin()
	if !ok {
		return map[string]interface{}{}
	}
	return copyConfig(p.Manifest.Config)
}

func (s *legacyPluginConfigSource) Update(config map[string]interface{}) (map[string]interface{}, error) {
	if s == nil || s.update == nil {
		return nil, ErrStylePackInvalid
	}
	return s.update(pluginName, config)
}
