package plugin

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	ScopeSystem = "system"
	ScopeUser   = "user"

	ManifestAPIVersionV1      = "campusos.plugin/v1"
	ManifestAPIVersionV2      = "campusos.plugin/v2"
	CurrentManifestAPIVersion = ManifestAPIVersionV1 // default for legacy packages
	HostAPIVersionV1          = "v1"
	HostAPIVersionV2          = "v2"
	CurrentHostAPIVersion     = HostAPIVersionV1 // default for legacy packages
)

// Manifest 插件清单（plugin.yaml 的结构定义）
type Manifest struct {
	APIVersion     string              `yaml:"api_version,omitempty" json:"api_version"`
	HostAPIVersion string              `yaml:"host_api_version,omitempty" json:"host_api_version"`
	Name           string              `yaml:"name" json:"name"`
	DisplayName    string              `yaml:"display_name" json:"display_name"`
	Version        string              `yaml:"version" json:"version"`
	Description    string              `yaml:"description" json:"description"`
	Author         string              `yaml:"author" json:"author"`
	Runtime        string              `yaml:"runtime" json:"runtime"` // grpc / wasm / builtin
	Scope          string              `yaml:"scope" json:"scope"`     // system / user
	Capabilities   []string            `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Compatibility  CompatibilityConfig `yaml:"compatibility,omitempty" json:"compatibility"`
	Lifecycle      LifecycleConfig     `yaml:"lifecycle,omitempty" json:"lifecycle"`
	UI             UIContribution      `yaml:"ui,omitempty" json:"ui,omitempty"`
	Type           string              `yaml:"type,omitempty" json:"type,omitempty"`
	ManagedData    ManagedDataConfig   `yaml:"managed_data,omitempty" json:"managed_data,omitempty"`
	Files          FileCapability      `yaml:"files,omitempty" json:"files,omitempty"`
	Release        ReleaseConfig       `yaml:"release,omitempty" json:"release,omitempty"`

	// 事件订阅
	Events EventsConfig `yaml:"events" json:"events"`

	// 权限声明
	Permissions PermissionsConfig `yaml:"permissions" json:"permissions"`

	// 存储配置
	Storage StorageConfig `yaml:"storage" json:"storage"`

	// 运行时配置
	Config map[string]interface{} `yaml:"config" json:"config,omitempty"`

	// 配置表单 schema，用于后台或 CLI 渲染可编辑配置项
	ConfigSchema *ConfigSchema `yaml:"config_schema,omitempty" json:"config_schema,omitempty"`
}

const (
	ActivationRestart       = "restart"
	ActivationPluginRestart = "plugin-restart"
	ActivationHot           = "hot"
	CurrentUIContract       = "campusos.ui/v1"
)

type LifecycleConfig struct {
	Backend  BackendLifecycleConfig  `yaml:"backend,omitempty" json:"backend"`
	Frontend FrontendLifecycleConfig `yaml:"frontend,omitempty" json:"frontend"`
}

type BackendLifecycleConfig struct {
	ActivationMode string `yaml:"activation_mode,omitempty" json:"activation_mode"`
}

type FrontendLifecycleConfig struct {
	ActivationMode string `yaml:"activation_mode,omitempty" json:"activation_mode"`
}

type CompatibilityConfig struct {
	CampusOS string `yaml:"campusos,omitempty" json:"campusos,omitempty"`
	HostAPI  string `yaml:"host_api,omitempty" json:"host_api,omitempty"`
	SDKGo    string `yaml:"sdk_go,omitempty" json:"sdk_go,omitempty"`
}

type EventsConfig struct {
	Subscribe []string `yaml:"subscribe" json:"subscribe"`
}

type PermissionsConfig struct {
	API  []APIPermission  `yaml:"api" json:"api"`
	User []UserPermission `yaml:"user,omitempty" json:"user,omitempty"`
}

type APIPermission struct {
	Resource string   `yaml:"resource" json:"resource"`
	Actions  []string `yaml:"actions" json:"actions"`
}

// UserPermission is an end-user consent item. It is distinct from the
// administrator-approved Host API permissions above.
type UserPermission struct {
	Resource  string   `yaml:"resource" json:"resource"`
	Actions   []string `yaml:"actions" json:"actions"`
	Purpose   string   `yaml:"purpose" json:"purpose"`
	Risk      string   `yaml:"risk,omitempty" json:"risk,omitempty"`
	Revocable bool     `yaml:"revocable" json:"revocable"`
}

type StorageConfig struct {
	Type   string        `yaml:"type" json:"type"` // sqlite / postgresql / none
	SQLite *SQLiteConfig `yaml:"sqlite,omitempty" json:"sqlite,omitempty"`
}

type SQLiteConfig struct {
	Filename string `yaml:"filename" json:"filename"`
}

const (
	PluginTypeExternal = "external"
	OwnerSystem        = "system"
	OwnerUser          = "user"
)

// ManagedDataConfig opts a v2 plugin into host-managed structured records.
// The host, rather than the plugin, validates collections, owners, filters,
// quotas and searchable fields.
type ManagedDataConfig struct {
	Collections      []DataCollection `yaml:"collections,omitempty" json:"collections,omitempty"`
	DefaultQuotaByte int64            `yaml:"default_quota_bytes,omitempty" json:"default_quota_bytes,omitempty"`
}

type DataCollection struct {
	Name          string      `yaml:"name" json:"name"`
	Owner         string      `yaml:"owner" json:"owner"`
	Fields        []DataField `yaml:"fields,omitempty" json:"fields,omitempty"`
	Searchable    []string    `yaml:"searchable,omitempty" json:"searchable,omitempty"`
	Filterable    []string    `yaml:"filterable,omitempty" json:"filterable,omitempty"`
	MaxRecords    int         `yaml:"max_records,omitempty" json:"max_records,omitempty"`
	MaxRecordByte int64       `yaml:"max_record_bytes,omitempty" json:"max_record_bytes,omitempty"`
}

type DataField struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type,omitempty" json:"type,omitempty"`
	Required bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

// FileCapability permits controlled access to a user-specific plugin file
// namespace. It deliberately does not expose a filesystem path to plugins.
type FileCapability struct {
	Enabled      bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	AllowedMIMEs []string `yaml:"allowed_mimes,omitempty" json:"allowed_mimes,omitempty"`
	AllowedExts  []string `yaml:"allowed_extensions,omitempty" json:"allowed_extensions,omitempty"`
	MaxFileBytes int64    `yaml:"max_file_bytes,omitempty" json:"max_file_bytes,omitempty"`
	QuotaBytes   int64    `yaml:"quota_bytes,omitempty" json:"quota_bytes,omitempty"`
	Retention    string   `yaml:"retention,omitempty" json:"retention,omitempty"`
}

type ReleaseConfig struct {
	Channel           string `yaml:"channel,omitempty" json:"channel,omitempty"`
	SigningKeyID      string `yaml:"signing_key_id,omitempty" json:"signing_key_id,omitempty"`
	SignatureRequired bool   `yaml:"signature_required,omitempty" json:"signature_required,omitempty"`
	DataSchemaVersion string `yaml:"data_schema_version,omitempty" json:"data_schema_version,omitempty"`
}

type ConfigSchema struct {
	Fields []ConfigField `yaml:"fields" json:"fields"`
}

type ConfigField struct {
	Key         string         `yaml:"key" json:"key"`
	Label       string         `yaml:"label" json:"label"`
	Type        string         `yaml:"type" json:"type"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool           `yaml:"required,omitempty" json:"required,omitempty"`
	Default     interface{}    `yaml:"default,omitempty" json:"default,omitempty"`
	Options     []ConfigOption `yaml:"options,omitempty" json:"options,omitempty"`
}

type ConfigOption struct {
	Label string      `yaml:"label" json:"label"`
	Value interface{} `yaml:"value" json:"value"`
}

// LoadManifest 从文件路径加载 Manifest
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return ParseManifest(data)
}

// ParseManifest 从 YAML 字节解析 Manifest
func ParseManifest(data []byte) (*Manifest, error) {
	m := &Manifest{}
	if err := yaml.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// Validate 验证 Manifest 合法性
func (m *Manifest) Validate() error {
	if m.APIVersion == "" {
		m.APIVersion = CurrentManifestAPIVersion
	}
	if m.APIVersion != ManifestAPIVersionV1 && m.APIVersion != ManifestAPIVersionV2 {
		return fmt.Errorf("manifest: unsupported api_version %q (supported: %s, %s)", m.APIVersion, ManifestAPIVersionV1, ManifestAPIVersionV2)
	}
	if m.HostAPIVersion == "" {
		m.HostAPIVersion = CurrentHostAPIVersion
	}
	if m.HostAPIVersion != HostAPIVersionV1 && m.HostAPIVersion != HostAPIVersionV2 {
		return fmt.Errorf("manifest: unsupported host_api_version %q (supported: %s, %s)", m.HostAPIVersion, HostAPIVersionV1, HostAPIVersionV2)
	}
	if m.Name == "" {
		return fmt.Errorf("manifest: name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("manifest: version is required")
	}
	if m.Runtime == "" {
		m.Runtime = "grpc"
	}
	if m.Runtime != "grpc" && m.Runtime != "wasm" && m.Runtime != "builtin" {
		return fmt.Errorf("manifest: runtime must be 'grpc', 'wasm' or 'builtin', got '%s'", m.Runtime)
	}
	if m.Scope == "" {
		if m.Runtime == "builtin" {
			m.Scope = ScopeSystem
		} else {
			m.Scope = ScopeUser
		}
	}
	if m.Scope != ScopeSystem && m.Scope != ScopeUser {
		return fmt.Errorf("manifest: scope must be 'system' or 'user', got '%s'", m.Scope)
	}
	m.applyLifecycleDefaults()
	if !isBackendActivationMode(m.Lifecycle.Backend.ActivationMode) {
		return fmt.Errorf("manifest: lifecycle.backend.activation_mode must be restart, plugin-restart or hot")
	}
	if m.Lifecycle.Frontend.ActivationMode != ActivationHot {
		return fmt.Errorf("manifest: lifecycle.frontend.activation_mode must be hot")
	}
	seenCapabilities := make(map[string]bool)
	for _, capability := range m.Capabilities {
		if capability == "" {
			return fmt.Errorf("manifest: capability cannot be empty")
		}
		if seenCapabilities[capability] {
			return fmt.Errorf("manifest: capability %q is duplicated", capability)
		}
		seenCapabilities[capability] = true
	}
	if err := m.validateConfigSchema(); err != nil {
		return err
	}
	if err := m.validateUI(); err != nil {
		return err
	}
	if m.APIVersion == ManifestAPIVersionV2 {
		if err := m.validateV2(); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manifest) IsV2() bool { return m != nil && m.APIVersion == ManifestAPIVersionV2 }

func (m *Manifest) Collection(name string) (DataCollection, bool) {
	if m == nil {
		return DataCollection{}, false
	}
	for _, collection := range m.ManagedData.Collections {
		if collection.Name == name {
			return collection, true
		}
	}
	return DataCollection{}, false
}

func (m *Manifest) validateV2() error {
	if m.Type == "" {
		m.Type = PluginTypeExternal
	}
	if m.Type != PluginTypeExternal {
		return fmt.Errorf("manifest: v2 type must be %q", PluginTypeExternal)
	}
	if m.HostAPIVersion != HostAPIVersionV2 {
		return fmt.Errorf("manifest: v2 requires host_api_version %q", HostAPIVersionV2)
	}
	if err := m.validateManagedData(); err != nil {
		return err
	}
	if err := m.validateFiles(); err != nil {
		return err
	}
	return m.validateUserPermissions()
}

func (m *Manifest) validateManagedData() error {
	seen := map[string]bool{}
	for i := range m.ManagedData.Collections {
		collection := &m.ManagedData.Collections[i]
		if collection.Name == "" || !isManifestIdentifier(collection.Name) {
			return fmt.Errorf("manifest: managed_data.collections[%d].name is invalid", i)
		}
		if seen[collection.Name] {
			return fmt.Errorf("manifest: managed data collection %q is duplicated", collection.Name)
		}
		seen[collection.Name] = true
		if collection.Owner != OwnerSystem && collection.Owner != OwnerUser {
			return fmt.Errorf("manifest: managed data collection %q owner must be system or user", collection.Name)
		}
		if collection.MaxRecords < 0 || collection.MaxRecordByte < 0 {
			return fmt.Errorf("manifest: managed data collection %q limits cannot be negative", collection.Name)
		}
		fields := map[string]bool{}
		for index := range collection.Fields {
			field := &collection.Fields[index]
			if field.Name == "" || !isManifestIdentifier(field.Name) || fields[field.Name] {
				return fmt.Errorf("manifest: managed data collection %q has an invalid or duplicate field", collection.Name)
			}
			fields[field.Name] = true
			if field.Type == "" {
				field.Type = "string"
			}
			if field.Type != "string" && field.Type != "number" && field.Type != "boolean" && field.Type != "array" && field.Type != "object" {
				return fmt.Errorf("manifest: managed data collection %q field %q has unsupported type", collection.Name, field.Name)
			}
		}
		for _, field := range append(append([]string{}, collection.Searchable...), collection.Filterable...) {
			if !fields[field] {
				return fmt.Errorf("manifest: managed data collection %q declares undeclared searchable/filterable field %q", collection.Name, field)
			}
		}
	}
	if m.ManagedData.DefaultQuotaByte < 0 {
		return errors.New("manifest: managed_data.default_quota_bytes cannot be negative")
	}
	return nil
}

func (m *Manifest) validateFiles() error {
	files := m.Files
	if !files.Enabled {
		return nil
	}
	if files.MaxFileBytes < 0 || files.QuotaBytes < 0 {
		return errors.New("manifest: file limits cannot be negative")
	}
	if files.Retention != "" && files.Retention != "retained" && files.Retention != "user-deletable" {
		return errors.New("manifest: files.retention must be retained or user-deletable")
	}
	return nil
}

func (m *Manifest) validateUserPermissions() error {
	seen := map[string]bool{}
	allowed := map[string]map[string]bool{
		ManagedDataResource:  {"read": true, "write": true, "delete": true},
		PluginFileResource:   {"read": true, "write": true, "delete": true},
		PluginSearchResource: {"read": true},
	}
	for index, permission := range m.Permissions.User {
		if permission.Resource == "" || len(permission.Actions) == 0 || permission.Purpose == "" {
			return fmt.Errorf("manifest: user permission %d requires resource, actions and purpose", index)
		}
		actions, ok := allowed[permission.Resource]
		if !ok {
			return fmt.Errorf("manifest: user permission %d has unsupported resource %q", index, permission.Resource)
		}
		if !permission.Revocable {
			return fmt.Errorf("manifest: user permission %d must be revocable", index)
		}
		if permission.Risk != "" && permission.Risk != "low" && permission.Risk != "medium" && permission.Risk != "high" {
			return fmt.Errorf("manifest: user permission %d has unsupported risk", index)
		}
		for _, action := range permission.Actions {
			key := permission.Resource + ":" + action
			if !actions[action] || seen[key] {
				return fmt.Errorf("manifest: user permission %q is duplicated or invalid", key)
			}
			seen[key] = true
		}
	}
	return nil
}

func isManifestIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			if index == 0 && (char == '_' || char == '-') {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func (m *Manifest) applyLifecycleDefaults() {
	if m.Lifecycle.Backend.ActivationMode == "" {
		switch {
		case m.Runtime == "wasm":
			m.Lifecycle.Backend.ActivationMode = ActivationHot
		case m.Runtime == "grpc":
			m.Lifecycle.Backend.ActivationMode = ActivationPluginRestart
		case m.Runtime == "builtin" && m.Scope == ScopeSystem:
			m.Lifecycle.Backend.ActivationMode = ActivationRestart
		default:
			m.Lifecycle.Backend.ActivationMode = ActivationHot
		}
	}
	if m.Lifecycle.Frontend.ActivationMode == "" {
		m.Lifecycle.Frontend.ActivationMode = ActivationHot
	}
}

func isBackendActivationMode(value string) bool {
	return value == ActivationRestart || value == ActivationPluginRestart || value == ActivationHot
}

func (m *Manifest) BackendActivationMode() string {
	if m == nil {
		return ActivationRestart
	}
	m.applyLifecycleDefaults()
	return m.Lifecycle.Backend.ActivationMode
}

func (m *Manifest) FrontendActivationMode() string {
	if m == nil {
		return ActivationHot
	}
	m.applyLifecycleDefaults()
	return m.Lifecycle.Frontend.ActivationMode
}

// IsSystemLevel reports the management scope. Lifecycle is controlled separately.
func (m *Manifest) IsSystemLevel() bool {
	return m != nil && m.Scope == ScopeSystem
}

// IsUserLevel reports whether lifecycle changes can be applied at runtime.
func (m *Manifest) IsUserLevel() bool {
	return m != nil && m.Scope == ScopeUser
}

func (m *Manifest) validateConfigSchema() error {
	if m.ConfigSchema == nil {
		return nil
	}
	seen := map[string]bool{}
	for i := range m.ConfigSchema.Fields {
		field := &m.ConfigSchema.Fields[i]
		if field.Key == "" {
			return fmt.Errorf("manifest: config_schema.fields[%d].key is required", i)
		}
		if seen[field.Key] {
			return fmt.Errorf("manifest: config_schema field %q is duplicated", field.Key)
		}
		seen[field.Key] = true
		if field.Type == "" {
			field.Type = "string"
		}
		if !isAllowedConfigFieldType(field.Type) {
			return fmt.Errorf("manifest: config_schema field %q has unsupported type %q", field.Key, field.Type)
		}
		if field.Type == "select" && len(field.Options) == 0 {
			return fmt.Errorf("manifest: config_schema field %q requires options", field.Key)
		}
	}
	return nil
}

func isAllowedConfigFieldType(fieldType string) bool {
	switch fieldType {
	case "string", "text", "number", "boolean", "select", "json":
		return true
	default:
		return false
	}
}

// HasEvent 检查插件是否订阅了指定事件
func (m *Manifest) HasEvent(eventType string) bool {
	for _, e := range m.Events.Subscribe {
		if e == eventType {
			return true
		}
	}
	return false
}

// HasPermission 检查插件是否拥有指定权限
func (m *Manifest) HasPermission(resource, action string) bool {
	for _, p := range m.Permissions.API {
		if p.Resource == resource || p.Resource == "*" {
			for _, a := range p.Actions {
				if a == action || a == "*" {
					return true
				}
			}
		}
	}
	return false
}
