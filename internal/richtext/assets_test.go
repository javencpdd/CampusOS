package richtext

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/campusos/CampusOS/internal/space"
)

var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44,
	0xae, 0x42, 0x60, 0x82,
}

func TestAssetStoreConfigUsesPersonalSpaceRoot(t *testing.T) {
	cfg := AssetStoreConfigFromPluginConfig(
		map[string]interface{}{"file_root": "data/images/richtext", "max_image_mb": 2},
		map[string]interface{}{"file_root": "data/personal-space", "default_quota_mb": 12},
	)
	if cfg.RootDir != "data/personal-space" {
		t.Fatalf("asset root = %q, want personal-space root", cfg.RootDir)
	}
	if cfg.QuotaBytes != 12*1024*1024 {
		t.Fatalf("quota = %d, want 12MB", cfg.QuotaBytes)
	}
}

func TestLocalAssetStoreSavesInPersonalSpace(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalAssetStore(AssetStoreConfig{
		RootDir:       root,
		MaxAssetBytes: 1024,
		QuotaBytes:    1024 * 1024,
	})
	if err != nil {
		t.Fatalf("new asset store: %v", err)
	}
	asset, err := store.Save("1001", "article.png", bytes.NewReader(tinyPNG))
	if err != nil {
		t.Fatalf("save asset: %v", err)
	}
	want := filepath.Join(root, "1001", space.PersonalSpaceImageDir, "richtext", asset.FileName)
	if path, err := store.Path("1001", asset.FileName); err != nil || path != want {
		t.Fatalf("asset path = %q, %v; want %q", path, err, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("stored asset not found: %v", err)
	}
	if asset.FileURL == "" || asset.UploaderID != "1001" {
		t.Fatalf("unexpected asset metadata: %#v", asset)
	}
}

func TestLocalAssetStoreEnforcesPersonalSpaceQuota(t *testing.T) {
	store, err := NewLocalAssetStore(AssetStoreConfig{
		RootDir:       t.TempDir(),
		MaxAssetBytes: 1024,
		QuotaBytes:    int64(len(tinyPNG) - 1),
	})
	if err != nil {
		t.Fatalf("new asset store: %v", err)
	}
	_, err = store.Save("1001", "article.png", bytes.NewReader(tinyPNG))
	if !errors.Is(err, ErrAssetQuotaExceeded) {
		t.Fatalf("expected personal-space quota error, got %v", err)
	}
}

func TestMigrateLegacyAssetRoot(t *testing.T) {
	legacyRoot := t.TempDir()
	targetRoot := t.TempDir()
	legacyFile := filepath.Join(legacyRoot, "users", "1001", "article.png")
	if err := os.MkdirAll(filepath.Dir(legacyFile), 0o755); err != nil {
		t.Fatalf("create legacy asset dir: %v", err)
	}
	if err := os.WriteFile(legacyFile, tinyPNG, 0o644); err != nil {
		t.Fatalf("write legacy asset: %v", err)
	}
	if err := migrateLegacyAssetRoot(legacyRoot, targetRoot); err != nil {
		t.Fatalf("migrate legacy asset root: %v", err)
	}
	want := filepath.Join(targetRoot, "1001", space.PersonalSpaceImageDir, "richtext", "article.png")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("migrated asset not found: %v", err)
	}
}
