package server

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSecretRotationConfigAndDisabledAssembly(t *testing.T) {
	t.Setenv("CAMPUSOS_SECRET_ROTATION_ENABLED", "")
	t.Setenv("CAMPUSOS_SECRET_ROTATION_INTERVAL", "not a duration")
	stop, err := startSecretRotation(nil)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	for _, value := range []string{"yes", "TRUE", "1", "true"} {
		t.Setenv("CAMPUSOS_SECRET_ROTATION_ENABLED", value)
		if _, err := prepareSecretRotation(nil); !errors.Is(err, errSecretRotationSetup) {
			t.Fatalf("missing infrastructure accepted: %s %v", value, err)
		}
	}
	for _, interval := range []string{"0s", "500ms", "25h", "DSN-do-not-emit"} {
		cfg, err := secretRotationConfig(func(key string) string {
			if key == "CAMPUSOS_SECRET_ROTATION_ENABLED" {
				return "true"
			}
			return interval
		})
		if err == nil || strings.Contains(err.Error(), interval) {
			t.Fatalf("unsafe interval: %+v %v", cfg, err)
		}
	}
	cfg, err := secretRotationConfig(func(key string) string {
		if key == "CAMPUSOS_SECRET_ROTATION_ENABLED" {
			return "true"
		}
		return "2m"
	})
	if err != nil || !cfg.Enabled || cfg.Interval != 2*time.Minute || cfg.BatchSize != 100 || cfg.MaxPluginsPerRun != 25 {
		t.Fatalf("config: %+v %v", cfg, err)
	}
}
