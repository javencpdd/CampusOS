package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginV5CheckCommand(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile("../../sdk/typescript/tests/plugin-v5-manifest-native.fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Manifest, Release json.RawMessage }
	if json.Unmarshal(raw, &cases) != nil {
		t.Fatal("fixtures")
	}
	write := func(name string, raw []byte) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if os.WriteFile(p, raw, 0600) != nil {
			t.Fatal("write input")
		}
		return p
	}
	manifest := write("manifest.json", cases[0].Manifest)
	release := write("release.json", cases[0].Release)
	args := []string{"--manifest", manifest, "--release", release, "--host-catalog", "../../sdk/typescript/tests/plugin-v5-host-catalog.fixture.json", "--extension-catalog", "../../sdk/typescript/tests/plugin-v5-extension-catalog.fixture.json"}
	t.Run("valid-declaration-is-no-grant", func(t *testing.T) {
		var out bytes.Buffer
		if code := run(args, &out); code != 0 {
			t.Fatalf("exit %d: %s", code, out.String())
		}
		var result result
		if json.Unmarshal(out.Bytes(), &result) != nil || !result.Valid || result.Installable || result.PackageBytesVerified || result.SignatureVerified {
			t.Fatal("unsafe success contract")
		}
		if strings.Contains(out.String(), "Read public posts") || strings.Contains(out.String(), `"signature":`) {
			t.Fatal("echoed input")
		}
	})
	for _, bad := range [][]string{nil, {"--unknown"}, append(append([]string{}, args...), "unexpected")} {
		var out bytes.Buffer
		if run(bad, &out) != 2 {
			t.Fatal("usage accepted")
		}
	}
	t.Run("malformed-is-low-sensitivity", func(t *testing.T) {
		bad := append([]string{}, args...)
		bad[1] = write("bad.json", []byte(`{"secret":"do-not-echo"}`))
		var out bytes.Buffer
		if run(bad, &out) != 1 || strings.Contains(out.String(), "do-not-echo") {
			t.Fatalf("bad result %s", out.String())
		}
	})
	t.Run("bounded-input", func(t *testing.T) {
		bad := append([]string{}, args...)
		bad[1] = write("large.json", bytes.Repeat([]byte("x"), (1<<20)+1))
		var out bytes.Buffer
		if run(bad, &out) != 2 {
			t.Fatal("oversized file accepted")
		}
		bad[1] = dir
		if run(bad, &out) != 2 {
			t.Fatal("directory accepted")
		}
	})
}
