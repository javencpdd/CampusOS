package homepage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	communitydomain "github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/safehtml"
	"github.com/campusos/CampusOS/internal/stylepack"
)

const configSnapshotKey = "last_config_snapshot"

// ConfigSource is the Appearance-facing contract for homepage configuration.
// It intentionally exposes no Plugin Manager or plugin lifecycle details.
type ConfigSource interface {
	Enabled() bool
	Config() map[string]interface{}
	Update(map[string]interface{}) (map[string]interface{}, error)
}

var ErrStylePackInvalid = errors.New("homepage style pack invalid")

type CategoryRepository interface {
	List(ctx context.Context) ([]*communitydomain.Category, error)
}

type Service struct {
	config     ConfigSource
	categories CategoryRepository
}

type Config struct {
	Enabled           bool          `json:"enabled"`
	HeroTitle         string        `json:"hero_title"`
	HeroSubtitle      string        `json:"hero_subtitle"`
	BackgroundImage   string        `json:"background_image"`
	BackgroundOverlay string        `json:"background_overlay"`
	ShowCategoryTags  bool          `json:"show_category_tags"`
	CategoryTagLimit  int           `json:"category_tag_limit"`
	CategoryTags      []CategoryTag `json:"category_tags"`
	CustomHTMLEnabled bool          `json:"custom_html_enabled"`
	CustomHTML        string        `json:"custom_html,omitempty"`
	CustomCSS         string        `json:"custom_css,omitempty"`
	ActiveStylePack   string        `json:"active_style_pack,omitempty"`
	StylePackVersion  string        `json:"style_pack_version,omitempty"`
	HasStyleSnapshot  bool          `json:"has_style_snapshot"`
	StyleSnapshotAt   string        `json:"style_snapshot_at,omitempty"`
}

type CategoryTag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

type StylePackResult struct {
	Validation stylepack.ValidationResult `json:"validation"`
	Package    *stylepack.Package         `json:"package,omitempty"`
}

type StylePackApplySourceRequest struct {
	Name string `json:"name" binding:"required"`
}

type ConfigSnapshot struct {
	Reason    string                 `json:"reason"`
	CreatedAt string                 `json:"created_at"`
	Config    map[string]interface{} `json:"config"`
}

// NewService accepts a ConfigSource. The interface parameter preserves the
// historical PluginLookup constructor through compatibility.go while all new
// composition uses the explicit Appearance ConfigSource contract.
func NewService(source interface{}, categories CategoryRepository) *Service {
	return &Service{config: configSourceFrom(source), categories: categories}
}

func (s *Service) PublicConfig(ctx context.Context) (*Config, error) {
	cfg := defaultConfig()
	if s.config == nil || !s.config.Enabled() {
		cfg.Enabled = false
		return cfg, nil
	}

	cfg.Enabled = true
	raw := s.config.Config()
	cfg.HeroTitle = stringConfig(raw, "hero_title", cfg.HeroTitle)
	cfg.HeroSubtitle = stringConfig(raw, "hero_subtitle", cfg.HeroSubtitle)
	cfg.BackgroundImage = stringConfig(raw, "background_image", cfg.BackgroundImage)
	cfg.BackgroundOverlay = stringConfig(raw, "background_overlay", cfg.BackgroundOverlay)
	cfg.ShowCategoryTags = boolConfig(raw, "show_category_tags", cfg.ShowCategoryTags)
	cfg.CategoryTagLimit = intConfig(raw, "category_tag_limit", cfg.CategoryTagLimit)
	cfg.CustomHTMLEnabled = boolConfig(raw, "custom_html_enabled", false)
	cfg.CustomHTML = safeCustomHTML(raw, cfg.CustomHTMLEnabled)
	cfg.CustomCSS = safeCustomCSS(raw, cfg.CustomHTMLEnabled)
	cfg.ActiveStylePack = stringConfig(raw, "active_style_pack", "")
	cfg.StylePackVersion = stringConfig(raw, "style_pack_version", "")
	cfg.HasStyleSnapshot, cfg.StyleSnapshotAt = snapshotStatus(raw)
	if cfg.CustomHTML == "" {
		cfg.CustomHTMLEnabled = false
		cfg.CustomCSS = ""
	}
	cfg.CategoryTags = s.categoryTags(ctx, cfg.CategoryTagLimit)
	return cfg, nil
}

func (s *Service) ValidateStylePackZip(reader io.ReaderAt, size int64) (*StylePackResult, error) {
	pack, validation := stylepack.LoadZip(reader, size)
	if validation.Valid {
		validation = ensureHomepageStylePackTarget(pack)
	}
	return &StylePackResult{Validation: validation, Package: pack}, nil
}

func (s *Service) StylePackExample(ctx context.Context) (*stylepack.FileBundle, error) {
	cfg, err := s.PublicConfig(ctx)
	if err != nil {
		return nil, err
	}
	example := stylepack.BuildExample(
		"homepage",
		"homepage-current-pack",
		"Homepage Current Pack",
		cfg.HeroTitle,
		cfg.HeroSubtitle,
		"#2563eb",
		"#ffffff",
		"#f8fafc",
	)
	return &example, nil
}

func (s *Service) ListSourceStylePacks(ctx context.Context) (*stylepack.SourcePackList, error) {
	if _, err := s.PublicConfig(ctx); err != nil {
		return nil, err
	}
	items, err := stylepack.ListSourcePacks("homepage-customizer")
	if err != nil {
		return nil, err
	}
	for i := range items {
		ensureHomepageSourcePackInfoTarget(&items[i])
	}
	return &stylepack.SourcePackList{Items: items}, nil
}

func (s *Service) ApplyStylePackZip(ctx context.Context, reader io.ReaderAt, size int64) (*Config, error) {
	pack, validation := stylepack.LoadZip(reader, size)
	if validation.Valid {
		validation = ensureHomepageStylePackTarget(pack)
	}
	if !validation.Valid {
		return nil, fmt.Errorf("%w: %s", ErrStylePackInvalid, strings.Join(validation.Errors, "; "))
	}
	return s.applyStylePack(ctx, pack)
}

func (s *Service) ApplySourceStylePack(ctx context.Context, name string) (*Config, error) {
	name = strings.TrimSpace(name)
	if !safeSourceStylePackName(name) {
		return nil, fmt.Errorf("%w: source style pack name must use lowercase letters, numbers and hyphens", ErrStylePackInvalid)
	}
	pack, validation := stylepack.LoadDir(stylepack.SourceDir("homepage-customizer", name))
	if validation.Valid {
		validation = ensureHomepageStylePackTarget(pack)
	}
	if !validation.Valid {
		return nil, fmt.Errorf("%w: %s", ErrStylePackInvalid, strings.Join(validation.Errors, "; "))
	}
	return s.applyStylePack(ctx, pack)
}

func (s *Service) applyStylePack(ctx context.Context, pack *stylepack.Package) (*Config, error) {
	if s.config == nil {
		return nil, fmt.Errorf("homepage config updater is unavailable")
	}
	current := s.config.Config()
	next := copyConfig(current)
	next[configSnapshotKey] = snapshotConfig(current, "before_homepage_style_pack_apply")
	next["custom_html_enabled"] = true
	next["custom_html"] = pack.HTML
	next["custom_css"] = pack.CSS
	next["active_style_pack"] = pack.Manifest.Name
	next["style_pack_version"] = pack.Manifest.Version
	if _, err := s.config.Update(next); err != nil {
		return nil, err
	}
	return s.PublicConfig(ctx)
}

func (s *Service) RollbackStylePack(ctx context.Context) (*Config, error) {
	if s.config == nil {
		return nil, fmt.Errorf("homepage config updater is unavailable")
	}
	current := s.config.Config()
	previous, ok := snapshotConfigValue(current)
	if !ok {
		return nil, fmt.Errorf("%w: homepage style snapshot not found", ErrStylePackInvalid)
	}
	next := copyConfig(previous)
	next[configSnapshotKey] = snapshotConfig(current, "before_homepage_style_pack_rollback")
	if _, err := s.config.Update(next); err != nil {
		return nil, err
	}
	return s.PublicConfig(ctx)
}

func ensureHomepageStylePackTarget(pack *stylepack.Package) stylepack.ValidationResult {
	if pack == nil {
		return stylepack.ValidationResult{Valid: false, Errors: []string{"style pack is empty"}}
	}
	if pack.Manifest.Target != "homepage" {
		return stylepack.ValidationResult{Valid: false, Errors: []string{"style pack target must be homepage"}}
	}
	return stylepack.ValidationResult{Valid: true}
}

func ensureHomepageSourcePackInfoTarget(info *stylepack.SourcePackInfo) {
	if info == nil || !info.Validation.Valid {
		return
	}
	if info.Target == "homepage" {
		return
	}
	info.Validation.Valid = false
	info.Validation.Errors = []string{"style pack target must be homepage"}
	if info.Validation.Warnings == nil {
		info.Validation.Warnings = []string{}
	}
}

func copyConfig(input map[string]interface{}) map[string]interface{} {
	copied := make(map[string]interface{}, len(input))
	for key, value := range input {
		copied[key] = value
	}
	return copied
}

func snapshotConfig(input map[string]interface{}, reason string) ConfigSnapshot {
	copied := copyConfig(input)
	delete(copied, configSnapshotKey)
	return ConfigSnapshot{
		Reason:    reason,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Config:    copied,
	}
}

func snapshotConfigValue(input map[string]interface{}) (map[string]interface{}, bool) {
	raw, ok := input[configSnapshotKey]
	if !ok {
		return nil, false
	}
	switch snapshot := raw.(type) {
	case ConfigSnapshot:
		if len(snapshot.Config) == 0 {
			return nil, false
		}
		return snapshot.Config, true
	case map[string]interface{}:
		config, ok := snapshot["config"].(map[string]interface{})
		if !ok || len(config) == 0 {
			return nil, false
		}
		return config, true
	default:
		return nil, false
	}
}

func snapshotStatus(input map[string]interface{}) (bool, string) {
	raw, ok := input[configSnapshotKey]
	if !ok {
		return false, ""
	}
	switch snapshot := raw.(type) {
	case ConfigSnapshot:
		return len(snapshot.Config) > 0, snapshot.CreatedAt
	case map[string]interface{}:
		if _, ok := snapshot["config"].(map[string]interface{}); !ok {
			return false, ""
		}
		return true, strings.TrimSpace(fmt.Sprint(snapshot["created_at"]))
	default:
		return false, ""
	}
}

func safeSourceStylePackName(name string) bool {
	if len(name) < 2 || len(name) > 63 {
		return false
	}
	for i, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		if r == '-' && i > 0 && i < len(name)-1 {
			continue
		}
		return false
	}
	return true
}

func (s *Service) categoryTags(ctx context.Context, limit int) []CategoryTag {
	if s.categories == nil || limit == 0 {
		return []CategoryTag{}
	}
	categories, err := s.categories.List(ctx)
	if err != nil {
		return []CategoryTag{}
	}
	sort.SliceStable(categories, func(i, j int) bool {
		if categories[i].SortOrder == categories[j].SortOrder {
			return categories[i].CreatedAt.Before(categories[j].CreatedAt)
		}
		return categories[i].SortOrder < categories[j].SortOrder
	})
	tags := make([]CategoryTag, 0, len(categories))
	for _, category := range categories {
		if category == nil || category.IsClosed {
			continue
		}
		tags = append(tags, CategoryTag{
			ID:   category.ID,
			Name: category.Name,
			Slug: category.Slug,
			Icon: category.Icon,
		})
		if limit > 0 && len(tags) >= limit {
			break
		}
	}
	return tags
}

func defaultConfig() *Config {
	return &Config{
		Enabled:           false,
		HeroTitle:         "欢迎来到 CampusOS",
		HeroSubtitle:      "下一代校园社区引擎 - 事件驱动、AI Native 的社区操作系统",
		BackgroundImage:   "",
		BackgroundOverlay: "rgba(15, 23, 42, 0.45)",
		ShowCategoryTags:  true,
		CategoryTagLimit:  8,
		CategoryTags:      []CategoryTag{},
		CustomHTMLEnabled: false,
		CustomHTML:        "",
		CustomCSS:         "",
		ActiveStylePack:   "",
		StylePackVersion:  "",
	}
}

func safeCustomHTML(raw map[string]interface{}, enabled bool) string {
	if !enabled {
		return ""
	}
	html, ok := raw["custom_html"]
	if !ok {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(html))
	if text == "" {
		return ""
	}
	if result := safehtml.Validate(text); !result.Valid {
		return ""
	}
	return text
}

func safeCustomCSS(raw map[string]interface{}, enabled bool) string {
	if !enabled {
		return ""
	}
	css, ok := raw["custom_css"]
	if !ok {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(css))
	if text == "" {
		return ""
	}
	if result := stylepack.ValidateCSS(text); !result.Valid {
		return ""
	}
	if result := stylepack.ValidateCSSScope(stylepack.TargetHomepage, text); !result.Valid {
		return ""
	}
	return text
}

func stringConfig(raw map[string]interface{}, key, fallback string) string {
	value, ok := raw[key]
	if !ok {
		return fallback
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return fallback
	}
	return text
}

func boolConfig(raw map[string]interface{}, key string, fallback bool) bool {
	value, ok := raw[key]
	if !ok {
		return fallback
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return fallback
}

func intConfig(raw map[string]interface{}, key string, fallback int) int {
	value, ok := raw[key]
	if !ok {
		return fallback
	}
	var parsed int
	switch v := value.(type) {
	case int:
		parsed = v
	case int64:
		parsed = int(v)
	case float64:
		parsed = int(v)
	case float32:
		parsed = int(v)
	case string:
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &parsed); err != nil {
			return fallback
		}
	default:
		return fallback
	}
	if parsed < 0 {
		return fallback
	}
	if parsed > 24 {
		return 24
	}
	return parsed
}
