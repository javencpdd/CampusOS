package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	platformversion "github.com/campusos/CampusOS/internal/platform/version"
	"github.com/campusos/CampusOS/internal/plugin"
)

func TestPluginInitCreatesScaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-plugin")
	var out, errout bytes.Buffer
	if run([]string{"plugin", "init", "sample-plugin", "--dir", dir}, &out, &errout) != 0 {
		t.Fatal(errout.String())
	}
	raw, err := os.ReadFile(filepath.Join(dir, plugin.PluginV5ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugin.ParsePluginV5Manifest(raw)
	if err != nil || m.Key != "sample-plugin" || m.Runtime != "none" || m.APIVersion != "campusos.plugin/v5" {
		t.Fatalf("manifest: %v", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "plugin.yaml")); !os.IsNotExist(err) {
		t.Fatal("generated a legacy wire manifest")
	}
}

func TestVersionReadsPlatformVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"version", "--short"}, &stdout, &stderr); code != 0 {
		t.Fatalf("version command code=%d stderr=%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != platformversion.Number {
		t.Fatalf("version output=%q", got)
	}
}

func TestIdentityResetPasswordRejectsNonSystemAccountBeforeTerminalRead(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"identity", "reset-password", "--email", "member@example.test"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected system-only recovery command to reject another email")
	}
	if !strings.Contains(stderr.String(), "only the system administrator account") {
		t.Fatalf("unexpected identity command error: %s", stderr.String())
	}
}

func TestIdentityRestoreAdminAdmissionRejectsInvalidArgumentsBeforeTerminalRead(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"identity", "restore-admin-admission", "--user-id", "0", "--reason", "operator review"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected administrator admission recovery command to reject an invalid user ID")
	}
	if !strings.Contains(stderr.String(), "positive --user-id") {
		t.Fatalf("unexpected identity command error: %s", stderr.String())
	}
}

func TestIdentityResetMFARejectsInvalidArgumentsBeforeTerminalRead(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"identity", "reset-mfa", "--user-id", "0", "--reason", "lost device"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected local MFA recovery command to reject an invalid user ID")
	}
	if !strings.Contains(stderr.String(), "positive --user-id") {
		t.Fatalf("unexpected identity command error: %s", stderr.String())
	}
}

func TestPluginInitRejectsRemovedRuntime(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-plugin")
	var out, errout bytes.Buffer
	if run([]string{"plugin", "init", "sample-plugin", "--runtime", "grpc", "--dir", dir}, &out, &errout) == 0 {
		t.Fatal("legacy runtime accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("rejected init wrote source")
	}
}

func TestPluginInitRejectsBuiltinModules(t *testing.T) {
	var out, errout bytes.Buffer
	if run([]string{"plugin", "init", "builtin-plugin", "--runtime", "builtin"}, &out, &errout) == 0 {
		t.Fatal("builtin runtime accepted as external package")
	}
}

func TestResourceAdoptAcceptsDocumentedDirectoryFirstSyntax(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "style.yaml"), []byte("name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"resource", "adopt", directory, "--type", "skill"}, &stdout, &stderr); code != 0 {
		t.Fatalf("resource adopt failed: code=%d stderr=%s", code, stderr.String())
	}
	if code := run([]string{"resource", "inspect", directory}, &stdout, &stderr); code != 0 {
		t.Fatalf("resource inspect failed: code=%d stderr=%s", code, stderr.String())
	}
}

func TestResourceLegacyAdoptionPreservesButDoesNotPublishAppearancePack(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "style.yaml"), []byte(`schema_version: page-style-pack.v1
target: personal-space
name: legacy-space
version: 1.0.0
entry: templates/page.html
styles: [styles/theme.css]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "templates", "page.html"), []byte(`<section class="cstyle-page">Legacy</section>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "styles", "theme.css"), []byte(`.public-space[data-campusos-space] .cstyle-page { color: #111827; background: #ffffff; }`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"resource", "adopt", directory, "--type", "space-style-pack"}, &stdout, &stderr); code == 0 {
		t.Fatalf("strict resource adoption accepted an old appearance package: stdout=%s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(directory, "resource.json")); err == nil {
		t.Fatal("strict resource adoption wrote a manifest for a legacy package")
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"resource", "adopt-legacy", directory, "--type", "space-style-pack"}, &stdout, &stderr); code != 0 {
		t.Fatalf("legacy layout adoption failed: code=%d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"resource", "inspect", directory}, &stdout, &stderr); code != 0 {
		t.Fatalf("compatibility inspection failed: code=%d stderr=%s", code, stderr.String())
	}
	var inspection map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &inspection); err != nil {
		t.Fatalf("decode inspection: %v", err)
	}
	if inspection["delivery_status"] != "legacy-readonly" {
		t.Fatalf("unexpected delivery status: %#v", inspection)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"resource", "inspect", directory, "--strict"}, &stdout, &stderr); code == 0 {
		t.Fatal("strict inspection accepted a legacy-only appearance package")
	}
}

func TestPluginInspectPrintsManifestSummary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-plugin")
	var out, errout bytes.Buffer
	if run([]string{"plugin", "init", "sample-plugin", "--dir", dir}, &out, &errout) != 0 {
		t.Fatal(errout.String())
	}
	out.Reset()
	if run([]string{"plugin", "inspect", dir}, &out, &errout) != 0 {
		t.Fatal(errout.String())
	}
	var summary map[string]any
	if json.Unmarshal(out.Bytes(), &summary) != nil || summary["key"] != "sample-plugin" || summary["api_version"] != "campusos.plugin/v5" || summary["installed"] != false {
		t.Fatal("invalid summary")
	}
}

func TestPluginInitRejectsInvalidName(t *testing.T) {
	var out, errout bytes.Buffer
	if run([]string{"plugin", "init", "InvalidName"}, &out, &errout) == 0 {
		t.Fatal("invalid name accepted")
	}
}

func TestPluginPackRejectsInvalidConfigSchema(t *testing.T) {
	dir := testV5CLISource(t)
	p := filepath.Join(dir, plugin.PluginV5ManifestFile)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		t.Fatal("json")
	}
	m["configuration"].(map[string]any)["install_script"] = "do-not-run"
	raw, _ = json.Marshal(m)
	if os.WriteFile(p, raw, 0600) != nil {
		t.Fatal("write")
	}
	var out, errout bytes.Buffer
	output := filepath.Join(t.TempDir(), "package.tar.gz")
	if run([]string{"plugin", "pack", dir, "--out", output}, &out, &errout) == 0 {
		t.Fatal("invalid schema accepted")
	}
	if _, err = os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("invalid pack wrote output")
	}
}

func TestPluginPackAndInstall(t *testing.T) {
	dir := testV5CLISource(t)
	output := filepath.Join(t.TempDir(), "package.tar.gz")
	var out, errout bytes.Buffer
	if os.MkdirAll(filepath.Join(dir, "data"), 0700) != nil || os.WriteFile(filepath.Join(dir, "data", "runtime.db"), []byte("not-shipped"), 0600) != nil {
		t.Fatal("fixture")
	}
	if run([]string{"plugin", "pack", dir, "--out", output}, &out, &errout) != 0 {
		t.Fatal(errout.String())
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	p, err := plugin.InspectPluginV5Package(raw)
	if err != nil || p.FileCount != 2 {
		t.Fatal("pack copied source or data")
	}
	target := filepath.Join(t.TempDir(), "installed")
	if run([]string{"plugin", "install", output, "--dir", target}, &out, &errout) == 0 {
		t.Fatal("03a performed an installation")
	}
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("install changed files")
	}
}

func TestPluginInstallRefusesExistingDirectory(t *testing.T) {
	target := t.TempDir()
	sentinel := filepath.Join(target, "keep")
	if os.WriteFile(sentinel, []byte("retained"), 0600) != nil {
		t.Fatal("write")
	}
	var out, errout bytes.Buffer
	if run([]string{"plugin", "install", "old.tar.gz", "--dir", target}, &out, &errout) == 0 {
		t.Fatal("legacy install accepted")
	}
	raw, err := os.ReadFile(sentinel)
	if err != nil || string(raw) != "retained" {
		t.Fatal("rejected install mutated existing data")
	}
}

func TestPluginVerifyRejectsUnknownPermission(t *testing.T) {
	dir := testV5CLISource(t)
	path := filepath.Join(dir, plugin.PluginV5ManifestFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		t.Fatal("json")
	}
	m["consumes"] = []any{map[string]any{"capability": "host.db.query", "scope": map[string]any{"kind": "public"}, "purpose": "Forbidden DB access", "required": false}}
	raw, _ = json.Marshal(m)
	if os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("write")
	}
	var out, errout bytes.Buffer
	if run([]string{"plugin", "validate", dir}, &out, &errout) == 0 || !strings.Contains(errout.String(), "capability_unpublished") {
		t.Fatal("unknown capability accepted")
	}
}

func TestRemovedPluginCommandsCannotExecuteSource(t *testing.T) {
	dir := t.TempDir()
	if os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte("api_version: campusos.plugin/v4\n"), 0600) != nil {
		t.Fatal("write")
	}
	for _, command := range []string{"v4", "dev", "build", "test", "doctor", "conformance", "watch"} {
		var out, errout bytes.Buffer
		if run([]string{"plugin", command, dir}, &out, &errout) == 0 {
			t.Fatalf("legacy command %s accepted", command)
		}
	}
}
func testV5CLISource(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sample-plugin")
	var out, errout bytes.Buffer
	if run([]string{"plugin", "init", "sample-plugin", "--dir", dir}, &out, &errout) != 0 {
		t.Fatal(errout.String())
	}
	return dir
}
