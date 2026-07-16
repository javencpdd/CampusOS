package modules

import "testing"

func TestCatalogSeparatesModulesFromPlugins(t *testing.T) {
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.List()) < 10 {
		t.Fatalf("expected complete built-in catalog, got %d descriptors", len(catalog.List()))
	}
	resolved, ok := catalog.Resolve("personal-schedule")
	if !ok || resolved.Descriptor.ID != "feature.personal-schedule" || resolved.Descriptor.Kind != KindBuiltinFeature {
		t.Fatalf("unexpected schedule descriptor: %#v", resolved)
	}
	appearance, ok := catalog.Resolve("homepage-customizer")
	if !ok || appearance.ConfigSection != "homepage" {
		t.Fatalf("legacy appearance alias was not projected: %#v", appearance)
	}
	for _, name := range []string{"appearance", "personal-schedule", "homepage-customizer", "moderation"} {
		if !catalog.IsReservedExtensionName(name) {
			t.Fatalf("expected %q to be reserved for a compiled module", name)
		}
	}
	if catalog.IsReservedExtensionName("campus-welcome") {
		t.Fatal("external plugin name was reserved")
	}
}

func TestNormalizeConfigPreservesAppearanceSiblingSection(t *testing.T) {
	catalog := MustLoad()
	resolved, _ := catalog.Resolve("homepage-customizer")
	current := DeepCopyConfig(resolved.Descriptor.Config)
	current["homepage"].(map[string]interface{})["last_config_snapshot"] = map[string]interface{}{
		"reason": "before_apply",
		"config": map[string]interface{}{"hero_title": "Previous"},
	}
	root, view, err := NormalizeConfig(resolved, map[string]interface{}{
		"hero_title":          "Campus",
		"hero_subtitle":       "Community",
		"show_category_tags":  true,
		"category_tag_limit":  5,
		"custom_html_enabled": false,
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if view["hero_title"] != "Campus" {
		t.Fatalf("unexpected projected config: %#v", view)
	}
	if _, ok := root["web_theme"].(map[string]interface{}); !ok {
		t.Fatalf("sibling appearance section was lost: %#v", root)
	}
	homepage := root["homepage"].(map[string]interface{})
	if _, ok := homepage["last_config_snapshot"].(map[string]interface{}); !ok {
		t.Fatalf("trusted homepage rollback snapshot was lost: %#v", homepage)
	}
	if _, ok := view["last_config_snapshot"].(map[string]interface{}); !ok {
		t.Fatalf("projected config lost rollback snapshot: %#v", view)
	}
}

func TestNormalizeConfigRejectsUnsafeHomepageHTML(t *testing.T) {
	catalog := MustLoad()
	resolved, _ := catalog.Resolve("homepage-customizer")
	_, _, err := NormalizeConfig(resolved, map[string]interface{}{
		"hero_title": "Campus", "hero_subtitle": "Community", "custom_html": `<script>alert(1)</script>`,
	}, resolved.Descriptor.Config)
	if err == nil {
		t.Fatal("unsafe HTML was accepted")
	}
}
