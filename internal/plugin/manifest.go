package plugin

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	ScopeSystem               = "system"
	ScopeUser                 = "user"
	CurrentManifestAPIVersion = "campusos.plugin/v1"
	CurrentHostAPIVersion     = "v1"
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
	UI             UIContribution     `yaml:"ui,omitempty" json:"ui,omitempty"`

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
	API []APIPermission `yaml:"api" json:"api"`
}

type APIPermission struct {
	Resource string   `yaml:"resource" json:"resource"`
	Actions  []string `yaml:"actions" json:"actions"`
}

type StorageConfig struct {
	Type   string        `yaml:"type" json:"type"` // sqlite / postgresql / none
	SQLite *SQLiteConfig `yaml:"sqlite,omitempty" json:"sqlite,omitempty"`
}

type SQLiteConfig struct {
	Filename string `yaml:"filename" json:"filename"`
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
	if m.APIVersion != CurrentManifestAPIVersion {
		return fmt.Errorf("manifest: unsupported api_version %q (supported: %s)", m.APIVersion, CurrentManifestAPIVersion)
	}
	if m.HostAPIVersion == "" {
		m.HostAPIVersion = CurrentHostAPIVersion
	}
	if m.HostAPIVersion != CurrentHostAPIVersion {
		return fmt.Errorf("manifest: unsupported host_api_version %q (supported: %s)", m.HostAPIVersion, CurrentHostAPIVersion)
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
	return nil
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
