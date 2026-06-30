package ai

import (
	"testing"

	"github.com/campusos/CampusOS/pkg/config"
)

func TestNewServiceFromConfigDisabled(t *testing.T) {
	service, err := NewServiceFromConfig(config.AIConfig{
		Enabled: false,
	})
	if err != nil {
		t.Fatalf("disabled service should not fail: %v", err)
	}
	status := service.Status()
	if status.Enabled || status.Ready {
		t.Fatalf("unexpected disabled status: %#v", status)
	}
}

func TestNewServiceFromConfigEnabled(t *testing.T) {
	service, err := NewServiceFromConfig(config.AIConfig{
		Enabled:              true,
		Provider:             "openai-compatible",
		BaseURL:              "https://ai.example.test/v1",
		Model:                "campus-model",
		APIKey:               "test-secret",
		Timeout:              "30s",
		MaxRequestsPerMinute: 60,
		MaxConcurrent:        4,
	})
	if err != nil {
		t.Fatalf("enabled service should not fail: %v", err)
	}
	status := service.Status()
	if !status.Enabled || !status.Ready || status.Provider != "openai-compatible" {
		t.Fatalf("unexpected status: %#v", status)
	}
	if status.Config["api_key_configured"] != true {
		t.Fatalf("expected redacted config to show key configured: %#v", status.Config)
	}
	if _, ok := status.Config["api_key"]; ok {
		t.Fatalf("status leaked api_key: %#v", status.Config)
	}
}

func TestNewServiceFromConfigInvalidTimeout(t *testing.T) {
	service, err := NewServiceFromConfig(config.AIConfig{
		Enabled:  true,
		Provider: "openai-compatible",
		BaseURL:  "https://ai.example.test/v1",
		Model:    "campus-model",
		APIKey:   "test-secret",
		Timeout:  "not-a-duration",
	})
	if err == nil {
		t.Fatalf("expected invalid timeout error")
	}
	status := service.Status()
	if !status.Enabled || status.Ready || status.Error == "" {
		t.Fatalf("unexpected invalid config status: %#v", status)
	}
}
