package feature

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	modulecatalog "github.com/campusos/CampusOS/modules"
	"github.com/gin-gonic/gin"
)

func testFeatureHandler(t *testing.T) (*Handler, *Registry) {
	t.Helper()
	catalog := modulecatalog.MustLoad()
	registry := NewAuthoritativeRegistry(NewMemoryStore())
	for _, descriptor := range catalog.FeatureDescriptors() {
		if err := registry.Register(Definition{
			ID: descriptor.FeatureID, Mode: ActivationMode(descriptor.ActivationMode), Dependencies: descriptor.Dependencies,
			DefaultEnabled: descriptor.DefaultEnabled, DefaultConfig: descriptor.Config,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return NewHandler(registry, catalog), registry
}

func TestHandlerListsBuiltinsWithoutExternalPluginProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := testFeatureHandler(t)
	router := gin.New()
	router.GET("/features", handler.List)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/features", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(recorder.Body.String(), "legacy-builtin") || !strings.Contains(recorder.Body.String(), "personal-schedule") {
		t.Fatalf("unexpected feature response: %#v", body)
	}
}

func TestHandlerUpdatesLegacyAppearanceAliasInFeatureStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, registry := testFeatureHandler(t)
	router := gin.New()
	router.PUT("/features/:id/config", handler.UpdateConfig)
	body := `{"hero_title":"Campus","hero_subtitle":"Community","show_category_tags":true,"category_tag_limit":5,"custom_html_enabled":false}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/features/homepage-customizer/config", strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	root := registry.Config("appearance")
	if root["homepage"].(map[string]interface{})["hero_title"] != "Campus" {
		t.Fatalf("appearance config was not stored: %#v", root)
	}
	if _, ok := root["web_theme"]; !ok {
		t.Fatalf("appearance sibling config was lost: %#v", root)
	}
}
