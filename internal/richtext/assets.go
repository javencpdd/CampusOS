package richtext

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/core/storage"
)

const (
	defaultAssetRoot      = "data/personal-space"
	legacyAssetRoot       = "data/images/richtext"
	defaultAssetURLPrefix = "/api/v1/richtext/assets"
	defaultMaxAssetBytes  = int64(5 * 1024 * 1024)
	defaultQuotaBytes     = int64(10 * 1024 * 1024)
)

type AssetStoreConfig struct {
	RootDir       string
	URLPrefix     string
	MaxAssetBytes int64
	QuotaBytes    int64
}

type LocalAssetStore struct {
	cfg     AssetStoreConfig
	storage corestorage.Port
}

func DefaultAssetStoreConfig() AssetStoreConfig {
	return AssetStoreConfig{
		RootDir:       defaultAssetRoot,
		URLPrefix:     defaultAssetURLPrefix,
		MaxAssetBytes: defaultMaxAssetBytes,
		QuotaBytes:    defaultQuotaBytes,
	}
}

func AssetStoreConfigFromPluginConfig(raw, personalSpace map[string]interface{}) AssetStoreConfig {
	cfg := DefaultAssetStoreConfig()
	if value := stringConfig(personalSpace, "file_root"); value != "" {
		cfg.RootDir = corestorage.NormalizeRoot(value)
	}
	if value := int64Config(personalSpace, "default_quota_bytes"); value > 0 {
		cfg.QuotaBytes = value
	}
	if value := int64Config(personalSpace, "default_quota_mb"); value > 0 {
		cfg.QuotaBytes = value * 1024 * 1024
	}
	if value := stringConfig(raw, "file_url_prefix"); value != "" {
		cfg.URLPrefix = "/" + strings.Trim(value, "/")
	}
	if value := int64Config(raw, "max_image_bytes"); value > 0 {
		cfg.MaxAssetBytes = value
	}
	if value := int64Config(raw, "max_image_mb"); value > 0 {
		cfg.MaxAssetBytes = value * 1024 * 1024
	}
	return cfg.withDefaults()
}

func NewLocalAssetStore(cfg AssetStoreConfig) (*LocalAssetStore, error) {
	cfg = cfg.withDefaults()
	root, err := filepath.Abs(filepath.Clean(cfg.RootDir))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	cfg.RootDir = root
	if err := MigrateLegacyAssets(root); err != nil {
		return nil, err
	}
	storage, err := corestorage.NewLocalAdapterWithQuota(root, cfg.QuotaBytes)
	if err != nil {
		return nil, err
	}
	return &LocalAssetStore{cfg: cfg, storage: storage}, nil
}

func (cfg AssetStoreConfig) withDefaults() AssetStoreConfig {
	defaults := DefaultAssetStoreConfig()
	if strings.TrimSpace(cfg.RootDir) == "" {
		cfg.RootDir = defaults.RootDir
	}
	cfg.RootDir = corestorage.NormalizeRoot(cfg.RootDir)
	if strings.TrimSpace(cfg.URLPrefix) == "" {
		cfg.URLPrefix = defaults.URLPrefix
	}
	if cfg.MaxAssetBytes <= 0 {
		cfg.MaxAssetBytes = defaults.MaxAssetBytes
	}
	if cfg.QuotaBytes <= 0 {
		cfg.QuotaBytes = defaults.QuotaBytes
	}
	cfg.URLPrefix = "/" + strings.Trim(strings.TrimSpace(cfg.URLPrefix), "/")
	return cfg
}

func (s *LocalAssetStore) Save(userID, originalName string, reader io.Reader) (*Asset, error) {
	if s == nil {
		return nil, ErrAssetUnavailable
	}
	if err := validateStorageSegment(userID); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, s.cfg.MaxAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > s.cfg.MaxAssetBytes {
		return nil, ErrAssetTooLarge
	}
	mimeType := http.DetectContentType(data)
	ext, err := imageExtension(originalName, mimeType)
	if err != nil {
		return nil, err
	}
	width, height := 0, 0
	if mimeType != "image/webp" {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, ErrAssetUnsupported
		}
		width = cfg.Width
		height = cfg.Height
	}
	dir, err := s.assetDir(userID)
	if err != nil {
		return nil, err
	}
	if err := s.checkQuota(userID, int64(len(data))); err != nil {
		return nil, err
	}
	if _, err := s.storage.EnsureLayout(userID); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fileName := fmt.Sprintf("%d%s", time.Now().UTC().UnixNano(), ext)
	if err := validateStorageSegment(fileName); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), data, 0o644); err != nil {
		return nil, err
	}
	return &Asset{
		UploaderID: userID,
		FileURL:    fmt.Sprintf("%s/%s/%s", s.cfg.URLPrefix, userID, fileName),
		FileName:   fileName,
		FileSize:   int64(len(data)),
		MimeType:   mimeType,
		Width:      width,
		Height:     height,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

func (s *LocalAssetStore) Path(userID, fileName string) (string, error) {
	if s == nil {
		return "", ErrAssetUnavailable
	}
	if err := validateStorageSegment(userID); err != nil {
		return "", err
	}
	if err := validateStorageSegment(fileName); err != nil {
		return "", err
	}
	root, err := s.assetDir(userID)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, fileName)
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(os.PathSeparator)) {
		return "", ErrAssetInvalid
	}
	if _, err := os.Stat(targetAbs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrAssetNotFound
		}
		return "", err
	}
	return targetAbs, nil
}

func (s *LocalAssetStore) assetDir(userID string) (string, error) {
	return s.storage.Path(userID, corestorage.ImageDir, "richtext")
}

func (s *LocalAssetStore) checkQuota(userID string, incomingBytes int64) error {
	usage, err := s.storage.Usage(userID)
	if err != nil {
		return err
	}
	if usage+incomingBytes > s.cfg.QuotaBytes {
		return ErrAssetQuotaExceeded
	}
	return nil
}

// MigrateLegacyAssets moves files from the former richtext-only root into
// the shared personal-space image directory. Custom personal-space roots are left untouched.
func MigrateLegacyAssets(targetRoot string) error {
	if !sameStoragePath(targetRoot, defaultAssetRoot) {
		return nil
	}
	legacyRoot, err := filepath.Abs(filepath.Clean(legacyAssetRoot))
	if err != nil {
		return err
	}
	return migrateLegacyAssetRoot(legacyRoot, targetRoot)
}

func migrateLegacyAssetRoot(legacyRoot, targetRoot string) error {
	entries, err := os.ReadDir(filepath.Join(legacyRoot, "users"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, user := range entries {
		if !user.IsDir() || validateStorageSegment(user.Name()) != nil {
			continue
		}
		legacyUserDir := filepath.Join(legacyRoot, "users", user.Name())
		targetDir, err := corestorage.UserPath(targetRoot, user.Name(), corestorage.ImageDir, "richtext")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return err
		}
		files, err := os.ReadDir(legacyUserDir)
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.IsDir() || validateStorageSegment(file.Name()) != nil {
				continue
			}
			if err := moveLegacyAssetFile(filepath.Join(legacyUserDir, file.Name()), filepath.Join(targetDir, file.Name())); err != nil {
				return err
			}
		}
		storage, err := corestorage.NewLocalAdapter(targetRoot)
		if err != nil {
			return err
		}
		if _, err := storage.EnsureLayout(user.Name()); err != nil {
			return err
		}
		_ = os.Remove(legacyUserDir)
	}
	return nil
}

func moveLegacyAssetFile(source, target string) error {
	if _, err := os.Stat(target); err == nil {
		base := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
		ext := filepath.Ext(target)
		target = filepath.Join(filepath.Dir(target), fmt.Sprintf("%s.legacy-%d%s", base, time.Now().UTC().UnixNano(), ext))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	from, err := os.Open(source)
	if err != nil {
		return err
	}
	defer from.Close()
	info, err := from.Stat()
	if err != nil {
		return err
	}
	to, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(to, from); err != nil {
		to.Close()
		return err
	}
	if err := to.Close(); err != nil {
		return err
	}
	return os.Remove(source)
}

func imageExtension(originalName, mimeType string) (string, error) {
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return "", ErrAssetUnsupported
	}
	ext := strings.ToLower(filepath.Ext(originalName))
	if ext == "" {
		extensions, _ := mime.ExtensionsByType(mimeType)
		if len(extensions) > 0 {
			ext = extensions[0]
		}
	}
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return ext, nil
	default:
		return "", ErrAssetUnsupported
	}
}

func validateStorageSegment(value string) error {
	if value == "" || value == "." || value == ".." {
		return ErrAssetInvalid
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return ErrAssetInvalid
	}
	return nil
}

func stringConfig(raw map[string]interface{}, key string) string {
	if raw == nil {
		return ""
	}
	if value, ok := raw[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func int64Config(raw map[string]interface{}, key string) int64 {
	if raw == nil {
		return 0
	}
	switch value := raw[key].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case int32:
		return int64(value)
	case float64:
		return int64(value)
	case float32:
		return int64(value)
	case string:
		var parsed int64
		if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

func sameStoragePath(a, b string) bool {
	aAbs, aErr := filepath.Abs(filepath.Clean(a))
	bAbs, bErr := filepath.Abs(filepath.Clean(b))
	return aErr == nil && bErr == nil && aAbs == bAbs
}
