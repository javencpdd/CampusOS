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

func TestLoadAuthPasswordHashEnabledFromDotEnv(t *testing.T) {
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

	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("AUTH_PASSWORD_HASH_ENABLED=false\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Setenv("AUTH_PASSWORD_HASH_ENABLED", "")

	cfg := Load()
	if cfg.Auth.PasswordHashEnabled {
		t.Fatalf("expected password hashing to be disabled from .env")
	}
}

func TestLoadAuthPasswordHashEnabledDefaultsToTrue(t *testing.T) {
	t.Setenv("AUTH_PASSWORD_HASH_ENABLED", "")

	cfg := Load()
	if !cfg.Auth.PasswordHashEnabled {
		t.Fatalf("expected password hashing to be enabled by default")
	}
}

func TestLoadBootstrapConfiguration(t *testing.T) {
	t.Setenv("CAMPUSOS_ENV", "production")
	t.Setenv("AUTH_BOOTSTRAP_ADMIN_SECRET", "bootstrap-secret")
	t.Setenv("AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN", "false")

	cfg := Load()
	if cfg.Deployment.Environment != "production" {
		t.Fatalf("environment=%q want production", cfg.Deployment.Environment)
	}
	if cfg.Auth.BootstrapAdminSecret != "bootstrap-secret" {
		t.Fatalf("bootstrap secret was not loaded")
	}
	if cfg.Auth.AllowDevelopmentDefaultAdmin {
		t.Fatal("development compatibility administrator should be disabled")
	}
}

func TestLoadChallengeSecretRing(t *testing.T) {
	t.Setenv("AUTH_CHALLENGE_ACTIVE_KEY_ID", "rotation-v2")
	t.Setenv("AUTH_CHALLENGE_HMAC_KEYS", "retired-v1:old-secret,rotation-v2:new-secret")
	t.Setenv("AUTH_CHALLENGE_IP_HASH_SECRET", "ip-hash-secret")
	t.Setenv("AUTH_SESSION_IP_HASH_SECRET", "session-ip-hash-secret")
	t.Setenv("AUTH_REFRESH_BODY_COMPAT", "true")

	cfg := Load()
	if cfg.Auth.ChallengeActiveKeyID != "rotation-v2" {
		t.Fatalf("active challenge key=%q", cfg.Auth.ChallengeActiveKeyID)
	}
	if got := cfg.Auth.ChallengeHMACKeys["rotation-v2"]; got != "new-secret" {
		t.Fatalf("active challenge secret=%q", got)
	}
	if got := cfg.Auth.ChallengeHMACKeys["retired-v1"]; got != "old-secret" {
		t.Fatalf("retired challenge secret=%q", got)
	}
	if cfg.Auth.ChallengeIPHashSecret != "ip-hash-secret" {
		t.Fatal("IP hash secret was not loaded")
	}
	if cfg.Auth.SessionIPHashSecret != "session-ip-hash-secret" || !cfg.Auth.RefreshBodyCompat {
		t.Fatalf("session configuration was not loaded: %#v", cfg.Auth)
	}
}

func TestLoadEmailDeliveryConfiguration(t *testing.T) {
	t.Setenv("EMAIL_PROVIDER", "smtp")
	t.Setenv("EMAIL_SMTP_HOST", "smtp.example.test")
	t.Setenv("EMAIL_SMTP_PORT", "2525")
	t.Setenv("EMAIL_SMTP_USERNAME", "mailer")
	t.Setenv("EMAIL_SMTP_PASSWORD", "mail-secret")
	t.Setenv("EMAIL_SMTP_FROM", "CampusOS <noreply@example.test>")
	t.Setenv("EMAIL_SMTP_TIMEOUT", "12s")
	t.Setenv("EMAIL_SMTP_STARTTLS", "false")

	cfg := Load()
	if cfg.Email.Provider != "smtp" || cfg.Email.SMTPHost != "smtp.example.test" || cfg.Email.SMTPPort != 2525 ||
		cfg.Email.SMTPUsername != "mailer" || cfg.Email.SMTPPassword != "mail-secret" || cfg.Email.SMTPFrom != "CampusOS <noreply@example.test>" ||
		cfg.Email.SMTPTimeout != "12s" || cfg.Email.SMTPStartTLS {
		t.Fatalf("unexpected email configuration: %#v", cfg.Email)
	}
}

func TestLoadObservabilityDefaultsAndExplicitExporter(t *testing.T) {
	t.Setenv("OBSERVABILITY_PROMETHEUS_ENABLED", "")
	t.Setenv("OBSERVABILITY_PROMETHEUS_ADDR", "")
	t.Setenv("OBSERVABILITY_PROMETHEUS_PATH", "")
	defaults := Load().Observability
	if defaults.PrometheusEnabled || defaults.PrometheusAddr != "127.0.0.1:9091" || defaults.PrometheusPath != "/metrics" {
		t.Fatalf("unexpected observability defaults: %#v", defaults)
	}

	t.Setenv("OBSERVABILITY_PROMETHEUS_ENABLED", "true")
	t.Setenv("OBSERVABILITY_PROMETHEUS_ADDR", "127.0.0.1:19091")
	t.Setenv("OBSERVABILITY_PROMETHEUS_PATH", "/internal/metrics")
	explicit := Load().Observability
	if !explicit.PrometheusEnabled || explicit.PrometheusAddr != "127.0.0.1:19091" || explicit.PrometheusPath != "/internal/metrics" {
		t.Fatalf("unexpected explicit observability config: %#v", explicit)
	}
}
