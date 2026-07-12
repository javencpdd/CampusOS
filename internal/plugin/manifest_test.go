package plugin

import "testing"

func TestParseManifestConfigSchema(t *testing.T) {
	manifest, err := ParseManifest([]byte(`
name: schema-plugin
version: "0.1.0"
runtime: wasm
config_schema:
  fields:
    - key: title
      label: "Title"
      type: string
      required: true
      default: "My Page"
    - key: layout
      label: "Layout"
      type: select
      options:
        - label: "Grid"
          value: "grid"
        - label: "List"
          value: "list"
`))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.ConfigSchema == nil || len(manifest.ConfigSchema.Fields) != 2 {
		t.Fatalf("expected config schema fields, got %#v", manifest.ConfigSchema)
	}
	if manifest.ConfigSchema.Fields[0].Key != "title" {
		t.Fatalf("unexpected first field: %#v", manifest.ConfigSchema.Fields[0])
	}
}

func TestParseManifestRejectsInvalidConfigSchema(t *testing.T) {
	_, err := ParseManifest([]byte(`
name: schema-plugin
version: "0.1.0"
runtime: wasm
config_schema:
  fields:
    - key: title
      type: unknown
`))
	if err == nil {
		t.Fatalf("expected unsupported config field type to fail")
	}
}

func TestParseManifestAcceptsBuiltinRuntime(t *testing.T) {
	manifest, err := ParseManifest([]byte(`
name: personal-space
version: "0.1.0"
runtime: builtin
storage:
  type: none
`))
	if err != nil {
		t.Fatalf("parse builtin manifest: %v", err)
	}
	if manifest.Runtime != "builtin" {
		t.Fatalf("expected builtin runtime, got %q", manifest.Runtime)
	}
	if manifest.Scope != ScopeSystem {
		t.Fatalf("builtin plugin should default to system scope, got %q", manifest.Scope)
	}
}

func TestParseManifestDefaultsExternalPluginsToUserScope(t *testing.T) {
	manifest, err := ParseManifest([]byte(`
name: user-extension
version: "0.1.0"
runtime: wasm
`))
	if err != nil {
		t.Fatalf("parse user manifest: %v", err)
	}
	if manifest.Scope != ScopeUser {
		t.Fatalf("external plugin should default to user scope, got %q", manifest.Scope)
	}
}

func TestParseManifestRejectsInvalidScope(t *testing.T) {
	_, err := ParseManifest([]byte(`
name: invalid-scope
version: "0.1.0"
runtime: wasm
scope: tenant
`))
	if err == nil {
		t.Fatal("expected invalid scope to be rejected")
	}
}

func TestParseManifestRejectsDuplicateConfigSchemaKeys(t *testing.T) {
	_, err := ParseManifest([]byte(`
name: schema-plugin
version: "0.1.0"
runtime: wasm
config_schema:
  fields:
    - key: title
      type: string
    - key: title
      type: text
`))
	if err == nil {
		t.Fatalf("expected duplicated config field key to fail")
	}
}

func TestManifestLifecycleDefaultsDoNotDependOnScope(t *testing.T) {
	cases := []struct{ runtime, scope, want string }{
		{"builtin", "system", ActivationRestart},
		{"grpc", "system", ActivationPluginRestart},
		{"grpc", "user", ActivationPluginRestart},
		{"wasm", "system", ActivationHot},
		{"wasm", "user", ActivationHot},
	}
	for _, test := range cases {
		manifest, err := ParseManifest([]byte("name: lifecycle-test\nversion: 0.1.0\nruntime: " + test.runtime + "\nscope: " + test.scope + "\n"))
		if err != nil {
			t.Fatalf("parse %s/%s: %v", test.runtime, test.scope, err)
		}
		if got := manifest.BackendActivationMode(); got != test.want {
			t.Fatalf("%s/%s mode=%s want=%s", test.runtime, test.scope, got, test.want)
		}
		if manifest.FrontendActivationMode() != ActivationHot {
			t.Fatal("frontend must default to hot")
		}
	}
}

func TestManifestUIContractRejectsUnknownAction(t *testing.T) {
	_, err := ParseManifest([]byte(`
name: ui-test
version: 0.1.0
runtime: wasm
ui:
  surfaces:
    - id: plugin.ui.page
      version: v1
      type: page
      layout_role: main
      renderer: schema
      schema: { component: stack }
      action_ids: [plugin.ui.missing]
`))
	if err == nil {
		t.Fatal("expected unknown action reference to fail")
	}
}

func TestManifestUIContractRejectsDeclarativeCoreRouteHijack(t *testing.T) {
	_, err := ParseManifest([]byte(`
name: route-hijack
version: 0.1.0
runtime: wasm
ui:
  surfaces:
    - id: plugin.route-hijack.page.main
      version: v1
      type: page
      layout_role: main
      renderer: schema
      schema: { component: text, text: unsafe }
  routes:
    - id: plugin.route-hijack.route.main
      path: /login
      surface_id: plugin.route-hijack.page.main
`))
	if err == nil {
		t.Fatal("expected declarative plugin route outside its namespace to fail")
	}
}

func TestManifestUIContractRejectsThirdPartyTrustedModule(t *testing.T) {
	_, err := ParseManifest([]byte(`
name: trusted-hijack
version: 0.1.0
runtime: wasm
ui:
  surfaces:
    - id: plugin.trusted-hijack.page.main
      version: v1
      type: page
      layout_role: main
      renderer: trusted-module
      module_id: core.schedule
`))
	if err == nil {
		t.Fatal("expected third-party trusted module to fail")
	}
}
