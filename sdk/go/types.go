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
	Lifecycle      LifecycleConfig        `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
	UI             UIContribution         `json:"ui,omitempty" yaml:"ui,omitempty"`
	Events         EventsConfig           `json:"events" yaml:"events"`
	Permissions    PermissionsConfig      `json:"permissions" yaml:"permissions"`
	Storage        StorageConfig          `json:"storage" yaml:"storage"`
	Config         map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
	ConfigSchema   *ConfigSchema          `json:"config_schema,omitempty" yaml:"config_schema,omitempty"`
}

type LifecycleConfig struct {
	Backend  BackendLifecycleConfig  `json:"backend,omitempty" yaml:"backend,omitempty"`
	Frontend FrontendLifecycleConfig `json:"frontend,omitempty" yaml:"frontend,omitempty"`
}
type BackendLifecycleConfig struct {
	ActivationMode string `json:"activation_mode,omitempty" yaml:"activation_mode,omitempty"`
}
type FrontendLifecycleConfig struct {
	ActivationMode string `json:"activation_mode,omitempty" yaml:"activation_mode,omitempty"`
}

type UIContribution struct {
	ContractVersion string         `json:"contract_version,omitempty" yaml:"contract_version,omitempty"`
	Routes          []UIRoute      `json:"routes,omitempty" yaml:"routes,omitempty"`
	Navigation      []UINavigation `json:"navigation,omitempty" yaml:"navigation,omitempty"`
	Slots           []UISlot       `json:"slots,omitempty" yaml:"slots,omitempty"`
	Surfaces        []UISurface    `json:"surfaces,omitempty" yaml:"surfaces,omitempty"`
	Actions         []UIAction     `json:"actions,omitempty" yaml:"actions,omitempty"`
}
type UIRoute struct {
	ID           string `json:"id" yaml:"id"`
	Path         string `json:"path" yaml:"path"`
	SurfaceID    string `json:"surface_id" yaml:"surface_id"`
	Title        string `json:"title,omitempty" yaml:"title,omitempty"`
	RequiresAuth bool   `json:"requires_auth,omitempty" yaml:"requires_auth,omitempty"`
}
type UINavigation struct {
	ID       string `json:"id" yaml:"id"`
	Label    string `json:"label" yaml:"label"`
	RouteID  string `json:"route_id" yaml:"route_id"`
	Location string `json:"location,omitempty" yaml:"location,omitempty"`
	Order    int    `json:"order,omitempty" yaml:"order,omitempty"`
}
type UISlot struct {
	ID        string `json:"id" yaml:"id"`
	Slot      string `json:"slot" yaml:"slot"`
	SurfaceID string `json:"surface_id" yaml:"surface_id"`
	Order     int    `json:"order,omitempty" yaml:"order,omitempty"`
}
type UISurface struct {
	ID           string                 `json:"id" yaml:"id"`
	Version      string                 `json:"version" yaml:"version"`
	Type         string                 `json:"type" yaml:"type"`
	LayoutRole   string                 `json:"layout_role" yaml:"layout_role"`
	Renderer     string                 `json:"renderer,omitempty" yaml:"renderer,omitempty"`
	ModuleID     string                 `json:"module_id,omitempty" yaml:"module_id,omitempty"`
	Schema       map[string]interface{} `json:"schema,omitempty" yaml:"schema,omitempty"`
	DataContract map[string]interface{} `json:"data_contract,omitempty" yaml:"data_contract,omitempty"`
	ActionIDs    []string               `json:"action_ids,omitempty" yaml:"action_ids,omitempty"`
	PublicTokens []string               `json:"public_tokens,omitempty" yaml:"public_tokens,omitempty"`
	Regions      []string               `json:"regions,omitempty" yaml:"regions,omitempty"`
}
type UIAction struct {
	ID         string                 `json:"id" yaml:"id"`
	Label      string                 `json:"label" yaml:"label"`
	Method     string                 `json:"method" yaml:"method"`
	Path       string                 `json:"path" yaml:"path"`
	Permission string                 `json:"permission,omitempty" yaml:"permission,omitempty"`
	Confirm    bool                   `json:"confirm,omitempty" yaml:"confirm,omitempty"`
	Audit      bool                   `json:"audit,omitempty" yaml:"audit,omitempty"`
	Body       map[string]interface{} `json:"body,omitempty" yaml:"body,omitempty"`
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
