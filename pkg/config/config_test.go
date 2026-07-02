package config

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestLoadReadsDotEnvFile(t *testing.T) {
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	content := []byte(`
# local development database
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5433/campusos?sslmode=disable
SERVER_PORT="18080"
AI_MAX_CONCURRENT=7
`)
	if err := os.WriteFile(filepath.Join(tmp, ".env"), content, 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("DATABASE_DSN", "")
	t.Setenv("SERVER_PORT", "")
	t.Setenv("AI_MAX_CONCURRENT", "")

	cfg := Load()
	if cfg.Database.DSN != "postgres://campusos:campusos_dev@localhost:5433/campusos?sslmode=disable" {
		t.Fatalf("expected DATABASE_DSN from .env, got %q", cfg.Database.DSN)
	}
	if cfg.Server.Port != "18080" {
		t.Fatalf("expected SERVER_PORT from .env, got %q", cfg.Server.Port)
	}
	if cfg.AI.MaxConcurrent != 7 {
		t.Fatalf("expected AI_MAX_CONCURRENT from .env, got %d", cfg.AI.MaxConcurrent)
	}
}

func TestLoadEnvironmentOverridesDotEnvFile(t *testing.T) {
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("DATABASE_DSN=postgres://file-value\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Setenv("DATABASE_DSN", "postgres://env-value")

	cfg := Load()
	if cfg.Database.DSN != "postgres://env-value" {
		t.Fatalf("expected environment DATABASE_DSN to override .env, got %q", cfg.Database.DSN)
	}
}
