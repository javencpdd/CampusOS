package homepage

import (
	"context"
	"fmt"
	"sort"
	"strings"

	communitydomain "github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/internal/safehtml"
)

const pluginName = "homepage-customizer"

type PluginLookup func(name string) (*plugin.Plugin, bool)

type CategoryRepository interface {
	List(ctx context.Context) ([]*communitydomain.Category, error)
}

type Service struct {
	plugins    PluginLookup
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
}

type CategoryTag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

func NewService(plugins PluginLookup, categories CategoryRepository) *Service {
	return &Service{plugins: plugins, categories: categories}
}

func (s *Service) PublicConfig(ctx context.Context) (*Config, error) {
	cfg := defaultConfig()
	p, ok := s.lookupPlugin()
	if !ok || p.Status != plugin.StatusRunning {
		cfg.Enabled = false
		return cfg, nil
	}

	cfg.Enabled = true
	raw := p.Manifest.Config
	cfg.HeroTitle = stringConfig(raw, "hero_title", cfg.HeroTitle)
	cfg.HeroSubtitle = stringConfig(raw, "hero_subtitle", cfg.HeroSubtitle)
	cfg.BackgroundImage = stringConfig(raw, "background_image", cfg.BackgroundImage)
	cfg.BackgroundOverlay = stringConfig(raw, "background_overlay", cfg.BackgroundOverlay)
	cfg.ShowCategoryTags = boolConfig(raw, "show_category_tags", cfg.ShowCategoryTags)
	cfg.CategoryTagLimit = intConfig(raw, "category_tag_limit", cfg.CategoryTagLimit)
	cfg.CustomHTMLEnabled = boolConfig(raw, "custom_html_enabled", false)
	cfg.CustomHTML = safeCustomHTML(raw, cfg.CustomHTMLEnabled)
	if cfg.CustomHTML == "" {
		cfg.CustomHTMLEnabled = false
	}
	cfg.CategoryTags = s.categoryTags(ctx, cfg.CategoryTagLimit)
	return cfg, nil
}

func (s *Service) lookupPlugin() (*plugin.Plugin, bool) {
	if s.plugins == nil {
		return nil, false
	}
	p, ok := s.plugins(pluginName)
	if !ok || p == nil || p.Manifest == nil {
		return nil, false
	}
	return p, true
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
