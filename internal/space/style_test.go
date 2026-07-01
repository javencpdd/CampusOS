package space

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestPreviewStylePackageIncludesCurrentSpace(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	title := "Alice Space"
	if _, err := svc.UpsertOwnSpace(testContext(), "1001", UpsertSpaceRequest{Title: &title}); err != nil {
		t.Fatalf("upsert own space: %v", err)
	}

	preview, err := svc.PreviewStylePackage(testContext(), "1001", validStylePackage())
	if err != nil {
		t.Fatalf("preview style package: %v", err)
	}
	if !preview.Validation.Valid {
		t.Fatalf("expected valid preview, got %#v", preview.Validation.Errors)
	}
	if preview.Owner.Username != "alice" {
		t.Fatalf("expected owner alice, got %q", preview.Owner.Username)
	}
	if preview.Space == nil || preview.Space.Title != "Alice Space" {
		t.Fatalf("expected current space title, got %#v", preview.Space)
	}
	if preview.Manifest == nil || preview.Manifest.Layout != "blog" {
		t.Fatalf("expected normalized manifest, got %#v", preview.Manifest)
	}
	if preview.Layout != "blog" || len(preview.Components) != 2 || len(preview.Tokens) == 0 {
		t.Fatalf("unexpected preview data: %#v", preview)
	}
}

func TestPreviewStylePackageWithInvalidManifestDoesNotExposePreviewManifest(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	pkg := validStylePackage()
	pkg.Manifest.Components[0].Props["onload"] = "alert(1)"

	preview, err := svc.PreviewStylePackage(testContext(), "1001", pkg)
	if err != nil {
		t.Fatalf("preview style package: %v", err)
	}
	if preview.Validation.Valid {
		t.Fatalf("expected invalid preview")
	}
	if preview.Manifest != nil || preview.Layout != "" || len(preview.Components) != 0 {
		t.Fatalf("invalid style package should not expose normalized preview: %#v", preview)
	}
}

func TestPreviewStylePackageHandler(t *testing.T) {
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
	router.POST("/spaces/me/styles/preview", middleware.JWTAuth(jwtMgr), handler.PreviewStylePackage)

	body, err := json.Marshal(validStylePackage())
	if err != nil {
		t.Fatalf("marshal package: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/spaces/me/styles/preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload struct {
		Code int          `json:"code"`
		Data StylePreview `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 0 || !payload.Data.Validation.Valid {
		t.Fatalf("expected valid preview response, got %#v", payload)
	}
	if payload.Data.Owner.ID != "1001" || payload.Data.Manifest == nil {
		t.Fatalf("expected owner and manifest in preview, got %#v", payload.Data)
	}
}

func TestExportStylePackageBuildsValidPackageFromCurrentSpace(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "Alice_Dev",
		Nickname: "Alice",
	}))
	layout := "grid"
	theme := "warm"
	cover := "https://example.com/cover.png"
	if _, err := svc.UpsertOwnSpace(testContext(), "1001", UpsertSpaceRequest{
		Layout:     &layout,
		Theme:      &theme,
		CoverImage: &cover,
		SyncTags:   []string{"go", "campusos"},
	}); err != nil {
		t.Fatalf("upsert own space: %v", err)
	}

	exported, err := svc.ExportStylePackage(testContext(), "1001", StyleExportRequest{})
	if err != nil {
		t.Fatalf("export style package: %v", err)
	}
	if !exported.Validation.Valid {
		t.Fatalf("expected exported package to be valid, got %#v", exported.Validation.Errors)
	}
	manifest := exported.Package.Manifest
	if manifest.Name != "alice-dev-space" {
		t.Fatalf("expected slugged default name, got %q", manifest.Name)
	}
	if manifest.Layout != "grid" {
		t.Fatalf("expected grid layout, got %q", manifest.Layout)
	}
	if manifest.Tokens["color.primary"] != "#b45309" {
		t.Fatalf("expected warm theme primary token, got %#v", manifest.Tokens)
	}
	if len(manifest.Components) < 3 {
		t.Fatalf("expected exported components, got %#v", manifest.Components)
	}
	foundTagCloud := false
	for _, component := range manifest.Components {
		if component.Type == "tag-cloud" {
			foundTagCloud = true
		}
	}
	if !foundTagCloud {
		t.Fatalf("expected tag-cloud when sync tags exist, got %#v", manifest.Components)
	}
}

func TestExportStylePackageRejectsInvalidVersion(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
	}))

	exported, err := svc.ExportStylePackage(testContext(), "1001", StyleExportRequest{
		Version: "next",
	})
	if err == nil {
		t.Fatalf("expected invalid export version to fail")
	}
	if exported == nil || exported.Validation.Valid {
		t.Fatalf("expected invalid validation result, got %#v", exported)
	}
}

func TestExportStylePackageHandler(t *testing.T) {
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
	router.POST("/spaces/me/styles/export", middleware.JWTAuth(jwtMgr), handler.ExportStylePackage)

	reqBody := `{"name":"Alice Clean Blog","version":"0.2.0","description":"Exported test style"}`
	req := httptest.NewRequest(http.MethodPost, "/spaces/me/styles/export", bytes.NewReader([]byte(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload struct {
		Code int               `json:"code"`
		Data StyleExportResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 0 || !payload.Data.Validation.Valid {
		t.Fatalf("expected valid export response, got %#v", payload)
	}
	if payload.Data.Package.Manifest.Name != "alice-clean-blog" || payload.Data.Package.Manifest.Version != "0.2.0" {
		t.Fatalf("unexpected exported manifest: %#v", payload.Data.Package.Manifest)
	}
	if payload.Data.Filename != "alice-clean-blog-0.2.0.space-style.json" {
		t.Fatalf("unexpected filename: %q", payload.Data.Filename)
	}
}

func TestApplyStylePackagePersistsStyleManifest(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	pkg := validStylePackage()
	pkg.Manifest.Name = "timeline-note"
	pkg.Manifest.Version = "0.3.0"
	pkg.Manifest.Layout = "timeline"

	applied, err := svc.ApplyStylePackage(testContext(), "1001", pkg)
	if err != nil {
		t.Fatalf("apply style package: %v", err)
	}
	if !applied.Validation.Valid {
		t.Fatalf("expected valid apply, got %#v", applied.Validation.Errors)
	}
	if applied.Space.Theme != "timeline-note" || applied.Space.Layout != "timeline" {
		t.Fatalf("expected applied theme/layout, got %#v", applied.Space)
	}
	if applied.Space.StyleManifest == nil || applied.Space.StyleManifest.Name != "timeline-note" {
		t.Fatalf("expected persisted style manifest, got %#v", applied.Space.StyleManifest)
	}
	if applied.Space.SyncCategories == nil || applied.Space.SyncTags == nil {
		t.Fatalf("expected empty sync arrays to be initialized, got categories=%#v tags=%#v", applied.Space.SyncCategories, applied.Space.SyncTags)
	}

	own, err := svc.GetOwnSpace(testContext(), "1001")
	if err != nil {
		t.Fatalf("get own space: %v", err)
	}
	if own.Space.StyleName != "timeline-note" || own.Space.StyleVersion != "0.3.0" {
		t.Fatalf("expected persisted style metadata, got %#v", own.Space)
	}

	exported, err := svc.ExportStylePackage(testContext(), "1001", StyleExportRequest{})
	if err != nil {
		t.Fatalf("export applied style package: %v", err)
	}
	if exported.Package.Manifest.Name != "timeline-note" || exported.Package.Manifest.Layout != "timeline" {
		t.Fatalf("expected exported applied manifest, got %#v", exported.Package.Manifest)
	}
}

func TestApplyStylePackageRejectsInvalidManifestWithoutPersisting(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	pkg := validStylePackage()
	pkg.Manifest.Components[0].Props["onclick"] = "alert(1)"

	applied, err := svc.ApplyStylePackage(testContext(), "1001", pkg)
	if err == nil {
		t.Fatalf("expected invalid apply to fail")
	}
	if applied == nil || applied.Validation.Valid {
		t.Fatalf("expected invalid validation result, got %#v", applied)
	}

	own, err := svc.GetOwnSpace(testContext(), "1001")
	if err != nil {
		t.Fatalf("get own space: %v", err)
	}
	if own.Space.StyleManifest != nil || own.Space.StyleName != "" {
		t.Fatalf("invalid style should not be persisted, got %#v", own.Space)
	}
}

func TestUpdateSpaceLayoutClearsAppliedStyleManifest(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	if _, err := svc.ApplyStylePackage(testContext(), "1001", validStylePackage()); err != nil {
		t.Fatalf("apply style package: %v", err)
	}

	layout := "grid"
	updated, err := svc.UpsertOwnSpace(testContext(), "1001", UpsertSpaceRequest{
		Layout: &layout,
	})
	if err != nil {
		t.Fatalf("update own space: %v", err)
	}
	if updated.Space.StyleName != "" || updated.Space.StyleManifest != nil {
		t.Fatalf("manual layout change should clear applied style, got %#v", updated.Space)
	}
}

func TestApplyStylePackageHandler(t *testing.T) {
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
	router.POST("/spaces/me/styles/apply", middleware.JWTAuth(jwtMgr), handler.ApplyStylePackage)

	body, err := json.Marshal(validStylePackage())
	if err != nil {
		t.Fatalf("marshal package: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/spaces/me/styles/apply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload struct {
		Code int              `json:"code"`
		Data StyleApplyResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 0 || !payload.Data.Validation.Valid {
		t.Fatalf("expected valid apply response, got %#v", payload)
	}
	if payload.Data.Space == nil || payload.Data.Space.StyleManifest == nil {
		t.Fatalf("expected applied style space, got %#v", payload.Data.Space)
	}
}

func TestExampleStylePackagesAreValid(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "examples", "space-styles", "*.space-style.json"))
	if err != nil {
		t.Fatalf("glob examples: %v", err)
	}
	if len(files) < 2 || len(files) > 5 {
		t.Fatalf("expected 2 to 5 example style packages, got %d", len(files))
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		var pkg StylePackage
		if err := json.Unmarshal(data, &pkg); err != nil {
			t.Fatalf("decode %s: %v", file, err)
		}
		result := ValidateStylePackage(pkg)
		if !result.Valid {
			t.Fatalf("example %s should be valid, got %#v", file, result.Errors)
		}
	}
}

func testContext() context.Context {
	return context.Background()
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
