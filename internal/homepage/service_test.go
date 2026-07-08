package homepage

import (
	"archive/zip"
	"bytes"
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

func TestApplyStylePackZipUpdatesHomepageConfig(t *testing.T) {
	p := &plugin.Plugin{
		Status: plugin.StatusRunning,
		Manifest: &plugin.Manifest{
			Name: pluginName,
			Config: map[string]interface{}{
				"hero_title":    "Campus Board",
				"hero_subtitle": "Find useful posts",
			},
		},
	}
	svc := NewService(func(name string) (*plugin.Plugin, bool) {
		return p, name == pluginName
	}, nil)
	svc.SetConfigUpdater(func(name string, config map[string]interface{}) (map[string]interface{}, error) {
		p.Manifest.Config = config
		return config, nil
	})
	data := zipHomepageStylePack(t, map[string]string{
		"style.yaml": `schema_version: page-style-pack.v1
target: homepage
name: home-folder
version: 0.1.0
entry: templates/page.html
styles:
  - styles/theme.css
`,
		"templates/page.html": `<section class="cstyle-page"><h2>Home Folder</h2></section>`,
		"styles/theme.css":    `.cstyle-page { padding: 16px; color: #2563eb; }`,
	})

	cfg, err := svc.ApplyStylePackZip(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("apply homepage style pack: %v", err)
	}
	if !cfg.CustomHTMLEnabled || cfg.CustomHTML == "" || cfg.CustomCSS == "" {
		t.Fatalf("expected custom html/css config, got %#v", cfg)
	}
	if cfg.ActiveStylePack != "home-folder" || cfg.StylePackVersion != "0.1.0" {
		t.Fatalf("unexpected style pack metadata: %#v", cfg)
	}
}

func TestApplySourceStylePackUpdatesHomepageConfig(t *testing.T) {
	t.Chdir("../..")
	p := &plugin.Plugin{
		Status: plugin.StatusRunning,
		Manifest: &plugin.Manifest{
			Name:   pluginName,
			Config: map[string]interface{}{},
		},
	}
	svc := NewService(func(name string) (*plugin.Plugin, bool) {
		return p, name == pluginName
	}, nil)
	svc.SetConfigUpdater(func(name string, config map[string]interface{}) (map[string]interface{}, error) {
		p.Manifest.Config = config
		return config, nil
	})

	cfg, err := svc.ApplySourceStylePack(context.Background(), "campus-hero")
	if err != nil {
		t.Fatalf("apply source homepage style pack: %v", err)
	}
	if !cfg.CustomHTMLEnabled || cfg.CustomHTML == "" || cfg.CustomCSS == "" {
		t.Fatalf("expected custom html/css config, got %#v", cfg)
	}
	if cfg.ActiveStylePack != "campus-hero" || cfg.StylePackVersion != "0.1.0" {
		t.Fatalf("unexpected style pack metadata: %#v", cfg)
	}
}

func zipHomepageStylePack(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}
