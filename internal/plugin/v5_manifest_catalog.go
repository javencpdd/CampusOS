package plugin

import "encoding/json"

type v5HostCapability struct {
	Code               string   `json:"code"`
	ScopeKind          string   `json:"scope_kind"`
	DataClassification string   `json:"data_classification"`
	ConsentRequired    bool     `json:"consent_required"`
	Collections        []string `json:"available_public_collection_ids,omitempty"`
	Keys               []string `json:"available_config_keys,omitempty"`
}
type v5HostManifestCatalog struct {
	Contract     string             `json:"contract"`
	HostContract string             `json:"host_contract"`
	Items        []v5HostCapability `json:"items"`
}
type v5ExtensionDescriptor struct {
	Point           string   `json:"point"`
	ContractVersion string   `json:"contract_version"`
	InputSchema     string   `json:"input_schema"`
	OutputSchema    string   `json:"output_schema"`
	Capabilities    []string `json:"capabilities"`
	DataModes       []string `json:"data_modes"`
}
type v5ExtensionManifestCatalog struct {
	Contract     string                  `json:"contract"`
	HostContract string                  `json:"host_contract"`
	Points       []v5ExtensionDescriptor `json:"points"`
}
type v5ReleaseEnvelope struct {
	Publisher     string `json:"publisher"`
	Key           string `json:"key"`
	Version       string `json:"version"`
	PackageDigest string `json:"package_digest"`
	Signature     struct {
		KeyID string `json:"key_id"`
		Value string `json:"value"`
	} `json:"signature"`
}

// ValidatePluginV5Manifest checks a declaration against explicit catalog
// snapshots and a release envelope. A future server caller must load these
// catalogs from host-owned current facts, never from plugin/request fields.
// This function performs no signature, package-byte, trust, grant, installation
// or execution verification. Successful validation is not an authority proof.
func ValidatePluginV5Manifest(manifest, hostCatalog, extensionCatalog, release []byte) (*PluginV5Manifest, error) {
	m, err := ParsePluginV5Manifest(manifest)
	if err != nil {
		return nil, err
	}
	hv, hok := v5JSON(hostCatalog)
	ev, eok := v5JSON(extensionCatalog)
	if !hok || !eok || !v5HostCatalogStructure(hv) || !v5ExtensionCatalogStructure(ev) {
		return nil, ErrPluginV5ManifestSchema
	}
	var host v5HostManifestCatalog
	var ext v5ExtensionManifestCatalog
	hwire, _ := json.Marshal(hv)
	ewire, _ := json.Marshal(ev)
	if json.Unmarshal(hwire, &host) != nil || json.Unmarshal(ewire, &ext) != nil {
		return nil, ErrPluginV5ManifestSchema
	}
	if !v5Contains(m.HostContracts, host.HostContract) || !v5Contains(m.HostContracts, ext.HostContract) {
		return nil, ErrPluginV5ManifestSchema
	}
	capabilities := map[string]v5HostCapability{}
	for _, item := range host.Items {
		if _, duplicate := capabilities[item.Code]; duplicate {
			return nil, ErrPluginV5ManifestSchema
		}
		capabilities[item.Code] = item
	}
	for _, c := range m.Consumes {
		item, ok := capabilities[c.Capability]
		if !ok || item.ScopeKind != c.Scope.Kind || !v5Subset(c.Scope.CollectionIDs, item.Collections) || !v5Subset(c.Scope.Keys, item.Keys) {
			return nil, ErrPluginV5ManifestCapability
		}
	}
	points := map[string]v5ExtensionDescriptor{}
	for _, item := range ext.Points {
		key := item.Point + "@" + item.ContractVersion
		if _, duplicate := points[key]; duplicate {
			return nil, ErrPluginV5ManifestSchema
		}
		points[key] = item
	}
	ids := map[string]bool{}
	for _, p := range m.Provides {
		item, ok := points[p.Point+"@"+p.ContractVersion]
		if !ok || ids[p.ID] {
			return nil, ErrPluginV5ManifestExtension
		}
		ids[p.ID] = true
		if p.InputSchema != item.InputSchema || p.OutputSchema != item.OutputSchema || !v5Subset(p.Capabilities, item.Capabilities) || !v5Contains(item.DataModes, p.DataMode) {
			return nil, ErrPluginV5ManifestSchema
		}
	}
	if m.HostResources != nil && m.Data.PrivateKVBytes+m.Data.PrivateObjectBytes > m.HostResources.StorageBytes {
		return nil, ErrPluginV5ManifestResource
	}
	if m.Data.Upgrade.Mode == "migrate" && m.Data.Upgrade.Rollback == "snapshot" && m.Data.PrivateKVBytes+m.Data.PrivateObjectBytes > 0 {
		return nil, ErrPluginV5ManifestUpgrade
	}
	rv, ok := v5JSON(release)
	if !ok || !v5Object("publisher key version package_digest signature", map[string]v5Check{
		"publisher": v5Name, "key": v5Name, "version": v5Version, "package_digest": v5Digest,
		"signature": v5Object("key_id value", map[string]v5Check{"key_id": v5String(1, 128, `(?s)^.*$`), "value": v5String(1, 2048, `(?s)^.*$`)}),
	})(rv) {
		return nil, ErrPluginV5ManifestInvalid
	}
	wire, _ := json.Marshal(rv)
	var r v5ReleaseEnvelope
	if json.Unmarshal(wire, &r) != nil {
		return nil, ErrPluginV5ManifestInvalid
	}
	if r.Publisher != m.Publisher || r.Key != m.Key || r.Version != m.Version {
		return nil, ErrPluginV5ManifestRelease
	}
	return m, nil
}

func v5HostCatalogStructure(v any) bool {
	item := func(v any) bool {
		if !v5Object("code scope_kind data_classification consent_required", map[string]v5Check{
			"code": v5Capability, "scope_kind": v5Enum("public", "self", "collection", "system_config"),
			"data_classification":             v5Enum("public", "internal", "sensitive", "restricted"),
			"consent_required":                func(v any) bool { _, ok := v.(bool); return ok },
			"available_public_collection_ids": v5Array(0, 256, true, v5CollectionID), "available_config_keys": v5Array(0, 256, true, v5ConfigName),
		})(v) {
			return false
		}
		m := v.(map[string]any)
		_, collections := m["available_public_collection_ids"]
		_, keys := m["available_config_keys"]
		switch m["scope_kind"] {
		case "public":
			return m["data_classification"] == "public" && m["consent_required"] == false && !collections && !keys
		case "self":
			return m["consent_required"] == true && !collections && !keys
		case "collection":
			return m["data_classification"] == "public" && m["consent_required"] == false && collections && !keys
		case "system_config":
			return m["data_classification"] == "internal" && m["consent_required"] == false && keys && !collections
		}
		return false
	}
	return v5Object("contract host_contract items", map[string]v5Check{"contract": v5Enum("campusos.host-capabilities/v1"), "host_contract": v5HostVersion, "items": v5Array(0, 256, false, item)})(v)
}
func v5ExtensionCatalogStructure(v any) bool {
	return v5Object("contract host_contract points", map[string]v5Check{
		"contract": v5Enum("campusos.extension-catalog/v1"), "host_contract": v5HostVersion,
		"points": v5Array(0, 256, false, v5Object("point contract_version input_schema output_schema capabilities data_modes", map[string]v5Check{
			"point": v5Point, "contract_version": v5Enum("v1"), "input_schema": v5String(1, 128, `^campusos\.[a-z.]+\.request/v1$`),
			"output_schema": v5String(1, 128, `^campusos\.[a-z.]+\.response/v1$`), "capabilities": v5Array(1, 16, true, v5ConfigName), "data_modes": v5Array(1, 3, true, v5Mode),
		})),
	})(v)
}
func v5Contains(set []string, value string) bool {
	for _, s := range set {
		if s == value {
			return true
		}
	}
	return false
}
func v5Subset(values, set []string) bool {
	for _, s := range values {
		if !v5Contains(set, s) {
			return false
		}
	}
	return true
}
