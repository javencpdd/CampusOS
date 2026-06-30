package config

import "testing"

func TestLoadAIConfig(t *testing.T) {
	t.Setenv("AI_ENABLED", "true")
	t.Setenv("AI_PROVIDER", "openai-compatible")
	t.Setenv("AI_BASE_URL", "https://ai.example.test/v1")
	t.Setenv("AI_MODEL", "campus-model")
	t.Setenv("AI_API_KEY", "test-secret")
	t.Setenv("AI_TIMEOUT", "45s")
	t.Setenv("AI_MAX_REQUESTS_PER_MINUTE", "12")
	t.Setenv("AI_MAX_CONCURRENT", "3")

	cfg := Load()
	if !cfg.AI.Enabled {
		t.Fatalf("expected AI to be enabled")
	}
	if cfg.AI.Provider != "openai-compatible" ||
		cfg.AI.BaseURL != "https://ai.example.test/v1" ||
		cfg.AI.Model != "campus-model" ||
		cfg.AI.APIKey != "test-secret" ||
		cfg.AI.Timeout != "45s" ||
		cfg.AI.MaxRequestsPerMinute != 12 ||
		cfg.AI.MaxConcurrent != 3 {
		t.Fatalf("unexpected AI config: %#v", cfg.AI)
	}
}

func TestLoadAIConfigFallsBackOnInvalidIntegers(t *testing.T) {
	t.Setenv("AI_MAX_REQUESTS_PER_MINUTE", "invalid")
	t.Setenv("AI_MAX_CONCURRENT", "invalid")

	cfg := Load()
	if cfg.AI.MaxRequestsPerMinute != 60 || cfg.AI.MaxConcurrent != 4 {
		t.Fatalf("expected fallback values, got %#v", cfg.AI)
	}
}
