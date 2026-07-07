package homepage

import (
	"context"
	"testing"

	communitydomain "github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/plugin"
)

type fakeCategoryRepo struct {
	categories []*communitydomain.Category
}

func (r fakeCategoryRepo) List(context.Context) ([]*communitydomain.Category, error) {
	return r.categories, nil
}

func TestPublicConfigUsesRunningPluginConfig(t *testing.T) {
	p := &plugin.Plugin{
		Status: plugin.StatusRunning,
		Manifest: &plugin.Manifest{
			Name: pluginName,
			Config: map[string]interface{}{
				"hero_title":          "Campus Board",
				"hero_subtitle":       "Find useful campus posts",
				"background_image":    "https://example.test/bg.png",
				"show_category_tags":  true,
				"category_tag_limit":  1,
				"custom_html_enabled": true,
				"custom_html":         `<section><h2>Campus Board</h2></section>`,
			},
		},
	}
	svc := NewService(func(name string) (*plugin.Plugin, bool) {
		return p, name == pluginName
	}, fakeCategoryRepo{categories: []*communitydomain.Category{
		{ID: "2", Name: "Hidden", Slug: "hidden", SortOrder: 2, IsClosed: true},
		{ID: "1", Name: "Campus", Slug: "campus", SortOrder: 1},
	}})

	cfg, err := svc.PublicConfig(context.Background())
	if err != nil {
		t.Fatalf("public config: %v", err)
	}
	if !cfg.Enabled {
		t.Fatalf("expected enabled config")
	}
	if cfg.HeroTitle != "Campus Board" || cfg.BackgroundImage == "" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if len(cfg.CategoryTags) != 1 || cfg.CategoryTags[0].Slug != "campus" {
		t.Fatalf("unexpected category tags: %#v", cfg.CategoryTags)
	}
	if !cfg.CustomHTMLEnabled || cfg.CustomHTML == "" {
		t.Fatalf("expected safe custom html to be exposed, got %#v", cfg)
	}
}

func TestPublicConfigFallsBackWhenPluginStopped(t *testing.T) {
	p := &plugin.Plugin{
		Status: plugin.StatusStopped,
		Manifest: &plugin.Manifest{
			Name: pluginName,
			Config: map[string]interface{}{
				"hero_title": "Should Not Apply",
			},
		},
	}
	svc := NewService(func(name string) (*plugin.Plugin, bool) {
		return p, name == pluginName
	}, nil)

	cfg, err := svc.PublicConfig(context.Background())
	if err != nil {
		t.Fatalf("public config: %v", err)
	}
	if cfg.Enabled {
		t.Fatalf("expected disabled config")
	}
	if cfg.HeroTitle == "Should Not Apply" {
		t.Fatalf("stopped plugin config should not be applied")
	}
}

func TestPublicConfigDropsUnsafeCustomHTML(t *testing.T) {
	p := &plugin.Plugin{
		Status: plugin.StatusRunning,
		Manifest: &plugin.Manifest{
			Name: pluginName,
			Config: map[string]interface{}{
				"custom_html_enabled": true,
				"custom_html":         `<script>alert(1)</script>`,
			},
		},
	}
	svc := NewService(func(name string) (*plugin.Plugin, bool) {
		return p, name == pluginName
	}, nil)

	cfg, err := svc.PublicConfig(context.Background())
	if err != nil {
		t.Fatalf("public config: %v", err)
	}
	if cfg.CustomHTMLEnabled || cfg.CustomHTML != "" {
		t.Fatalf("unsafe custom html should be hidden, got %#v", cfg)
	}
}
