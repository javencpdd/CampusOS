package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/config"
)

// validateDeployment refuses a configuration that would silently turn local
// user files or legacy SQLite KV into multi-writer storage. A shared provider
// and coordinated runtime are intentionally future work, not a v0.9 promise.
func validateDeployment(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("server config is required")
	}
	environment := strings.ToLower(strings.TrimSpace(cfg.Deployment.Environment))
	if environment == "" {
		environment = "development"
	}
	switch environment {
	case "development", "test", "staging", "production":
	default:
		return fmt.Errorf("CAMPUSOS_ENV must be development, test, staging, or production, got %q", environment)
	}
	if !cfg.Auth.PasswordHashEnabled && environment != "development" {
		return fmt.Errorf("AUTH_PASSWORD_HASH_ENABLED=false is allowed only when CAMPUSOS_ENV=development")
	}
	if cfg.Auth.AllowDevelopmentDefaultAdmin && environment != "development" {
		return fmt.Errorf("AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN is allowed only when CAMPUSOS_ENV=development")
	}
	if environment == "production" {
		if len(strings.TrimSpace(cfg.Auth.BootstrapAdminSecret)) < 16 {
			return fmt.Errorf("AUTH_BOOTSTRAP_ADMIN_SECRET with at least 16 characters is required when CAMPUSOS_ENV=production")
		}
		if strings.TrimSpace(cfg.JWT.Secret) == "" || cfg.JWT.Secret == "campusos-dev-secret-key-change-in-production" {
			return fmt.Errorf("JWT_SECRET must be replaced before CAMPUSOS_ENV=production can start")
		}
		activeChallengeKey := strings.TrimSpace(cfg.Auth.ChallengeActiveKeyID)
		challengeSecret := ""
		if cfg.Auth.ChallengeHMACKeys != nil {
			challengeSecret = strings.TrimSpace(cfg.Auth.ChallengeHMACKeys[activeChallengeKey])
		}
		if activeChallengeKey == "" || len(challengeSecret) < 16 || challengeSecret == "campusos-development-challenge-key-change-before-production" {
			return fmt.Errorf("AUTH_CHALLENGE_ACTIVE_KEY_ID and a non-development AUTH_CHALLENGE_HMAC_KEYS secret are required when CAMPUSOS_ENV=production")
		}
		ipHashSecret := strings.TrimSpace(cfg.Auth.ChallengeIPHashSecret)
		if len(ipHashSecret) < 16 || ipHashSecret == "campusos-development-ip-hash-key-change-before-production" {
			return fmt.Errorf("a non-development AUTH_CHALLENGE_IP_HASH_SECRET is required when CAMPUSOS_ENV=production")
		}
		sessionIPHashSecret := strings.TrimSpace(cfg.Auth.SessionIPHashSecret)
		if len(sessionIPHashSecret) < 16 || sessionIPHashSecret == "campusos-development-session-ip-hash-key-change-before-production" {
			return fmt.Errorf("a non-development AUTH_SESSION_IP_HASH_SECRET is required when CAMPUSOS_ENV=production")
		}
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Email.Provider))
	if provider == "" {
		provider = "fake"
	}
	switch provider {
	case "fake":
		if environment != "development" && environment != "test" {
			return fmt.Errorf("EMAIL_PROVIDER=fake is allowed only when CAMPUSOS_ENV=development or test")
		}
	case "smtp":
		if strings.TrimSpace(cfg.Email.SMTPHost) == "" || strings.TrimSpace(cfg.Email.SMTPFrom) == "" {
			return fmt.Errorf("EMAIL_SMTP_HOST and EMAIL_SMTP_FROM are required when EMAIL_PROVIDER=smtp")
		}
		if cfg.Email.SMTPPort < 1 || cfg.Email.SMTPPort > 65535 {
			return fmt.Errorf("EMAIL_SMTP_PORT must be between 1 and 65535")
		}
		if timeout, err := time.ParseDuration(strings.TrimSpace(cfg.Email.SMTPTimeout)); err != nil || timeout <= 0 {
			return fmt.Errorf("EMAIL_SMTP_TIMEOUT must be a positive duration when EMAIL_PROVIDER=smtp")
		}
	default:
		return fmt.Errorf("EMAIL_PROVIDER must be smtp or fake, got %q", provider)
	}

	mode := strings.ToLower(strings.TrimSpace(cfg.Deployment.InstanceMode))
	if mode == "" || mode == "single" {
		return nil
	}
	if mode != "multi" {
		return fmt.Errorf("CAMPUSOS_INSTANCE_MODE must be single or multi, got %q", mode)
	}
	return fmt.Errorf("multi-instance write mode is blocked: User Storage Local Provider and legacy plugin SQLite KV require CAMPUSOS_INSTANCE_MODE=single")
}
