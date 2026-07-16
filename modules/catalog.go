// Package modules owns the embedded descriptor catalog for CampusOS core and
// built-in feature modules. External plugin manifests are intentionally not
// part of this package or lifecycle.
package modules

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
	"github.com/campusos/CampusOS/internal/safehtml"
	"gopkg.in/yaml.v3"
)

const (
	SchemaV1           = "campusos.module/v1"
	KindCore           = "core"
	KindBuiltinFeature = "builtin-feature"
	ModeAlwaysOn       = "always-on"
	ModeRestart        = "restart"
	ModeHotGated       = "hot-gated"
)

//go:embed core/*/module.yaml features/*/module.yaml
var descriptorFiles embed.FS

type Descriptor struct {
	Schema               string                   `yaml:"schema" json:"schema"`
	ID                   string                   `yaml:"id" json:"id"`
	FeatureID            string                   `yaml:"feature_id,omitempty" json:"feature_id,omitempty"`
	Name                 string                   `yaml:"name" json:"name"`
	DisplayName          string                   `yaml:"display_name" json:"display_name"`
	Version              string                   `yaml:"version" json:"version"`
	Description          string                   `yaml:"description" json:"description"`
	Kind                 string                   `yaml:"kind" json:"kind"`
	ActivationMode       string                   `yaml:"activation_mode" json:"activation_mode"`
	DefaultEnabled       bool                     `yaml:"default_enabled" json:"default_enabled"`
	Dependencies         []string                 `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	Implementation       string                   `yaml:"implementation" json:"implementation"`
	CompatibilityAliases []CompatibilityAlias     `yaml:"compatibility_aliases,omitempty" json:"compatibility_aliases,omitempty"`
	Config               map[string]interface{}   `yaml:"config,omitempty" json:"config,omitempty"`
	ConfigSchema         *ConfigSchema            `yaml:"config_schema,omitempty" json:"config_schema,omitempty"`
	ConfigSections       map[string]ConfigSection `yaml:"config_sections,omitempty" json:"config_sections,omitempty"`
	UI                   UIContribution           `yaml:"ui,omitempty" json:"ui,omitempty"`
}

type CompatibilityAlias struct {
	Name          string `yaml:"name" json:"name"`
	ConfigSection string `yaml:"config_section,omitempty" json:"config_section,omitempty"`
}

type ConfigSection struct {
	DisplayName  string        `yaml:"display_name" json:"display_name"`
	ConfigSchema *ConfigSchema `yaml:"config_schema,omitempty" json:"config_schema,omitempty"`
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

// UI types deliberately mirror the public campusos.ui/v1 wire contract while
// remaining independent from internal/plugin.
type UIContribution struct {
	ContractVersion string         `yaml:"contract_version,omitempty" json:"contract_version,omitempty"`
	Routes          []UIRoute      `yaml:"routes,omitempty" json:"routes,omitempty"`
	Navigation      []UINavigation `yaml:"navigation,omitempty" json:"navigation,omitempty"`
	Surfaces        []UISurface    `yaml:"surfaces,omitempty" json:"surfaces,omitempty"`
}

type UIRoute struct {
	ID           string `yaml:"id" json:"id"`
	Path         string `yaml:"path" json:"path"`
	SurfaceID    string `yaml:"surface_id" json:"surface_id"`
	Title        string `yaml:"title,omitempty" json:"title,omitempty"`
	RequiresAuth bool   `yaml:"requires_auth,omitempty" json:"requires_auth,omitempty"`
}

type UINavigation struct {
	ID       string `yaml:"id" json:"id"`
	Label    string `yaml:"label" json:"label"`
	RouteID  string `yaml:"route_id" json:"route_id"`
	Location string `yaml:"location,omitempty" json:"location,omitempty"`
	Order    int    `yaml:"order,omitempty" json:"order,omitempty"`
}

type UISurface struct {
	ID         string   `yaml:"id" json:"id"`
	Version    string   `yaml:"version" json:"version"`
	Type       string   `yaml:"type" json:"type"`
	LayoutRole string   `yaml:"layout_role" json:"layout_role"`
	Renderer   string   `yaml:"renderer,omitempty" json:"renderer,omitempty"`
	ModuleID   string   `yaml:"module_id,omitempty" json:"module_id,omitempty"`
	Regions    []string `yaml:"regions,omitempty" json:"regions,omitempty"`
}

type Catalog struct {
	descriptors []Descriptor
	byID        map[string]int
	byFeature   map[string]int
	aliases     map[string]Resolved
}

type Resolved struct {
	Descriptor    Descriptor
	ConfigSection string
	Alias         string
}

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)

func Load() (*Catalog, error) {
	paths, err := fs.Glob(descriptorFiles, "{core,features}/*/module.yaml")
	if err != nil || len(paths) == 0 {
		// embed.FS uses path.Match, which does not support brace expansion.
		paths = nil
		for _, pattern := range []string{"core/*/module.yaml", "features/*/module.yaml"} {
			items, globErr := fs.Glob(descriptorFiles, pattern)
			if globErr != nil {
				return nil, globErr
			}
			paths = append(paths, items...)
		}
	}
	sort.Strings(paths)
	catalog := &Catalog{byID: map[string]int{}, byFeature: map[string]int{}, aliases: map[string]Resolved{}}
	for _, filename := range paths {
		data, readErr := descriptorFiles.ReadFile(filename)
		if readErr != nil {
			return nil, fmt.Errorf("read module descriptor %s: %w", filename, readErr)
		}
		var descriptor Descriptor
		if unmarshalErr := yaml.Unmarshal(data, &descriptor); unmarshalErr != nil {
			return nil, fmt.Errorf("parse module descriptor %s: %w", filename, unmarshalErr)
		}
		if validateErr := validateDescriptor(filename, &descriptor); validateErr != nil {
			return nil, validateErr
		}
		if _, exists := catalog.byID[descriptor.ID]; exists {
			return nil, fmt.Errorf("module descriptor %s duplicates ID %q", filename, descriptor.ID)
		}
		index := len(catalog.descriptors)
		catalog.descriptors = append(catalog.descriptors, descriptor)
		catalog.byID[descriptor.ID] = index
		if descriptor.FeatureID != "" {
			if _, exists := catalog.byFeature[descriptor.FeatureID]; exists {
				return nil, fmt.Errorf("module descriptor %s duplicates feature ID %q", filename, descriptor.FeatureID)
			}
			catalog.byFeature[descriptor.FeatureID] = index
		}
	}
	if err := catalog.indexAliases(); err != nil {
		return nil, err
	}
	if err := catalog.validateDependencies(); err != nil {
		return nil, err
	}
	return catalog, nil
}

func MustLoad() *Catalog {
	catalog, err := Load()
	if err != nil {
		panic(err)
	}
	return catalog
}

func (c *Catalog) List() []Descriptor {
	result := make([]Descriptor, len(c.descriptors))
	copy(result, c.descriptors)
	return result
}

func (c *Catalog) FeatureDescriptors() []Descriptor {
	result := make([]Descriptor, 0)
	for _, descriptor := range c.descriptors {
		if descriptor.FeatureID != "" {
			result = append(result, descriptor)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].FeatureID < result[j].FeatureID })
	return result
}

func (c *Catalog) Resolve(identifier string) (Resolved, bool) {
	identifier = strings.TrimSpace(identifier)
	if value, ok := c.aliases[identifier]; ok {
		return cloneResolved(value), true
	}
	if index, ok := c.byFeature[identifier]; ok {
		return Resolved{Descriptor: cloneDescriptor(c.descriptors[index])}, true
	}
	if index, ok := c.byID[identifier]; ok {
		return Resolved{Descriptor: cloneDescriptor(c.descriptors[index])}, true
	}
	return Resolved{}, false
}

// IsReservedExtensionName reports whether an external plugin name would
// collide with a compiled module, feature ID, or compatibility alias.
func (c *Catalog) IsReservedExtensionName(name string) bool {
	_, ok := c.Resolve(name)
	return ok
}

func (c *Catalog) UIContributions() []Resolved {
	result := make([]Resolved, 0)
	for _, descriptor := range c.descriptors {
		if len(descriptor.UI.Routes)+len(descriptor.UI.Navigation)+len(descriptor.UI.Surfaces) == 0 {
			continue
		}
		result = append(result, Resolved{Descriptor: cloneDescriptor(descriptor)})
	}
	return result
}

func (c *Catalog) indexAliases() error {
	for _, descriptor := range c.descriptors {
		for _, alias := range descriptor.CompatibilityAliases {
			if _, exists := c.aliases[alias.Name]; exists {
				return fmt.Errorf("module alias %q is duplicated", alias.Name)
			}
			if alias.ConfigSection != "" {
				if _, ok := descriptor.ConfigSections[alias.ConfigSection]; !ok {
					return fmt.Errorf("module alias %q references unknown config section %q", alias.Name, alias.ConfigSection)
				}
			}
			c.aliases[alias.Name] = Resolved{Descriptor: descriptor, ConfigSection: alias.ConfigSection, Alias: alias.Name}
		}
	}
	return nil
}

func (c *Catalog) validateDependencies() error {
	for _, descriptor := range c.descriptors {
		for _, dependency := range descriptor.Dependencies {
			if _, ok := c.byID[dependency]; !ok {
				return fmt.Errorf("module %q depends on unknown module %q", descriptor.ID, dependency)
			}
		}
	}
	return nil
}

func validateDescriptor(filename string, descriptor *Descriptor) error {
	fail := func(format string, args ...interface{}) error {
		return fmt.Errorf("module descriptor %s: %s", filename, fmt.Sprintf(format, args...))
	}
	if descriptor.Schema != SchemaV1 {
		return fail("unsupported schema %q", descriptor.Schema)
	}
	if !identifierPattern.MatchString(descriptor.ID) || !identifierPattern.MatchString(descriptor.Name) {
		return fail("invalid id or name")
	}
	if descriptor.FeatureID != "" && !identifierPattern.MatchString(descriptor.FeatureID) {
		return fail("invalid feature_id %q", descriptor.FeatureID)
	}
	if descriptor.Version == "" || descriptor.DisplayName == "" || descriptor.Description == "" {
		return fail("version, display_name, and description are required")
	}
	if descriptor.Kind != KindCore && descriptor.Kind != KindBuiltinFeature {
		return fail("kind must be core or builtin-feature")
	}
	if descriptor.Kind == KindCore && descriptor.ActivationMode != ModeAlwaysOn {
		return fail("core modules must use always-on activation")
	}
	if descriptor.Kind == KindBuiltinFeature && descriptor.ActivationMode != ModeRestart && descriptor.ActivationMode != ModeHotGated {
		return fail("built-in features must use restart or hot-gated activation")
	}
	if descriptor.Implementation == "" || strings.Contains(descriptor.Implementation, "..") || path.IsAbs(descriptor.Implementation) {
		return fail("implementation must be a repository-relative path")
	}
	for _, alias := range descriptor.CompatibilityAliases {
		if !identifierPattern.MatchString(alias.Name) {
			return fail("invalid compatibility alias %q", alias.Name)
		}
	}
	if err := validateUI(descriptor.UI); err != nil {
		return fail("%v", err)
	}
	return nil
}

func validateUI(ui UIContribution) error {
	if len(ui.Routes)+len(ui.Navigation)+len(ui.Surfaces) == 0 {
		return nil
	}
	if ui.ContractVersion != "campusos.ui/v1" {
		return fmt.Errorf("unsupported UI contract %q", ui.ContractVersion)
	}
	surfaces := map[string]bool{}
	routes := map[string]bool{}
	for _, surface := range ui.Surfaces {
		if !identifierPattern.MatchString(surface.ID) || surface.Renderer != "trusted-module" || surface.ModuleID == "" {
			return fmt.Errorf("invalid trusted module UI surface %q", surface.ID)
		}
		surfaces[surface.ID] = true
	}
	for _, route := range ui.Routes {
		if !identifierPattern.MatchString(route.ID) || !strings.HasPrefix(route.Path, "/") || strings.Contains(route.Path, "..") || !surfaces[route.SurfaceID] {
			return fmt.Errorf("invalid module UI route %q", route.ID)
		}
		routes[route.ID] = true
	}
	for _, navigation := range ui.Navigation {
		if !identifierPattern.MatchString(navigation.ID) || !routes[navigation.RouteID] {
			return fmt.Errorf("invalid module UI navigation %q", navigation.ID)
		}
	}
	return nil
}

// ConfigView returns the feature config projected through a compatibility
// alias section when applicable.
func ConfigView(resolved Resolved, root map[string]interface{}) map[string]interface{} {
	root = DeepCopyConfig(root)
	if resolved.ConfigSection == "" {
		return root
	}
	section, _ := root[resolved.ConfigSection].(map[string]interface{})
	return DeepCopyConfig(section)
}

func ConfigSchemaFor(resolved Resolved) *ConfigSchema {
	if resolved.ConfigSection == "" {
		return cloneSchema(resolved.Descriptor.ConfigSchema)
	}
	return cloneSchema(resolved.Descriptor.ConfigSections[resolved.ConfigSection].ConfigSchema)
}

// NormalizeConfig applies the module-owned schema and security checks. It
// returns both the complete feature config and the projected section response.
func NormalizeConfig(resolved Resolved, input, currentRoot map[string]interface{}) (map[string]interface{}, map[string]interface{}, error) {
	schema := ConfigSchemaFor(resolved)
	defaults := ConfigView(resolved, resolved.Descriptor.Config)
	current := ConfigView(resolved, currentRoot)
	fallback := DeepCopyConfig(defaults)
	for key, value := range current {
		fallback[key] = deepCopyValue(value)
	}
	normalized, err := normalizeFields(schema, fallback, input)
	if err != nil {
		return nil, nil, err
	}
	if resolved.Descriptor.FeatureID == "appearance" && resolved.ConfigSection == "homepage" {
		if err := validateHomepageConfig(normalized); err != nil {
			return nil, nil, err
		}
		// The homepage service owns this rollback snapshot. It is intentionally
		// not accepted from an admin request, but a partial config update must
		// not silently discard a snapshot created by style-pack application.
		if snapshot, ok := current["last_config_snapshot"]; ok {
			normalized["last_config_snapshot"] = deepCopyValue(snapshot)
		}
	}
	root := DeepCopyConfig(currentRoot)
	if resolved.ConfigSection == "" {
		root = normalized
	} else {
		root[resolved.ConfigSection] = normalized
	}
	return root, DeepCopyConfig(normalized), nil
}

func normalizeFields(schema *ConfigSchema, defaults, input map[string]interface{}) (map[string]interface{}, error) {
	if schema == nil {
		return DeepCopyConfig(input), nil
	}
	output := make(map[string]interface{}, len(schema.Fields))
	for _, field := range schema.Fields {
		value, ok := input[field.Key]
		if !ok {
			value, ok = defaults[field.Key]
		}
		if !ok {
			value = field.Default
		}
		if value == nil {
			if field.Required {
				return nil, fmt.Errorf("config field %q is required", field.Key)
			}
			continue
		}
		coerced, err := coerceField(field, value)
		if err != nil {
			return nil, err
		}
		output[field.Key] = coerced
	}
	return output, nil
}

func coerceField(field ConfigField, value interface{}) (interface{}, error) {
	switch field.Type {
	case "boolean":
		switch typed := value.(type) {
		case bool:
			return typed, nil
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
			if err == nil {
				return parsed, nil
			}
		}
		return nil, fmt.Errorf("config field %q must be boolean", field.Key)
	case "number":
		switch typed := value.(type) {
		case int, int32, int64, float32, float64:
			return typed, nil
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
			if err == nil {
				return parsed, nil
			}
		}
		return nil, fmt.Errorf("config field %q must be number", field.Key)
	case "select":
		for _, option := range field.Options {
			if fmt.Sprint(option.Value) == fmt.Sprint(value) {
				return option.Value, nil
			}
		}
		return nil, fmt.Errorf("config field %q has invalid option %q", field.Key, value)
	case "string", "text":
		return fmt.Sprint(value), nil
	case "json":
		return value, nil
	default:
		return nil, fmt.Errorf("config field %q has unsupported type %q", field.Key, field.Type)
	}
}

func validateHomepageConfig(config map[string]interface{}) error {
	if value := strings.TrimSpace(fmt.Sprint(config["custom_html"])); value != "" {
		result := safehtml.Validate(value)
		if !result.Valid {
			return fmt.Errorf("config field %q failed safe HTML validation: %s", "custom_html", strings.Join(result.Errors, "; "))
		}
	}
	if value := strings.TrimSpace(fmt.Sprint(config["custom_css"])); value != "" {
		result := stylepack.ValidateCSS(value)
		if result.Valid {
			result = stylepack.ValidateCSSScope(stylepack.TargetHomepage, value)
		}
		if !result.Valid {
			return fmt.Errorf("config field %q failed safe CSS validation: %s", "custom_css", strings.Join(result.Errors, "; "))
		}
	}
	return nil
}

func DeepCopyConfig(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = deepCopyValue(value)
	}
	return output
}

func deepCopyValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return DeepCopyConfig(typed)
	case []interface{}:
		result := make([]interface{}, len(typed))
		for index := range typed {
			result[index] = deepCopyValue(typed[index])
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

func cloneDescriptor(input Descriptor) Descriptor {
	input.Dependencies = append([]string(nil), input.Dependencies...)
	input.CompatibilityAliases = append([]CompatibilityAlias(nil), input.CompatibilityAliases...)
	input.Config = DeepCopyConfig(input.Config)
	input.ConfigSchema = cloneSchema(input.ConfigSchema)
	sections := make(map[string]ConfigSection, len(input.ConfigSections))
	for key, section := range input.ConfigSections {
		section.ConfigSchema = cloneSchema(section.ConfigSchema)
		sections[key] = section
	}
	input.ConfigSections = sections
	input.UI.Routes = append([]UIRoute(nil), input.UI.Routes...)
	input.UI.Navigation = append([]UINavigation(nil), input.UI.Navigation...)
	input.UI.Surfaces = append([]UISurface(nil), input.UI.Surfaces...)
	return input
}

func cloneResolved(input Resolved) Resolved {
	input.Descriptor = cloneDescriptor(input.Descriptor)
	return input
}

func cloneSchema(input *ConfigSchema) *ConfigSchema {
	if input == nil {
		return nil
	}
	result := &ConfigSchema{Fields: make([]ConfigField, len(input.Fields))}
	copy(result.Fields, input.Fields)
	for index := range result.Fields {
		result.Fields[index].Options = append([]ConfigOption(nil), result.Fields[index].Options...)
	}
	return result
}

var ErrNotFound = errors.New("module or feature not found")
