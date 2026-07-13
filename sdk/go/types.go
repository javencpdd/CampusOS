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
	Type           string                 `json:"type,omitempty" yaml:"type,omitempty"`
	ManagedData    ManagedDataConfig      `json:"managed_data,omitempty" yaml:"managed_data,omitempty"`
	Files          FileCapability         `json:"files,omitempty" yaml:"files,omitempty"`
	Release        ReleaseConfig          `json:"release,omitempty" yaml:"release,omitempty"`
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
	Responsive      ResponsiveUI   `json:"responsive,omitempty" yaml:"responsive,omitempty"`
	Routes          []UIRoute      `json:"routes,omitempty" yaml:"routes,omitempty"`
	Navigation      []UINavigation `json:"navigation,omitempty" yaml:"navigation,omitempty"`
	Slots           []UISlot       `json:"slots,omitempty" yaml:"slots,omitempty"`
	Surfaces        []UISurface    `json:"surfaces,omitempty" yaml:"surfaces,omitempty"`
	Actions         []UIAction     `json:"actions,omitempty" yaml:"actions,omitempty"`
}
type ResponsiveUI struct {
	SupportedViewports []string `json:"supported_viewports,omitempty" yaml:"supported_viewports,omitempty"`
	MinimumWidth       int      `json:"minimum_width,omitempty" yaml:"minimum_width,omitempty"`
	MobileBehavior     string   `json:"mobile_behavior,omitempty" yaml:"mobile_behavior,omitempty"`
	OverflowPolicy     string   `json:"overflow_policy,omitempty" yaml:"overflow_policy,omitempty"`
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
	API  []APIPermission  `json:"api" yaml:"api"`
	User []UserPermission `json:"user,omitempty" yaml:"user,omitempty"`
}

type APIPermission struct {
	Resource string   `json:"resource" yaml:"resource"`
	Actions  []string `json:"actions" yaml:"actions"`
}
type UserPermission struct {
	Resource  string   `json:"resource" yaml:"resource"`
	Actions   []string `json:"actions" yaml:"actions"`
	Purpose   string   `json:"purpose" yaml:"purpose"`
	Risk      string   `json:"risk,omitempty" yaml:"risk,omitempty"`
	Revocable bool     `json:"revocable" yaml:"revocable"`
}

type StorageConfig struct {
	Type   string        `json:"type" yaml:"type"`
	SQLite *SQLiteConfig `json:"sqlite,omitempty" yaml:"sqlite,omitempty"`
}

type SQLiteConfig struct {
	Filename string `json:"filename" yaml:"filename"`
}
type ManagedDataConfig struct {
	Collections      []DataCollection `json:"collections,omitempty" yaml:"collections,omitempty"`
	DefaultQuotaByte int64            `json:"default_quota_bytes,omitempty" yaml:"default_quota_bytes,omitempty"`
}
type DataCollection struct {
	Name          string      `json:"name" yaml:"name"`
	Owner         string      `json:"owner" yaml:"owner"`
	Fields        []DataField `json:"fields,omitempty" yaml:"fields,omitempty"`
	Searchable    []string    `json:"searchable,omitempty" yaml:"searchable,omitempty"`
	Filterable    []string    `json:"filterable,omitempty" yaml:"filterable,omitempty"`
	MaxRecords    int         `json:"max_records,omitempty" yaml:"max_records,omitempty"`
	MaxRecordByte int64       `json:"max_record_bytes,omitempty" yaml:"max_record_bytes,omitempty"`
}
type DataField struct {
	Name     string `json:"name" yaml:"name"`
	Type     string `json:"type,omitempty" yaml:"type,omitempty"`
	Required bool   `json:"required,omitempty" yaml:"required,omitempty"`
}
type FileCapability struct {
	Enabled      bool     `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	AllowedMIMEs []string `json:"allowed_mimes,omitempty" yaml:"allowed_mimes,omitempty"`
	AllowedExts  []string `json:"allowed_extensions,omitempty" yaml:"allowed_extensions,omitempty"`
	MaxFileBytes int64    `json:"max_file_bytes,omitempty" yaml:"max_file_bytes,omitempty"`
	QuotaBytes   int64    `json:"quota_bytes,omitempty" yaml:"quota_bytes,omitempty"`
	Retention    string   `json:"retention,omitempty" yaml:"retention,omitempty"`
}
type ReleaseConfig struct {
	Channel           string `json:"channel,omitempty" yaml:"channel,omitempty"`
	SigningKeyID      string `json:"signing_key_id,omitempty" yaml:"signing_key_id,omitempty"`
	SignatureRequired bool   `json:"signature_required,omitempty" yaml:"signature_required,omitempty"`
	DataSchemaVersion string `json:"data_schema_version,omitempty" yaml:"data_schema_version,omitempty"`
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
