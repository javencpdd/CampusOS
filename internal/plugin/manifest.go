package plugin

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Manifest 插件清单（plugin.yaml 的结构定义）
type Manifest struct {
	Name        string `yaml:"name" json:"name"`
	DisplayName string `yaml:"display_name" json:"display_name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description" json:"description"`
	Author      string `yaml:"author" json:"author"`
	Runtime     string `yaml:"runtime" json:"runtime"` // grpc / wasm

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
	if m.Name == "" {
		return fmt.Errorf("manifest: name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("manifest: version is required")
	}
	if m.Runtime == "" {
		m.Runtime = "grpc"
	}
	if m.Runtime != "grpc" && m.Runtime != "wasm" {
		return fmt.Errorf("manifest: runtime must be 'grpc' or 'wasm', got '%s'", m.Runtime)
	}
	if err := m.validateConfigSchema(); err != nil {
		return err
	}
	return nil
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
