package space

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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
		"file_root":           "data/images/custom-space",
		"file_url_prefix":     "api/v1/custom-space/files",
		"default_quota_mb":    12,
		"avatar_keep_limit":   5,
		"max_avatar_mb":       3,
		"unused_future_field": "ignored",
	})

	if cfg.RootDir != "data/images/custom-space" {
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

func TestLocalFileStoreSaveAvatarKeepsLatestFiles(t *testing.T) {
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
	status, err := store.Status("1001")
	if err != nil {
		t.Fatalf("storage status: %v", err)
	}
	if status.QuotaBytes != 10*1024*1024 || status.AvatarKeepLimit != 3 {
		t.Fatalf("unexpected status: %#v", status)
	}
	if status.UsedBytes != int64(len(tinyPNG))*3 {
		t.Fatalf("unexpected used bytes: %d", status.UsedBytes)
	}
}

func TestLocalFileStoreRejectsQuotaExceeded(t *testing.T) {
	store, err := NewLocalFileStore(FileStorageConfig{
		RootDir:           filepath.Join(t.TempDir(), "space-files"),
		DefaultQuotaBytes: int64(len(tinyPNG)),
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
