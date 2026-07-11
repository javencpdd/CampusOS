package campusos

type Event struct {
	Type    string      `json:"type"`
	Source  string      `json:"source"`
	Subject string      `json:"subject"`
	Data    interface{} `json:"data"`
}

type Manifest struct {
	APIVersion     string                 `json:"api_version" yaml:"api_version"`
	HostAPIVersion string                 `json:"host_api_version" yaml:"host_api_version"`
	Name           string                 `json:"name" yaml:"name"`
	DisplayName    string                 `json:"display_name" yaml:"display_name"`
	Version        string                 `json:"version" yaml:"version"`
	Description    string                 `json:"description" yaml:"description"`
	Author         string                 `json:"author" yaml:"author"`
	Runtime        string                 `json:"runtime" yaml:"runtime"`
	Scope          string                 `json:"scope" yaml:"scope"`
	Capabilities   []string               `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Compatibility  CompatibilityConfig    `json:"compatibility,omitempty" yaml:"compatibility,omitempty"`
	Events         EventsConfig           `json:"events" yaml:"events"`
	Permissions    PermissionsConfig      `json:"permissions" yaml:"permissions"`
	Storage        StorageConfig          `json:"storage" yaml:"storage"`
	Config         map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
	ConfigSchema   *ConfigSchema          `json:"config_schema,omitempty" yaml:"config_schema,omitempty"`
}

type CompatibilityConfig struct {
	CampusOS string `json:"campusos,omitempty" yaml:"campusos,omitempty"`
	HostAPI  string `json:"host_api,omitempty" yaml:"host_api,omitempty"`
	SDKGo    string `json:"sdk_go,omitempty" yaml:"sdk_go,omitempty"`
}

type EventsConfig struct {
	Subscribe []string `json:"subscribe" yaml:"subscribe"`
}

type PermissionsConfig struct {
	API []APIPermission `json:"api" yaml:"api"`
}

type APIPermission struct {
	Resource string   `json:"resource" yaml:"resource"`
	Actions  []string `json:"actions" yaml:"actions"`
}

type StorageConfig struct {
	Type   string        `json:"type" yaml:"type"`
	SQLite *SQLiteConfig `json:"sqlite,omitempty" yaml:"sqlite,omitempty"`
}

type SQLiteConfig struct {
	Filename string `json:"filename" yaml:"filename"`
}

type ConfigSchema struct {
	Fields []ConfigField `json:"fields" yaml:"fields"`
}

type ConfigField struct {
	Key         string         `json:"key" yaml:"key"`
	Label       string         `json:"label" yaml:"label"`
	Type        string         `json:"type" yaml:"type"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool           `json:"required,omitempty" yaml:"required,omitempty"`
	Default     interface{}    `json:"default,omitempty" yaml:"default,omitempty"`
	Options     []ConfigOption `json:"options,omitempty" yaml:"options,omitempty"`
}

type ConfigOption struct {
	Label string      `json:"label" yaml:"label"`
	Value interface{} `json:"value" yaml:"value"`
}
