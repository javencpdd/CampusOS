package space

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestValidateStylePackageAcceptsSafeManifest(t *testing.T) {
	result := ValidateStylePackage(validStylePackage())
	if !result.Valid {
		t.Fatalf("expected valid style package, got errors: %#v", result.Errors)
	}
}

func TestValidateStylePackageRejectsUnsafeComponent(t *testing.T) {
	pkg := validStylePackage()
	pkg.Manifest.Components = append(pkg.Manifest.Components, StyleComponent{
		Slot: "main",
		Type: "script",
		Props: map[string]interface{}{
			"onload": "alert(1)",
		},
	})

	result := ValidateStylePackage(pkg)
	if result.Valid {
		t.Fatalf("expected invalid style package")
	}
	if len(result.Errors) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateStylePackageRejectsExternalAndTraversalAssets(t *testing.T) {
	pkg := validStylePackage()
	pkg.Manifest.PreviewImage = "https://example.com/preview.png"
	pkg.Manifest.Assets = append(pkg.Manifest.Assets, StyleAsset{
		Name: "bad-asset",
		Path: "../secret.png",
		Type: "image/png",
	})

	result := ValidateStylePackage(pkg)
	if result.Valid {
		t.Fatalf("expected invalid style package")
	}
	if len(result.Errors) < 2 {
		t.Fatalf("expected external and traversal errors, got %#v", result.Errors)
	}
}

func TestValidateStylePackageRejectsDangerousTokenValue(t *testing.T) {
	pkg := validStylePackage()
	pkg.Manifest.Tokens["color.background"] = `url("javascript:alert(1)")`

	result := ValidateStylePackage(pkg)
	if result.Valid {
		t.Fatalf("expected invalid style package")
	}
}

func TestValidateStylePackageHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	jwtMgr := auth.NewJWTManager(auth.JWTConfig{
		Secret:    "test-secret",
		AccessTTL: time.Hour,
		Issuer:    "campusos-test",
	})
	token, err := jwtMgr.GenerateAccessToken("1001", "alice")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	handler := NewHandler(svc)

	router := gin.New()
	router.POST("/spaces/me/styles/validate", middleware.JWTAuth(jwtMgr), handler.ValidateStylePackage)

	body, err := json.Marshal(validStylePackage())
	if err != nil {
		t.Fatalf("marshal package: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/spaces/me/styles/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload struct {
		Code int                   `json:"code"`
		Data StyleValidationResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 0 || !payload.Data.Valid {
		t.Fatalf("expected valid response, got %#v", payload)
	}
}

func validStylePackage() StylePackage {
	return StylePackage{
		Manifest: StyleManifest{
			SchemaVersion:      StyleSchemaVersion,
			Name:               "clean-blog",
			Version:            "0.1.0",
			Author:             "Alice",
			Description:        "Clean blog style for CampusOS personal spaces.",
			PreviewImage:       "assets/preview.png",
			CompatibleCampusOS: []string{">=0.4.0"},
			Layout:             "blog",
			Components: []StyleComponent{
				{
					Slot: "header",
					Type: "profile-header",
					Props: map[string]interface{}{
						"show_avatar": true,
						"title_size":  "lg",
					},
				},
				{
					Slot: "main",
					Type: "content-list",
					Props: map[string]interface{}{
						"density": "comfortable",
					},
				},
			},
			Tokens: map[string]string{
				"color.primary": "#2563eb",
				"font.body":     "system-ui",
				"radius.card":   "8px",
				"space.section": "24px",
			},
			Assets: []StyleAsset{
				{
					Name: "preview",
					Path: "assets/preview.png",
					Type: "image/png",
				},
			},
		},
	}
}
