package campusos

type Event struct {
	Type    string      `json:"type"`
	Source  string      `json:"source"`
	Subject string      `json:"subject"`
	Data    interface{} `json:"data"`
}

type Manifest struct {
	Name        string                 `json:"name" yaml:"name"`
	DisplayName string                 `json:"display_name" yaml:"display_name"`
	Version     string                 `json:"version" yaml:"version"`
	Description string                 `json:"description" yaml:"description"`
	Author      string                 `json:"author" yaml:"author"`
	Runtime     string                 `json:"runtime" yaml:"runtime"`
	Events      EventsConfig           `json:"events" yaml:"events"`
	Permissions PermissionsConfig      `json:"permissions" yaml:"permissions"`
	Storage     StorageConfig          `json:"storage" yaml:"storage"`
	Config      map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
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
