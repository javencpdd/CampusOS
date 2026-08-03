package homepage

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

var testLogoPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

type fakeCategoryRepo struct {
	categories []*communitydomain.Category
}

func (r fakeCategoryRepo) List(context.Context) ([]*communitydomain.Category, error) {
	return r.categories, nil
}

type fakeConfigSource struct {
	enabled bool
	config  map[string]interface{}
}

func (s *fakeConfigSource) Enabled() bool { return s != nil && s.enabled }
func (s *fakeConfigSource) Config() map[string]interface{} {
	return copyConfig(s.config)
}
func (s *fakeConfigSource) Update(config map[string]interface{}) (map[string]interface{}, error) {
	s.config = copyConfig(config)
	return s.Config(), nil
}

func TestPublicConfigUsesEnabledFeatureConfig(t *testing.T) {
	source := &fakeConfigSource{enabled: true, config: map[string]interface{}{
		"hero_title":          "Campus Board",
		"hero_subtitle":       "Find useful campus posts",
		"background_image":    "https://example.test/bg.png",
		"show_category_tags":  true,
		"category_tag_limit":  1,
		"custom_html_enabled": true,
		"custom_html":         `<section><h2>Campus Board</h2></section>`,
	}}
	svc := NewService(source, fakeCategoryRepo{categories: []*communitydomain.Category{
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

func TestPublicConfigFallsBackWhenFeatureDisabled(t *testing.T) {
	source := &fakeConfigSource{enabled: false, config: map[string]interface{}{
		"hero_title": "Should Not Apply",
	}}
	svc := NewService(source, nil)

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
	source := &fakeConfigSource{enabled: true, config: map[string]interface{}{
		"custom_html_enabled": true,
		"custom_html":         `<script>alert(1)</script>`,
	}}
	svc := NewService(source, nil)

	cfg, err := svc.PublicConfig(context.Background())
	if err != nil {
		t.Fatalf("public config: %v", err)
	}
	if cfg.CustomHTMLEnabled || cfg.CustomHTML != "" {
		t.Fatalf("unsafe custom html should be hidden, got %#v", cfg)
	}
}

func TestLogoCanBeReplacedServedAndReset(t *testing.T) {
	resourceDir := filepath.Join(t.TempDir(), "resources")
	t.Setenv("RESOURCE_DIR", resourceDir)
	if err := os.MkdirAll(filepath.Join(resourceDir, "branding"), 0o755); err != nil {
		t.Fatalf("create branding directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "branding", "default-logo.png"), testLogoPNG, 0o644); err != nil {
		t.Fatalf("write default logo: %v", err)
	}

	source := &fakeConfigSource{enabled: true, config: map[string]interface{}{}}
	svc := NewService(source, nil)
	info, err := svc.SaveLogo(context.Background(), bytes.NewReader(testLogoPNG))
	if err != nil {
		t.Fatalf("save logo: %v", err)
	}
	if !info.Custom || info.SizeBytes <= 0 || info.Width != 1 || info.Height != 1 || info.MaxBytes != DefaultLogoMaxBytes {
		t.Fatalf("unexpected custom logo info: %#v", info)
	}
	asset, err := svc.LogoAsset(context.Background())
	if err != nil {
		t.Fatalf("read custom logo: %v", err)
	}
	if asset.Version == "default" || len(asset.Data) == 0 || asset.MIMEType != "image/png" {
		t.Fatalf("unexpected custom logo asset: %#v", asset)
	}

	reset, err := svc.ResetLogo(context.Background())
	if err != nil {
		t.Fatalf("reset logo: %v", err)
	}
	if reset.Custom || reset.URL != logoURL("default") {
		t.Fatalf("unexpected reset logo info: %#v", reset)
	}
	asset, err = svc.LogoAsset(context.Background())
	if err != nil || asset.Version != "default" || !bytes.Equal(asset.Data, testLogoPNG) {
		t.Fatalf("expected bundled default logo after reset: asset=%#v err=%v", asset, err)
	}
}

func TestApplyStylePackZipUpdatesHomepageConfig(t *testing.T) {
	source := &fakeConfigSource{enabled: true, config: map[string]interface{}{
		"hero_title":    "Campus Board",
		"hero_subtitle": "Find useful posts",
	}}
	svc := NewService(source, nil)
	data := zipHomepageStylePack(t, map[string]string{
		"style.yaml": `schema_version: page-style-pack.v1
target: homepage
name: home-folder
version: 0.1.0
delivery_contract: campusos.appearance-delivery/v1
viewport_support:
  desktop: true
  mobile: true
  mobile_breakpoint: 720px
entry: templates/page.html
styles:
  - styles/theme.css
preview_images:
  desktop: preview-desktop.png
  mobile: preview-mobile.png
`,
		"templates/page.html": `<section class="cstyle-page"><h2>Home Folder</h2></section>`,
		"styles/theme.css":    `.home[data-campusos-home] .cstyle-page { padding: 16px; color: #2563eb; } @media (max-width: 720px) { .home[data-campusos-home] .cstyle-page { padding: 12px; } }`,
		"preview-desktop.png": "desktop preview",
		"preview-mobile.png":  "mobile preview",
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
	source := &fakeConfigSource{enabled: true, config: map[string]interface{}{
		"hero_title":          "Original",
		"custom_html_enabled": false,
		"active_style_pack":   "",
	}}
	svc := NewService(source, nil)
	data := zipHomepageStylePack(t, map[string]string{
		"style.yaml": `schema_version: page-style-pack.v1
target: homepage
name: rollback-pack
version: 0.1.0
delivery_contract: campusos.appearance-delivery/v1
viewport_support:
  desktop: true
  mobile: true
  mobile_breakpoint: 720px
entry: templates/page.html
styles:
  - styles/theme.css
preview_images:
  desktop: preview-desktop.png
  mobile: preview-mobile.png
`,
		"templates/page.html": `<section class="cstyle-page"><h2>Changed</h2></section>`,
		"styles/theme.css":    `.home[data-campusos-home] .cstyle-page { padding: 16px; color: #2563eb; } @media (max-width: 720px) { .home[data-campusos-home] .cstyle-page { padding: 12px; } }`,
		"preview-desktop.png": "desktop preview",
		"preview-mobile.png":  "mobile preview",
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
	t.Setenv("RESOURCE_DIR", "../../../../../data/resources")
	source := &fakeConfigSource{enabled: true, config: map[string]interface{}{}}
	svc := NewService(source, nil)

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
	t.Setenv("RESOURCE_DIR", "../../../../../data/resources")
	source := &fakeConfigSource{enabled: true, config: map[string]interface{}{}}
	svc := NewService(source, nil)

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
