package moderation

// Settings owns configurable moderation actions while the authorization,
// category scope and audit core remain always-on.
type Settings interface{ Current() Config }

type FeatureSettings struct{ read func() map[string]interface{} }

func NewFeatureSettings(read func() map[string]interface{}) *FeatureSettings {
	return &FeatureSettings{read: read}
}
func (s *FeatureSettings) Current() Config {
	if s == nil || s.read == nil {
		return ConfigFromPluginConfig(nil)
	}
	return ConfigFromPluginConfig(s.read())
}
