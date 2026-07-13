package server

import (
	"fmt"
	"strings"

	"github.com/campusos/CampusOS/pkg/config"
)

// validateDeployment refuses a configuration that would silently turn local
// user files or legacy SQLite KV into multi-writer storage. A shared provider
// and coordinated runtime are intentionally future work, not a v0.9 promise.
func validateDeployment(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("server config is required")
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
