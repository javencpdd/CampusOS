package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestV2ManagedExampleConformsToUIContract keeps the checked-in example from
// silently drifting away from the same parser and safety rules that gate a
// real plugin installation.
func TestV2ManagedExampleConformsToUIContract(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate UI v2 example test source")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "examples", "plugins", "v2-managed-example", "plugin.yaml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read UI v2 example: %v", err)
	}
	manifest, err := ParseManifest(contents)
	if err != nil {
		t.Fatalf("parse UI v2 example: %v", err)
	}
	if manifest.UI.ContractVersion != CurrentUIContract {
		t.Fatalf("ui contract=%q, want %q", manifest.UI.ContractVersion, CurrentUIContract)
	}
	if len(manifest.UI.Surfaces) != 1 || len(manifest.UI.Actions) != 1 || len(manifest.UI.Routes) != 1 {
		t.Fatalf("unexpected example contribution shape: %#v", manifest.UI)
	}
	surface := manifest.UI.Surfaces[0]
	if surface.Renderer != "schema" || len(surface.Presentations) != 4 {
		t.Fatalf("example must stay declarative and enumerate all host presentations: %#v", surface)
	}
	action := manifest.UI.Actions[0]
	if action.Kind != "open-surface" || action.Path != "" || action.Method != "" || action.SurfaceID != surface.ID {
		t.Fatalf("example must request only a named host surface: %#v", action)
	}
}
