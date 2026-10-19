package plugin_test

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
	containerimage "github.com/campusos/CampusOS/internal/plugin/container"
)

func TestV5ContainerImageActualDocker(t *testing.T) {
	ref, expected := os.Getenv("CAMPUSOS_V5_CONTAINER_TEST_REFERENCE"), os.Getenv("CAMPUSOS_V5_CONTAINER_TEST_CONFIG_ID")
	if ref == "" || expected == "" {
		t.Skip("requires owned immutable06 image, not a live plugin")
	}
	if !strings.HasPrefix(ref, "localhost/campusos-owned-v12-06-") {
		t.Fatal("requires exact owned06 repository")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p := plugin.NewV5ContainerTestProgram(t, ref)
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:1")
	t.Setenv("DOCKER_CONTEXT", "must-not-be-used")
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("DATABASE_DSN", "OWNED_FAKE_SENTINEL_MUST_NOT_LEAVE_HOST")
	got, err := containerimage.ResolveLocalImage(ctx, p)
	if err != nil || got.Reference() != ref || got.ID() != expected || got.PackageDigest() != p.PackageDigest() || got.Coordinate() != p.Coordinate() || got.Transport() != "http" || got.Architecture() != runtime.GOARCH {
		t.Fatal("real local image mapping", err)
	}
	t.Run("missing-never-pulls-or-falls-back", func(t *testing.T) {
		missing := strings.Split(ref, "@")[0] + "@sha256:" + strings.Repeat("0", 64)
		p := plugin.NewV5ContainerTestProgram(t, missing)
		if got, err := containerimage.ResolveLocalImage(ctx, p); err != containerimage.ErrImageUnavailable || got.ID() != "" {
			t.Fatal("missing exact digest accepted", err)
		}
	})
	t.Run("wrong-repository-cannot-use-same-digest", func(t *testing.T) {
		other := strings.Replace(ref, "localhost/", "absent-registry.invalid/", 1)
		p := plugin.NewV5ContainerTestProgram(t, other)
		if got, err := containerimage.ResolveLocalImage(ctx, p); err == nil || got.ID() != "" {
			t.Fatal("unbound repository accepted")
		}
	})
	t.Run("current-caller-cancellation", func(t *testing.T) {
		canceled, stop := context.WithCancel(ctx)
		stop()
		if got, err := containerimage.ResolveLocalImage(canceled, p); err != context.Canceled || got.ID() != "" {
			t.Fatal("canceled resolver ran", err)
		}
	})
	if path := os.Getenv("CAMPUSOS_V5_CONTAINER_TEST_PROOF"); path != "" {
		proof, _ := json.Marshal(map[string]any{"reference": got.Reference(), "config_id": got.ID(), "package_digest": got.PackageDigest(), "architecture": got.Architecture(), "transport": got.Transport(), "registry_pull": false, "plugin_started": false})
		if err := os.WriteFile(path, append(proof, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
