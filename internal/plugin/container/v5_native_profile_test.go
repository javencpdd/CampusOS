package container

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This calls the real compiled OS implementation. An injected Windows reader on
// Linux cannot create Windows target evidence through this test.
func TestV5ContainerNativeProfile(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("requires supported native Container host")
	}
	platform, err := currentDockerPlatform()
	if err != nil || platform.os != runtime.GOOS || !filepath.IsAbs(platform.binary) {
		t.Fatal("native Docker platform", err)
	}
	command := localCommand(context.Background(), t.TempDir(), "version")
	if command.Err != nil || command.Path != platform.binary || command.Args[1] != "--host="+platform.host {
		t.Fatal("native command profile")
	}
	proof := struct {
		GOOS       string `json:"goos"`
		GOARCH     string `json:"goarch"`
		Binary     string `json:"binary"`
		Host       string `json:"host"`
		SystemRoot string `json:"system_root"`
	}{runtime.GOOS, runtime.GOARCH, platform.binary, platform.host, platform.systemRoot}
	if path := os.Getenv("CAMPUSOS_V5_CONTAINER_NATIVE_PROFILE"); path != "" {
		if !filepath.IsAbs(path) || !strings.Contains(filepath.ToSlash(path), "/.cache/v12-06-runtime/acceptance.") {
			t.Fatal("requires owned06 native profile path")
		}
		raw, err := json.Marshal(proof)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(append(raw, '\n'))
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal("native profile write", writeErr, closeErr)
		}
	}
}
