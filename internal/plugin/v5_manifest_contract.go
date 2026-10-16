package plugin

import (
	"encoding/json"
	"errors"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
	"strings"
)

var (
	ErrPluginV5ManifestInvalid    = errors.New("plugin.manifest_invalid")
	ErrPluginV5ManifestShape      = errors.New("plugin.manifest_shape_mismatch")
	ErrPluginV5ManifestCapability = errors.New("plugin.manifest_capability_unpublished")
	ErrPluginV5ManifestExtension  = errors.New("plugin.manifest_extension_unpublished")
	ErrPluginV5ManifestSchema     = errors.New("plugin.manifest_schema_mismatch")
	ErrPluginV5ManifestResource   = errors.New("plugin.manifest_resource_invalid")
	ErrPluginV5ManifestUpgrade    = errors.New("plugin.manifest_upgrade_unrecoverable")
	ErrPluginV5ManifestRelease    = errors.New("plugin.manifest_release_mismatch")
)

// Shared Go SDK DTOs keep the host and author wire fields identical.
type PluginV5UIArtifact = campusossdk.V5UIArtifact
type PluginV5BackendArtifact = campusossdk.V5BackendArtifact
type PluginV5Backend = campusossdk.V5Backend
type PluginV5ConsumeScope = campusossdk.V5ConsumeScope
type PluginV5Consume = campusossdk.V5Consume
type PluginV5SchemaFile = campusossdk.V5SchemaFile
type PluginV5Provide = campusossdk.V5Provide
type PluginV5ManifestResources = campusossdk.V5ManifestResources
type PluginV5PrivateData = campusossdk.V5PrivateData
type PluginV5Manifest = campusossdk.V5Manifest

func v5Scope(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	switch m["kind"] {
	case "public", "self":
		return v5Object("kind", map[string]v5Check{"kind": v5Enum("public", "self")})(v)
	case "collection":
		return v5Object("kind collection_ids", map[string]v5Check{"kind": v5Enum("collection"), "collection_ids": v5Array(1, 32, true, v5CollectionID)})(v)
	case "system_config":
		return v5Object("kind keys", map[string]v5Check{"kind": v5Enum("system_config"), "keys": v5Array(1, 32, true, v5ConfigName)})(v)
	}
	return false
}

func v5ManifestStructure(v any) bool {
	artifact := func(v any) bool {
		return v5Object("kind path digest", map[string]v5Check{"kind": v5Enum("wasm"), "path": v5PackagePath, "digest": v5Digest})(v) ||
			v5Object("kind image", map[string]v5Check{"kind": v5Enum("container"), "image": v5String(1, 512, `^[a-zA-Z0-9._:/-]+@sha256:[0-9a-f]{64}$`)})(v)
	}
	resource := v5Object("network_targets storage_bytes cpu_millis memory_mb max_concurrency timeout_ms", map[string]v5Check{
		"network_targets": v5Array(0, 32, true, v5String(10, 253, `(?s)^.*$`)), "storage_bytes": v5Integer(1, 10737418240),
		"cpu_millis": v5Integer(1, 4000), "memory_mb": v5Integer(1, 65536), "max_concurrency": v5Integer(1, 1024), "timeout_ms": v5Integer(1, 300000),
	})
	config := func(v any) bool { _, ok := v.(map[string]any); return ok }
	return v5Object("api_version publisher key version host_contracts runtime provides consumes configuration data", map[string]v5Check{
		"api_version": v5Enum("campusos.plugin/v5"), "publisher": v5Name, "key": v5Name, "version": v5Version,
		"host_contracts": v5Array(1, 64, true, v5HostVersion), "runtime": v5Enum("none", "wasm", "container"),
		"ui":      v5Array(1, 2, false, v5Object("audience entrypoint digest", map[string]v5Check{"audience": v5Enum("user", "admin"), "entrypoint": v5PackagePath, "digest": v5Digest})),
		"backend": v5Object("artifact transport", map[string]v5Check{"artifact": artifact, "transport": v5Enum("host-call", "http", "grpc")}),
		"provides": v5Array(0, 32, false, v5Object("id point contract_version input_schema output_schema capabilities data_mode", map[string]v5Check{
			"id": v5Name, "point": v5Point, "contract_version": v5Enum("v1"),
			"input_schema": v5String(1, 128, `^campusos\.[a-z.]+\.request/v1$`), "output_schema": v5String(1, 128, `^campusos\.[a-z.]+\.response/v1$`),
			"capabilities": v5Array(1, 16, true, v5ConfigName), "data_mode": v5Mode,
			"config_schema": v5Object("path digest", map[string]v5Check{"path": func(v any) bool { return v5PackagePath(v) && strings.HasSuffix(v.(string), ".json") }, "digest": v5Digest}),
		})),
		"consumes": v5Array(0, 64, false, v5Object("capability scope purpose required", map[string]v5Check{
			"capability": v5Capability, "scope": v5Scope, "purpose": func(v any) bool {
				s, ok := v.(string)
				return ok && len([]rune(s)) <= 300 && strings.TrimSpace(s) != ""
			}, "required": func(v any) bool { _, ok := v.(bool); return ok },
		})),
		"host_resources": resource, "configuration": config,
		"data": v5Object("config_scope private_kv_bytes private_object_bytes upgrade", map[string]v5Check{
			"config_scope": v5Enum("system", "user", "both"), "private_kv_bytes": v5Integer(0, 10737418240), "private_object_bytes": v5Integer(0, 10737418240),
			"upgrade": v5Object("schema_version mode rollback", map[string]v5Check{"schema_version": v5Integer(1, 9007199254740991), "mode": v5Enum("preserve", "migrate", "reset"), "rollback": v5Enum("snapshot", "forbidden")}),
		}),
	})(v)
}

func ParsePluginV5Manifest(raw []byte) (*PluginV5Manifest, error) {
	v, ok := v5JSON(raw)
	if !ok || !v5ManifestStructure(v) {
		return nil, ErrPluginV5ManifestInvalid
	}
	wire, err := json.Marshal(v5CanonicalNumbers(v))
	if err != nil {
		return nil, ErrPluginV5ManifestInvalid
	}
	var m PluginV5Manifest
	if json.Unmarshal(wire, &m) != nil {
		return nil, ErrPluginV5ManifestInvalid
	}
	obj := v.(map[string]any)
	_, backend := obj["backend"]
	_, resources := obj["host_resources"]
	if m.Runtime == "none" && (len(m.UI) == 0 || backend || resources) || m.Runtime != "none" && (!backend || !resources) {
		return nil, ErrPluginV5ManifestInvalid
	}
	if m.Runtime == "none" && len(m.Provides) > 0 || m.Runtime != "none" && (m.Backend.Artifact.Kind != m.Runtime || len(m.Provides) == 0) {
		return nil, ErrPluginV5ManifestShape
	}
	// Reuse the existing bounded configuration kernel, with exact field presence
	// checked first (Go struct decoding alone accepts case aliases and nulls).
	if !v5ConfigurationStructure(obj["configuration"]) {
		return nil, ErrPluginV5ManifestInvalid
	}
	if _, err := parsePluginV5ConfigDefinition(wire); err != nil {
		return nil, ErrPluginV5ManifestInvalid
	}
	seen := map[string]bool{}
	for _, ui := range m.UI {
		if seen[ui.Audience] {
			return nil, ErrPluginV5ManifestShape
		}
		seen[ui.Audience] = true
	}
	seen = map[string]bool{}
	for _, c := range m.Consumes {
		if seen[c.Capability] {
			return nil, ErrPluginV5ManifestCapability
		}
		seen[c.Capability] = true
	}
	return &m, nil
}

func v5ConfigurationStructure(v any) bool {
	field := v5Object("type title", map[string]v5Check{
		"type": v5Enum("string", "integer", "number", "boolean"), "title": v5String(1, 120, `(?s)^.*$`),
		"enum": v5Array(1, 32, false, func(v any) bool {
			switch v.(type) {
			case string, bool, json.Number:
				return true
			}
			return false
		}),
		"minimum": func(v any) bool { _, ok := v.(json.Number); return ok }, "maximum": func(v any) bool { _, ok := v.(json.Number); return ok },
		"minLength": v5Integer(0, 512), "maxLength": v5Integer(1, 512),
	})
	properties := func(v any) bool {
		m, ok := v.(map[string]any)
		if !ok || len(m) > 32 {
			return false
		}
		for k, value := range m {
			if !v5ConfigName(k) || !field(value) {
				return false
			}
		}
		return true
	}
	return v5Object("contract version schema defaults selectors secret_names", map[string]v5Check{
		"contract": v5Enum("campusos.plugin-config/v1"), "version": v5String(1, 128, `^v[1-9][0-9]*$`),
		"schema":       v5Object("type additionalProperties properties required", map[string]v5Check{"type": v5Enum("object"), "additionalProperties": func(v any) bool { return v == false }, "properties": properties, "required": v5Array(0, 32, true, v5ConfigName)}),
		"defaults":     func(v any) bool { m, ok := v.(map[string]any); return ok && len(m) <= 32 },
		"selectors":    v5Array(0, 32, false, v5Object("field kind", map[string]v5Check{"field": v5ConfigName, "kind": v5Enum("profile", "public_collection")})),
		"secret_names": v5Array(0, 32, true, v5ConfigName),
	})(v)
}
