package moderation

// Settings owns configurable moderation actions while the authorization,
// category scope and audit core remain always-on.
type Settings interface{ Current() Config }

type LegacySettings struct{ read func() map[string]interface{} }

func NewLegacySettings(read func() map[string]interface{}) *LegacySettings {
	return &LegacySettings{read: read}
}
func (s *LegacySettings) Current() Config {
	if s == nil || s.read == nil {
		return ConfigFromPluginConfig(nil)
	}
	return ConfigFromPluginConfig(s.read())
}
