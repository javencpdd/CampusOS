package v4

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validManifest = `api_version: campusos.plugin/v4
key: campusos.pdf-viewer
version: 2.0.0-dev.1
publisher:
  id: campusos
  name: CampusOS
display_name: PDF 文档预览
description: 受宿主控制的 PDF 阅读器。
compatibility:
  host: ">=1.1"
  ui: campusos.ui/v3
  bridge: campusos.bridge/v1
backend:
  runtime: none
artifacts:
  user_ui:
    source: frontend/user
    entry: ui/user/index.html
  admin_ui:
    source: frontend/admin
    entry: ui/admin/index.html
ui:
  user:
    surfaces:
      - id: preview
        route: preview
        title: PDF 预览
        presentations: [modal, drawer, fullscreen, new-tab]
  admin:
    surfaces:
      - id: settings
        route: settings
        title: PDF 插件设置
        presentations: [page]
preview_providers:
  - id: pdf
    resource_types: [article_attachment, personal_asset, personal_document]
    mime_types: [application/pdf]
    surface_id: preview
capabilities:
  - code: personal_space_file.self.read
    required: true
    purpose: 预览本人空间中的 PDF。
    scope: self
configuration:
  system:
    schema: config/system.schema.json
    defaults: config/system.defaults.json
    version: v1
  user:
    schema: config/user.schema.json
    defaults: config/user.defaults.json
    version: v1
data:
  user_config_max_bytes: 262144
`

func TestManifestRejectsUnsafeContractValues(t *testing.T) {
	for name, replace := range map[string]string{
		"wrong version":   "api_version: campusos.plugin/v5",
		"path escape":     "source: ../frontend/user",
		"remote artifact": "entry: https://example.invalid/plugin.js",
		"unknown runtime": "runtime: shell",
	} {
		t.Run(name, func(t *testing.T) {
			data := validManifest
			switch name {
			case "wrong version":
				data = strings.Replace(data, "api_version: campusos.plugin/v4", replace, 1)
			case "path escape":
				data = strings.Replace(data, "source: frontend/user", replace, 1)
			case "remote artifact":
				data = strings.Replace(data, "entry: ui/user/index.html", replace, 1)
			case "unknown runtime":
				data = strings.Replace(data, "runtime: none", replace, 1)
			}
			filename := filepath.Join(t.TempDir(), "plugin.yaml")
			if err := os.WriteFile(filename, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(filename); err == nil {
				t.Fatal("unsafe manifest was accepted")
			}
		})
	}
}

func TestValidateSourceAndReleaseDirectoriesHaveDifferentResponsibilities(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"frontend/user", "frontend/admin", "config"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "plugin.yaml"), []byte(validManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"README.md", "LICENSE"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"system.schema.json", "system.defaults.json", "user.schema.json", "user.defaults.json"} {
		contents := []byte("{}\n")
		if strings.HasSuffix(name, ".schema.json") {
			contents = []byte(`{"type":"object","additionalProperties":false}`)
		}
		if err := os.WriteFile(filepath.Join(root, "config", name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ValidateSourceDirectory(root); err != nil {
		t.Fatalf("source must validate before build: %v", err)
	}
	if _, err := ValidateReleaseDirectory(root); err == nil {
		t.Fatal("release accepted without built artifact")
	}
	for _, name := range []string{"ui/user/index.html", "ui/admin/index.html"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte("<!doctype html>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ValidateReleaseDirectory(root); err != nil {
		t.Fatalf("release entries rejected: %v", err)
	}
}

func TestFindSourcePackagesSkipsInstalledAndStagingDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"campusos.pdf-viewer", ".installed", ".staging"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	packages, err := FindSourcePackages(root)
	if err != nil || len(packages) != 1 || filepath.Base(packages[0]) != "campusos.pdf-viewer" {
		t.Fatalf("unexpected source package result: %v, %v", packages, err)
	}
}

func TestValidateCapabilitiesUsesHostOwnedCatalog(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "plugin.yaml")
	if err := os.WriteFile(filename, []byte(validManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	resolver := func(code string) (CapabilityDescriptor, bool) {
		if code == "personal_space_file.self.read" {
			return CapabilityDescriptor{Code: code, Scope: "self"}, true
		}
		return CapabilityDescriptor{}, false
	}
	if err := manifest.ValidateCapabilities(resolver); err != nil {
		t.Fatalf("catalog matching capability was rejected: %v", err)
	}
	manifest.Capabilities[0].Scope = "system"
	if err := manifest.ValidateCapabilities(resolver); err == nil {
		t.Fatal("scope escalation was accepted")
	}
}
