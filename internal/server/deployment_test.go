package server

import (
	"strings"
	"testing"

	"github.com/campusos/CampusOS/pkg/config"
)

func TestValidateDeploymentBlocksUnsafeMultiWriterMode(t *testing.T) {
	if err := validateDeployment(&config.Config{Deployment: config.DeploymentConfig{InstanceMode: "single"}}); err != nil {
		t.Fatalf("single mode: %v", err)
	}
	err := validateDeployment(&config.Config{Deployment: config.DeploymentConfig{InstanceMode: "multi"}})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("multi mode = %v, want safety rejection", err)
	}
}

func TestValidateDeploymentRequiresProductionBootstrapSecret(t *testing.T) {
	base := &config.Config{
		JWT:        config.JWTConfig{Secret: "production-jwt-secret"},
		Auth:       config.AuthConfig{PasswordHashEnabled: true},
		Deployment: config.DeploymentConfig{Environment: "production", InstanceMode: "single"},
	}
	if err := validateDeployment(base); err == nil || !strings.Contains(err.Error(), "AUTH_BOOTSTRAP_ADMIN_SECRET") {
		t.Fatalf("missing bootstrap secret error = %v", err)
	}
	base.Auth.BootstrapAdminSecret = "sufficient-production-bootstrap-secret"
	base.Auth.ChallengeActiveKeyID = "production-v1"
	base.Auth.ChallengeHMACKeys = map[string]string{"production-v1": "production-challenge-signing-secret"}
	base.Auth.ChallengeIPHashSecret = "production-ip-hash-secret"
	base.Auth.SessionIPHashSecret = "production-session-ip-hash-secret"
	base.Email = config.EmailConfig{
		Provider: "smtp", SMTPHost: "smtp.example.test", SMTPPort: 587,
		SMTPFrom: "noreply@example.test", SMTPTimeout: "10s", SMTPStartTLS: true,
	}
	if err := validateDeployment(base); err != nil {
		t.Fatalf("production config with secret: %v", err)
	}
}

func TestValidateDeploymentRejectsFakeEmailOutsideLocalProfiles(t *testing.T) {
	cfg := &config.Config{
		Deployment: config.DeploymentConfig{Environment: "staging", InstanceMode: "single"},
		Auth:       config.AuthConfig{PasswordHashEnabled: true},
		Email:      config.EmailConfig{Provider: "fake"},
	}
	if err := validateDeployment(cfg); err == nil || !strings.Contains(err.Error(), "EMAIL_PROVIDER=fake") {
		t.Fatalf("unsafe fake email configuration error = %v", err)
	}
}

func TestValidateDeploymentRejectsUnsafeAuthOutsideDevelopment(t *testing.T) {
	cfg := &config.Config{
		JWT: config.JWTConfig{Secret: "production-jwt-secret"},
		Auth: config.AuthConfig{
			PasswordHashEnabled:          false,
			BootstrapAdminSecret:         "sufficient-production-bootstrap-secret",
			AllowDevelopmentDefaultAdmin: true,
		},
		Deployment: config.DeploymentConfig{Environment: "staging", InstanceMode: "single"},
	}
	if err := validateDeployment(cfg); err == nil || !strings.Contains(err.Error(), "AUTH_PASSWORD_HASH_ENABLED") {
		t.Fatalf("plaintext password policy error = %v", err)
	}
}
