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
	if !cfg.HasStyleSnapshot {
		t.Fatalf("expected homepage style snapshot after applying pack")
	}
}

func TestRollbackStylePackRestoresPreviousHomepageConfig(t *testing.T) {
	p := &plugin.Plugin{
		Status: plugin.StatusRunning,
		Manifest: &plugin.Manifest{
			Name: pluginName,
			Config: map[string]interface{}{
				"hero_title":          "Original",
				"custom_html_enabled": false,
				"active_style_pack":   "",
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
name: rollback-pack
version: 0.1.0
entry: templates/page.html
styles:
  - styles/theme.css
`,
		"templates/page.html": `<section class="cstyle-page"><h2>Changed</h2></section>`,
		"styles/theme.css":    `.cstyle-page { padding: 16px; color: #2563eb; }`,
	})
	if _, err := svc.ApplyStylePackZip(context.Background(), bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("apply homepage style pack: %v", err)
	}

	cfg, err := svc.RollbackStylePack(context.Background())
	if err != nil {
		t.Fatalf("rollback homepage style pack: %v", err)
	}
	if cfg.HeroTitle != "Original" || cfg.CustomHTMLEnabled || cfg.ActiveStylePack != "" {
		t.Fatalf("expected original homepage config after rollback, got %#v", cfg)
	}
	if !cfg.HasStyleSnapshot {
		t.Fatalf("expected rollback to keep a new reverse snapshot")
	}
}

func TestApplySourceStylePackUpdatesHomepageConfig(t *testing.T) {
	t.Chdir("../..")
	t.Setenv("PLUGIN_DATA_DIR", "data/plugin_data")
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

func TestListSourceStylePacksIncludesCampusHero(t *testing.T) {
	t.Chdir("../..")
	t.Setenv("PLUGIN_DATA_DIR", "data/plugin_data")
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

	result, err := svc.ListSourceStylePacks(context.Background())
	if err != nil {
		t.Fatalf("list source homepage style packs: %v", err)
	}
	var found bool
	for _, item := range result.Items {
		if item.Name != "campus-hero" {
			continue
		}
		found = true
		if !item.Validation.Valid {
			t.Fatalf("expected campus-hero to be valid, got %#v", item.Validation.Errors)
		}
		if item.Target != "homepage" {
			t.Fatalf("unexpected campus-hero target: %#v", item)
		}
	}
	if !found {
		t.Fatalf("expected campus-hero in source style pack list, got %#v", result.Items)
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
