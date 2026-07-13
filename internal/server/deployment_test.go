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
