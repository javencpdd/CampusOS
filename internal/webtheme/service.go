package webtheme

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/campusos/CampusOS/internal/stylepack"
)

const PluginName = "web-theme"

var (
	ErrDisabled      = errors.New("web-theme plugin is disabled")
	ErrNotFound      = errors.New("web theme not found")
	themeNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
)

// ConfigSource is the Appearance-facing contract for web-theme preferences.
// It intentionally contains no Plugin Manager lifecycle capability.
type ConfigSource interface {
	Enabled() bool
	Config() map[string]interface{}
}

type Service struct {
	config ConfigSource
}

type Catalog struct {
	Enabled          bool          `json:"enabled"`
	AllowUserSwitch  bool          `json:"allow_user_switch"`
	DefaultStylePack string        `json:"default_style_pack,omitempty"`
	Items            []CatalogItem `json:"items"`
}

type CatalogItem struct {
	Name         string            `json:"name"`
	DisplayName  string            `json:"display_name"`
	Version      string            `json:"version"`
	Description  string            `json:"description,omitempty"`
	PreviewURL   string            `json:"preview_url,omitempty"`
	Tokens       map[string]string `json:"tokens,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
}

type RuntimePackage struct {
	Manifest     stylepack.Manifest     `json:"manifest"`
	HTML         string                 `json:"html,omitempty"`
	CSS          string                 `json:"css,omitempty"`
	EffectJS     string                 `json:"effect_js,omitempty"`
	ConfigSchema map[string]interface{} `json:"config_schema,omitempty"`
}

// NewService accepts ConfigSource. compatibility.go continues to accept the
// historical PluginLookup function while callers migrate to Appearance.
func NewService(source interface{}) *Service {
	return &Service{config: configSourceFrom(source)}
}

func (s *Service) Catalog() (*Catalog, error) {
	running := s.enabled()
	catalog := &Catalog{Items: []CatalogItem{}}
	if !running {
		return catalog, nil
	}
	catalog.Enabled = true
	config := s.config.Config()
	catalog.AllowUserSwitch = boolConfig(config, "allow_user_switch", true)
	catalog.DefaultStylePack = stringConfig(config, "default_style_pack", "")

	items, err := stylepack.ListSourcePacks(PluginName)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if !item.Validation.Valid || item.Manifest == nil || item.Target != stylepack.TargetWeb {
			continue
		}
		manifest := item.Manifest
		catalog.Items = append(catalog.Items, CatalogItem{
			Name:         manifest.Name,
			DisplayName:  displayName(*manifest),
			Version:      manifest.Version,
			Description:  manifest.Description,
			PreviewURL:   previewURL(*manifest),
			Tokens:       copyMap(manifest.Tokens),
			Capabilities: append([]string(nil), manifest.Capabilities...),
		})
	}
	if !containsTheme(catalog.Items, catalog.DefaultStylePack) {
		if len(catalog.Items) > 0 {
			catalog.DefaultStylePack = catalog.Items[0].Name
		} else {
			catalog.DefaultStylePack = ""
		}
	}
	return catalog, nil
}

// Theme* methods implement the Appearance application transport contract while
// preserving the historical Service method names for compatibility callers.
func (s *Service) ThemeCatalog() (*Catalog, error) { return s.Catalog() }
func (s *Service) ThemePackage(name string) (*RuntimePackage, error) {
	return s.Package(name)
}
func (s *Service) ThemeAsset(name, assetPath string) ([]byte, string, error) {
	return s.Asset(name, assetPath)
}

func (s *Service) Package(name string) (*RuntimePackage, error) {
	if !s.enabled() {
		return nil, ErrDisabled
	}
	pack, err := s.load(name)
	if err != nil {
		return nil, err
	}
	return &RuntimePackage{
		Manifest:     pack.Manifest,
		HTML:         pack.HTML,
		CSS:          pack.CSS,
		EffectJS:     pack.EffectJS,
		ConfigSchema: pack.ConfigSchema,
	}, nil
}

func (s *Service) Asset(name, assetPath string) ([]byte, string, error) {
	if !s.enabled() {
		return nil, "", ErrDisabled
	}
	pack, err := s.load(name)
	if err != nil {
		return nil, "", err
	}
	assetPath = strings.TrimPrefix(strings.TrimSpace(assetPath), "/")
	if !declaredAsset(pack.Manifest, assetPath) {
		return nil, "", ErrNotFound
	}
	data, ok := pack.RawFiles[assetPath]
	if !ok {
		return nil, "", ErrNotFound
	}
	return []byte(data), imageContentType(assetPath), nil
}

func (s *Service) load(name string) (*stylepack.Package, error) {
	name = strings.TrimSpace(name)
	if !themeNamePattern.MatchString(name) {
		return nil, ErrNotFound
	}
	pack, validation := stylepack.LoadDir(stylepack.SourceDir(PluginName, name))
	if !validation.Valid || pack == nil || pack.Manifest.Name != name || pack.Manifest.Target != stylepack.TargetWeb {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, strings.Join(validation.Errors, "; "))
	}
	return pack, nil
}

func (s *Service) enabled() bool {
	return s.config != nil && s.config.Enabled()
}

func displayName(manifest stylepack.Manifest) string {
	if strings.TrimSpace(manifest.DisplayName) != "" {
		return manifest.DisplayName
	}
	return manifest.Name
}

func previewURL(manifest stylepack.Manifest) string {
	if manifest.PreviewImage == "" {
		return ""
	}
	return "/api/v1/web-themes/" + manifest.Name + "/assets/" + manifest.PreviewImage
}

func declaredAsset(manifest stylepack.Manifest, assetPath string) bool {
	if assetPath == manifest.PreviewImage {
		return true
	}
	for _, asset := range manifest.Assets {
		if asset.Path == assetPath {
			return true
		}
	}
	return false
}

func imageContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func containsTheme(items []CatalogItem, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func stringConfig(config map[string]interface{}, key, fallback string) string {
	if value, ok := config[key]; ok {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
			return text
		}
	}
	return fallback
}

func boolConfig(config map[string]interface{}, key string, fallback bool) bool {
	value, ok := config[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return fallback
	}
}

func copyMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
