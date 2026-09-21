// Package v4 defines the source and release contract for self-contained
// CampusOS plugins. It intentionally is not wired into the legacy v1-v3
// package installer: a v4 source tree must pass the new packaging and
// isolation gates before it can become runnable.
package v4

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	APIVersion                  = "campusos.plugin/v4"
	UIContractVersion           = "campusos.ui/v3"
	BridgeContractVersion       = "campusos.bridge/v1"
	maxConfigBytes        int64 = 256 * 1024
)

var (
	pluginKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$`)
	semverPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	idPattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
)

// Manifest is the deliberately small, declarative v4 package entrypoint.
// It contains no executable callbacks, database names, host paths, URLs or
// credentials. The host resolves capabilities and UI origins itself.
type Manifest struct {
	APIVersion    string        `yaml:"api_version" json:"api_version"`
	Key           string        `yaml:"key" json:"key"`
	Version       string        `yaml:"version" json:"version"`
	Publisher     Publisher     `yaml:"publisher" json:"publisher"`
	DisplayName   string        `yaml:"display_name" json:"display_name"`
	Description   string        `yaml:"description" json:"description"`
	Compatibility Compatibility `yaml:"compatibility" json:"compatibility"`
	Backend       Backend       `yaml:"backend" json:"backend"`
	Artifacts     Artifacts     `yaml:"artifacts" json:"artifacts"`
	UI            UI            `yaml:"ui" json:"ui"`
	Previewers    []Previewer   `yaml:"preview_providers,omitempty" json:"preview_providers,omitempty"`
	Capabilities  []Capability  `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Configuration Configuration `yaml:"configuration,omitempty" json:"configuration,omitempty"`
	Data          Data          `yaml:"data,omitempty" json:"data,omitempty"`
}

type Publisher struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
}

type Compatibility struct {
	Host   string `yaml:"host" json:"host"`
	UI     string `yaml:"ui" json:"ui"`
	Bridge string `yaml:"bridge" json:"bridge"`
}

// Backend has no process command. A container or Wasm module is selected by
// a verified release artifact and a host-owned RuntimeAdapter, never by an
// arbitrary command line supplied by a package.
type Backend struct {
	Runtime string `yaml:"runtime" json:"runtime"` // none / wasm / container
}

type Artifacts struct {
	UserUI  *UIArtifact `yaml:"user_ui,omitempty" json:"user_ui,omitempty"`
	AdminUI *UIArtifact `yaml:"admin_ui,omitempty" json:"admin_ui,omitempty"`
}

// Source is present only in a repository source tree. Entry is the relative
// path required in a release package after the builder writes its output.
type UIArtifact struct {
	Source string `yaml:"source" json:"source"`
	Entry  string `yaml:"entry" json:"entry"`
}

type UI struct {
	User  *Audience `yaml:"user,omitempty" json:"user,omitempty"`
	Admin *Audience `yaml:"admin,omitempty" json:"admin,omitempty"`
}

type Audience struct {
	Surfaces   []Surface `yaml:"surfaces" json:"surfaces"`
	Navigation []NavItem `yaml:"navigation,omitempty" json:"navigation,omitempty"`
}

type Surface struct {
	ID            string   `yaml:"id" json:"id"`
	Route         string   `yaml:"route" json:"route"`
	Title         string   `yaml:"title" json:"title"`
	Presentations []string `yaml:"presentations" json:"presentations"`
}

type NavItem struct {
	ID        string `yaml:"id" json:"id"`
	SurfaceID string `yaml:"surface_id" json:"surface_id"`
	Label     string `yaml:"label" json:"label"`
	Order     int    `yaml:"order" json:"order"`
}

// Previewer declares a candidate for host-owned preview selection. It is not
// an access grant: the host will later resolve an eligible provider only after
// the resource ACL and three-layer authorization pass.
type Previewer struct {
	ID            string   `yaml:"id" json:"id"`
	ResourceTypes []string `yaml:"resource_types" json:"resource_types"`
	MIMETypes     []string `yaml:"mime_types" json:"mime_types"`
	SurfaceID     string   `yaml:"surface_id" json:"surface_id"`
}

type Capability struct {
	Code     string `yaml:"code" json:"code"`
	Required bool   `yaml:"required" json:"required"`
	Purpose  string `yaml:"purpose" json:"purpose"`
	Scope    string `yaml:"scope" json:"scope"`
}

// CapabilityDescriptor is the minimal host-owned metadata needed by the v4
// contract. It deliberately avoids importing the legacy plugin package so a
// future Manager can use v4 without an import cycle.
type CapabilityDescriptor struct {
	Code  string
	Scope string
}

// CapabilityResolver maps a requested capability to the platform-owned
// catalog. It must never be supplied by a plugin package.
type CapabilityResolver func(code string) (CapabilityDescriptor, bool)

type Configuration struct {
	System *ConfigDefinition `yaml:"system,omitempty" json:"system,omitempty"`
	User   *ConfigDefinition `yaml:"user,omitempty" json:"user,omitempty"`
}

type ConfigDefinition struct {
	Schema   string `yaml:"schema" json:"schema"`
	Defaults string `yaml:"defaults" json:"defaults"`
	Version  string `yaml:"version" json:"version"`
}

type Data struct {
	UserConfigMaxBytes int64 `yaml:"user_config_max_bytes,omitempty" json:"user_config_max_bytes,omitempty"`
}

func Load(pathname string) (*Manifest, error) {
	data, err := os.ReadFile(pathname)
	if err != nil {
		return nil, fmt.Errorf("read v4 manifest: %w", err)
	}
	manifest := &Manifest{}
	if err := yaml.Unmarshal(data, manifest); err != nil {
		return nil, fmt.Errorf("parse v4 manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (m *Manifest) Validate() error {
	if m == nil {
		return errors.New("v4 manifest is required")
	}
	if m.APIVersion != APIVersion {
		return fmt.Errorf("v4 manifest api_version must be %q", APIVersion)
	}
	if !pluginKeyPattern.MatchString(m.Key) || len(m.Key) > 128 {
		return errors.New("v4 manifest key must be a lower-case namespaced identifier up to 128 characters")
	}
	if !semverPattern.MatchString(m.Version) {
		return errors.New("v4 manifest version must be SemVer")
	}
	if !idPattern.MatchString(m.Publisher.ID) || strings.TrimSpace(m.Publisher.Name) == "" {
		return errors.New("v4 manifest publisher id and name are required")
	}
	if strings.TrimSpace(m.DisplayName) == "" || len([]rune(m.DisplayName)) > 120 || len([]rune(m.Description)) > 2000 {
		return errors.New("v4 manifest display_name is required and display fields are too long")
	}
	if strings.TrimSpace(m.Compatibility.Host) == "" || m.Compatibility.UI != UIContractVersion || m.Compatibility.Bridge != BridgeContractVersion {
		return fmt.Errorf("v4 manifest must declare host compatibility, ui %q and bridge %q", UIContractVersion, BridgeContractVersion)
	}
	if m.Backend.Runtime != "none" && m.Backend.Runtime != "wasm" && m.Backend.Runtime != "container" {
		return errors.New("v4 manifest backend.runtime must be none, wasm or container")
	}
	if (m.UI.User == nil) != (m.Artifacts.UserUI == nil) || (m.UI.Admin == nil) != (m.Artifacts.AdminUI == nil) || (m.UI.User == nil && m.UI.Admin == nil) {
		return errors.New("v4 manifest must pair each UI audience with exactly one artifact and declare at least one audience")
	}
	for _, artifact := range []*UIArtifact{m.Artifacts.UserUI, m.Artifacts.AdminUI} {
		if artifact != nil && (!safeRelativePath(artifact.Source) || !safeRelativePath(artifact.Entry)) {
			return errors.New("v4 manifest artifact source and entry must be safe package-relative paths")
		}
	}
	if err := validateAudience("user", m.UI.User); err != nil {
		return err
	}
	if err := validateAudience("admin", m.UI.Admin); err != nil {
		return err
	}
	if err := validatePreviewers(m.Previewers, m.UI.User); err != nil {
		return err
	}
	seenCapabilities := map[string]bool{}
	for _, capability := range m.Capabilities {
		if strings.TrimSpace(capability.Code) == "" || strings.TrimSpace(capability.Purpose) == "" || capability.Scope != "self" || seenCapabilities[capability.Code] {
			return errors.New("v4 manifest capabilities require unique code, purpose and self scope")
		}
		seenCapabilities[capability.Code] = true
	}
	for _, definition := range []*ConfigDefinition{m.Configuration.System, m.Configuration.User} {
		if definition != nil && (!safeRelativePath(definition.Schema) || !safeRelativePath(definition.Defaults) || strings.TrimSpace(definition.Version) == "") {
			return errors.New("v4 manifest configuration needs safe schema/default paths and a version")
		}
	}
	if m.Data.UserConfigMaxBytes < 0 || m.Data.UserConfigMaxBytes > maxConfigBytes {
		return fmt.Errorf("v4 manifest user_config_max_bytes must be between 0 and %d", maxConfigBytes)
	}
	return nil
}

// ValidateCapabilities applies a host-owned capability catalog to a syntactic
// Manifest. Keeping this separate from Validate lets the v4 package remain
// independent of the legacy Manager while ensuring callers cannot turn an
// arbitrary Manifest string into a usable permission request.
func (m *Manifest) ValidateCapabilities(resolve CapabilityResolver) error {
	if m == nil || resolve == nil {
		return errors.New("v4 manifest and host capability resolver are required")
	}
	for _, capability := range m.Capabilities {
		descriptor, ok := resolve(capability.Code)
		if !ok || descriptor.Code != capability.Code || descriptor.Scope != capability.Scope {
			return fmt.Errorf("v4 manifest capability %q is not permitted by the host catalog", capability.Code)
		}
	}
	return nil
}

func validateAudience(name string, audience *Audience) error {
	if audience == nil {
		return nil
	}
	if len(audience.Surfaces) == 0 {
		return fmt.Errorf("v4 manifest %s UI requires a surface", name)
	}
	surfaces := map[string]bool{}
	for _, surface := range audience.Surfaces {
		if !idPattern.MatchString(surface.ID) || !safeRoute(surface.Route) || strings.TrimSpace(surface.Title) == "" || surfaces[surface.ID] {
			return fmt.Errorf("v4 manifest %s UI surface is invalid or duplicated", name)
		}
		surfaces[surface.ID] = true
		seen := map[string]bool{}
		if len(surface.Presentations) == 0 {
			return fmt.Errorf("v4 manifest %s UI surface %q needs a presentation", name, surface.ID)
		}
		for _, presentation := range surface.Presentations {
			if presentation != "page" && presentation != "modal" && presentation != "drawer" && presentation != "fullscreen" && presentation != "new-tab" || seen[presentation] {
				return fmt.Errorf("v4 manifest %s UI surface %q has an invalid presentation", name, surface.ID)
			}
			seen[presentation] = true
		}
	}
	seen := map[string]bool{}
	for _, item := range audience.Navigation {
		if !idPattern.MatchString(item.ID) || !surfaces[item.SurfaceID] || strings.TrimSpace(item.Label) == "" || seen[item.ID] {
			return fmt.Errorf("v4 manifest %s UI navigation is invalid or duplicated", name)
		}
		seen[item.ID] = true
	}
	return nil
}

func validatePreviewers(previewers []Previewer, user *Audience) error {
	if len(previewers) == 0 {
		return nil
	}
	if user == nil {
		return errors.New("v4 manifest preview providers require a user UI audience")
	}
	surfaces := map[string]bool{}
	for _, surface := range user.Surfaces {
		surfaces[surface.ID] = true
	}
	allowedResources := map[string]bool{
		"article_attachment": true,
		"personal_asset":     true,
		"personal_document":  true,
	}
	seen := map[string]bool{}
	for _, previewer := range previewers {
		if !idPattern.MatchString(previewer.ID) || seen[previewer.ID] || !surfaces[previewer.SurfaceID] || len(previewer.ResourceTypes) == 0 || len(previewer.MIMETypes) == 0 {
			return errors.New("v4 manifest preview provider is invalid or duplicated")
		}
		seen[previewer.ID] = true
		resourceSeen := map[string]bool{}
		for _, resourceType := range previewer.ResourceTypes {
			if !allowedResources[resourceType] || resourceSeen[resourceType] {
				return fmt.Errorf("v4 manifest preview provider %q has an invalid resource type", previewer.ID)
			}
			resourceSeen[resourceType] = true
		}
		mimeSeen := map[string]bool{}
		for _, mimeType := range previewer.MIMETypes {
			if mimeType != "application/pdf" || mimeSeen[mimeType] {
				return fmt.Errorf("v4 manifest preview provider %q has an unsupported MIME type", previewer.ID)
			}
			mimeSeen[mimeType] = true
		}
	}
	return nil
}

func safeRelativePath(value string) bool {
	if value == "" || strings.Contains(value, "\\") || strings.Contains(value, "://") || path.IsAbs(value) {
		return false
	}
	clean := path.Clean(value)
	return clean == value && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func safeRoute(value string) bool {
	return safeRelativePath(value) && !strings.Contains(value, "?") && !strings.Contains(value, "#")
}

// ValidateSourceDirectory validates repository source. It deliberately checks
// source directories and config JSON, not release artifacts: those do not
// exist until a later package build stage.
func ValidateSourceDirectory(root string) (*Manifest, error) {
	manifest, err := Load(filepath.Join(root, "plugin.yaml"))
	if err != nil {
		return nil, err
	}
	for _, filename := range []string{"README.md", "LICENSE"} {
		if err := validateRegularFile(root, filename, 256*1024); err != nil {
			return nil, fmt.Errorf("v4 source package %q: %w", filename, err)
		}
	}
	for _, artifact := range []*UIArtifact{manifest.Artifacts.UserUI, manifest.Artifacts.AdminUI} {
		if artifact == nil {
			continue
		}
		if err := validateDirectory(root, artifact.Source); err != nil {
			return nil, fmt.Errorf("v4 manifest UI source: %w", err)
		}
	}
	for _, definition := range []*ConfigDefinition{manifest.Configuration.System, manifest.Configuration.User} {
		if definition == nil {
			continue
		}
		for _, filename := range []string{definition.Schema, definition.Defaults} {
			if err := validateJSONFile(root, filename); err != nil {
				return nil, fmt.Errorf("v4 manifest configuration %q: %w", filename, err)
			}
		}
	}
	return manifest, nil
}

// ValidateReleaseDirectory is for the future pack/install path. A release may
// contain only immutable artifacts, but must contain every declared entry.
func ValidateReleaseDirectory(root string) (*Manifest, error) {
	manifest, err := Load(filepath.Join(root, "plugin.yaml"))
	if err != nil {
		return nil, err
	}
	for _, filename := range []string{"README.md", "LICENSE"} {
		if err := validateRegularFile(root, filename, 256*1024); err != nil {
			return nil, fmt.Errorf("v4 release package %q: %w", filename, err)
		}
	}
	for _, artifact := range []*UIArtifact{manifest.Artifacts.UserUI, manifest.Artifacts.AdminUI} {
		if artifact == nil {
			continue
		}
		if err := validateRegularFile(root, artifact.Entry, 32*1024*1024); err != nil {
			return nil, fmt.Errorf("v4 release entry %q: %w", artifact.Entry, err)
		}
	}
	for _, definition := range []*ConfigDefinition{manifest.Configuration.System, manifest.Configuration.User} {
		if definition == nil {
			continue
		}
		for _, filename := range []string{definition.Schema, definition.Defaults} {
			if err := validateJSONFile(root, filename); err != nil {
				return nil, fmt.Errorf("v4 release configuration %q: %w", filename, err)
			}
		}
	}
	return manifest, nil
}

func validateDirectory(root, relative string) error {
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("must be a real directory")
	}
	return nil
}

func validateJSONFile(root, relative string) error {
	if err := validateRegularFile(root, relative, maxConfigBytes); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return err
	}
	if !json.Valid(data) {
		return errors.New("must contain valid JSON")
	}
	if strings.HasSuffix(relative, ".schema.json") {
		if err := ValidateConfigSchema(data); err != nil {
			return err
		}
	}
	return nil
}

func validateRegularFile(root, relative string, limit int64) error {
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > limit {
		return errors.New("must be a non-symlink regular file within size limit")
	}
	return nil
}

// FindSourcePackages discovers only explicit v4 source trees. Dot-prefixed
// staging/install directories are excluded and are never executable sources.
func FindSourcePackages(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		result = append(result, filepath.Join(root, entry.Name()))
	}
	sort.Strings(result)
	return result, nil
}
