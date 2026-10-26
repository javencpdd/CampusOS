// v5 wire declarations are distinct from legacy Host API credentials and DTOs.
package campusos

import "encoding/json"

const V5ManifestAPIVersion = "campusos.plugin/v5"

type V5UIArtifact struct {
	Audience   string `json:"audience"`
	Entrypoint string `json:"entrypoint"`
	Digest     string `json:"digest"`
}
type V5BackendArtifact struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Digest string `json:"digest,omitempty"`
	Image  string `json:"image,omitempty"`
}
type V5Backend struct {
	Artifact  V5BackendArtifact `json:"artifact"`
	Transport string            `json:"transport"`
}
type V5ConsumeScope struct {
	Kind          string   `json:"kind"`
	CollectionIDs []string `json:"collection_ids,omitempty"`
	Keys          []string `json:"keys,omitempty"`
}
type V5Consume struct {
	Capability string         `json:"capability"`
	Scope      V5ConsumeScope `json:"scope"`
	Purpose    string         `json:"purpose"`
	Required   bool           `json:"required"`
}
type V5SchemaFile struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}
type V5Provide struct {
	ID              string        `json:"id"`
	Point           string        `json:"point"`
	ContractVersion string        `json:"contract_version"`
	InputSchema     string        `json:"input_schema"`
	OutputSchema    string        `json:"output_schema"`
	Capabilities    []string      `json:"capabilities"`
	DataMode        string        `json:"data_mode"`
	ConfigSchema    *V5SchemaFile `json:"config_schema,omitempty"`
}
type V5ManifestResources struct {
	NetworkTargets []string `json:"network_targets"`
	StorageBytes   int64    `json:"storage_bytes"`
	CPUMillis      int64    `json:"cpu_millis"`
	MemoryMB       int64    `json:"memory_mb"`
	MaxConcurrency int64    `json:"max_concurrency"`
	TimeoutMS      int64    `json:"timeout_ms"`
}
type V5PrivateData struct {
	ConfigScope        string `json:"config_scope"`
	PrivateKVBytes     int64  `json:"private_kv_bytes"`
	PrivateObjectBytes int64  `json:"private_object_bytes"`
	Upgrade            struct {
		SchemaVersion int64  `json:"schema_version"`
		Mode          string `json:"mode"`
		Rollback      string `json:"rollback"`
	} `json:"upgrade"`
}

// V5Manifest is a declaration, never an installation or execution grant.
// ParseV5Manifest is the sole untrusted-wire decoder for this DTO.
type V5Manifest struct {
	APIVersion    string               `json:"api_version"`
	Publisher     string               `json:"publisher"`
	Key           string               `json:"key"`
	Version       string               `json:"version"`
	HostContracts []string             `json:"host_contracts"`
	Runtime       string               `json:"runtime"`
	UI            []V5UIArtifact       `json:"ui,omitempty"`
	Backend       *V5Backend           `json:"backend,omitempty"`
	Provides      []V5Provide          `json:"provides"`
	Consumes      []V5Consume          `json:"consumes"`
	HostResources *V5ManifestResources `json:"host_resources,omitempty"`
	Configuration json.RawMessage      `json:"configuration"`
	Data          V5PrivateData        `json:"data"`
}

// V5ChatRequest/Response describe the published contract. A runtime grant and
// Dispatcher are still required before a host can call a provider.
type V5ChatRequest struct {
	RequestID          string `json:"request_id"`
	ContributionID     string `json:"contribution_id"`
	InstanceGeneration int64  `json:"instance_generation"`
	DeadlineMS         int64  `json:"deadline_ms"`
	Prompt             string `json:"prompt"`
	ContextClass       string `json:"context_class"`
}
type V5ChatResponse struct {
	RequestID          string `json:"request_id"`
	ContributionID     string `json:"contribution_id"`
	InstanceGeneration int64  `json:"instance_generation"`
	Text               string `json:"text"`
	InputTokens        int64  `json:"input_tokens"`
	OutputTokens       int64  `json:"output_tokens"`
	FinishReason       string `json:"finish_reason"`
}
