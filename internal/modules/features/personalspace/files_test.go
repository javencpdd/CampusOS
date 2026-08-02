package space

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

var tinyPNG = []byte{
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

func TestFileStorageConfigFromPluginConfig(t *testing.T) {
	cfg := FileStorageConfigFromPluginConfig(map[string]interface{}{
		"file_root":           "data/custom-space",
		"file_url_prefix":     "api/v1/custom-space/files",
		"default_quota_mb":    12,
		"avatar_keep_limit":   5,
		"max_avatar_mb":       3,
		"unused_future_field": "ignored",
	})

	if cfg.RootDir != "data/custom-space" {
		t.Fatalf("unexpected root: %q", cfg.RootDir)
	}
	if cfg.URLPrefix != "/api/v1/custom-space/files" {
		t.Fatalf("unexpected url prefix: %q", cfg.URLPrefix)
	}
	if cfg.DefaultQuotaBytes != 12*1024*1024 {
		t.Fatalf("unexpected quota: %d", cfg.DefaultQuotaBytes)
	}
	if cfg.AvatarKeepLimit != 5 {
		t.Fatalf("unexpected avatar keep limit: %d", cfg.AvatarKeepLimit)
	}
	if cfg.MaxAvatarBytes != 3*1024*1024 {
		t.Fatalf("unexpected max avatar bytes: %d", cfg.MaxAvatarBytes)
	}
}

func TestFileStorageConfigMigratesLegacyDefaultRoot(t *testing.T) {
	cfg := FileStorageConfigFromPluginConfig(map[string]interface{}{
		"file_root": "data/images/personal-space",
	})
	if cfg.RootDir != "data/personal-space" {
		t.Fatalf("legacy root was not normalized: %q", cfg.RootDir)
	}
}

func TestLocalFileStoreSaveAvatarKeepsLatestFiles(t *testing.T) {
	optimized, err := corestorage.OptimizeImage(tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewLocalFileStore(FileStorageConfig{
		RootDir:           filepath.Join(t.TempDir(), "space-files"),
		DefaultQuotaBytes: 10 * 1024 * 1024,
		AvatarKeepLimit:   3,
		MaxAvatarBytes:    1024,
	})
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	for i := 0; i < 5; i++ {
		if _, _, err := store.SaveAvatar("1001", "avatar.png", bytes.NewReader(tinyPNG)); err != nil {
			t.Fatalf("save avatar %d: %v", i, err)
		}
		time.Sleep(time.Millisecond)
	}

	avatarDir, err := store.avatarDir("1001")
	if err != nil {
		t.Fatalf("avatar dir: %v", err)
	}
	entries, err := os.ReadDir(avatarDir)
	if err != nil {
		t.Fatalf("read avatar dir: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 avatar source files, got %d", len(entries))
	}
	if want := filepath.Join("space-files", "1001", PersonalSpaceImageDir, "avatars"); !strings.HasSuffix(avatarDir, want) {
		t.Fatalf("unexpected avatar directory: %q", avatarDir)
	}
	userDir, err := store.userDir("1001")
	if err != nil {
		t.Fatalf("user dir: %v", err)
	}
	for _, category := range []string{PersonalSpaceFileDir, PersonalSpaceImageDir, PersonalSpaceExcelDir, PersonalSpaceWordDir, PersonalSpacePDFDir} {
		if _, err := os.Stat(filepath.Join(userDir, category)); err != nil {
			t.Fatalf("expected %s directory: %v", category, err)
		}
	}
	status, err := store.Status("1001")
	if err != nil {
		t.Fatalf("storage status: %v", err)
	}
	if status.QuotaBytes != 10*1024*1024 || status.AvatarKeepLimit != 3 {
		t.Fatalf("unexpected status: %#v", status)
	}
	if status.UsedBytes != int64(len(optimized.Data))*3 {
		t.Fatalf("unexpected used bytes: %d", status.UsedBytes)
	}
}

func TestFileCategoryForName(t *testing.T) {
	cases := map[string]string{
		"avatar.webp": PersonalSpaceImageDir,
		"table.xls":   PersonalSpaceExcelDir,
		"table.xlsx":  PersonalSpaceExcelDir,
		"report.docx": PersonalSpaceWordDir,
		"paper.pdf":   PersonalSpacePDFDir,
		"notes.txt":   PersonalSpaceFileDir,
	}
	for fileName, want := range cases {
		if got := FileCategoryForName(fileName); got != want {
			t.Fatalf("category for %q = %q, want %q", fileName, got, want)
		}
	}
}

func TestMigrateLegacyUserDir(t *testing.T) {
	legacyUserDir := filepath.Join(t.TempDir(), "users", "1001")
	newUserDir := filepath.Join(t.TempDir(), "1001")
	avatarPath := filepath.Join(legacyUserDir, "avatars", "old.png")
	schedulePath := filepath.Join(legacyUserDir, "schedule", "schedule.json")
	if err := os.MkdirAll(filepath.Dir(avatarPath), 0o755); err != nil {
		t.Fatalf("create legacy avatar dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(schedulePath), 0o755); err != nil {
		t.Fatalf("create legacy schedule dir: %v", err)
	}
	if err := os.WriteFile(avatarPath, tinyPNG, 0o644); err != nil {
		t.Fatalf("write legacy avatar: %v", err)
	}
	if err := os.WriteFile(schedulePath, []byte(`{"courses":[]}`), 0o644); err != nil {
		t.Fatalf("write legacy schedule: %v", err)
	}

	if err := migrateLegacyUserDir(legacyUserDir, newUserDir); err != nil {
		t.Fatalf("migrate legacy user dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newUserDir, PersonalSpaceImageDir, "avatars", "old.png")); err != nil {
		t.Fatalf("migrated avatar not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newUserDir, PersonalSpaceFileDir, "schedule", "schedule.json")); err != nil {
		t.Fatalf("migrated schedule not found: %v", err)
	}
}

func TestLocalFileStoreRejectsQuotaExceeded(t *testing.T) {
	optimized, err := corestorage.OptimizeImage(tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewLocalFileStore(FileStorageConfig{
		RootDir:           filepath.Join(t.TempDir(), "space-files"),
		DefaultQuotaBytes: int64(len(optimized.Data)),
		AvatarKeepLimit:   3,
		MaxAvatarBytes:    int64(len(tinyPNG) + 32),
	})
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	if _, _, err := store.SaveAvatar("1001", "avatar.png", bytes.NewReader(tinyPNG)); err != nil {
		t.Fatalf("first avatar should fit quota: %v", err)
	}
	_, _, err = store.SaveAvatar("1001", "avatar.png", bytes.NewReader(tinyPNG))
	if !errors.Is(err, ErrSpaceFileQuotaExceeded) {
		t.Fatalf("expected quota exceeded, got %v", err)
	}
}
