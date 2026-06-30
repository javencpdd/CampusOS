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
