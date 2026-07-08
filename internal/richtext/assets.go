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
)

const (
	defaultAssetRoot      = "data/images/richtext"
	defaultAssetURLPrefix = "/api/v1/richtext/assets"
	defaultMaxAssetBytes  = int64(5 * 1024 * 1024)
)

type AssetStoreConfig struct {
	RootDir       string
	URLPrefix     string
	MaxAssetBytes int64
}

type LocalAssetStore struct {
	cfg AssetStoreConfig
}

func DefaultAssetStoreConfig() AssetStoreConfig {
	return AssetStoreConfig{
		RootDir:       defaultAssetRoot,
		URLPrefix:     defaultAssetURLPrefix,
		MaxAssetBytes: defaultMaxAssetBytes,
	}
}

func AssetStoreConfigFromPluginConfig(raw map[string]interface{}) AssetStoreConfig {
	cfg := DefaultAssetStoreConfig()
	if value := stringConfig(raw, "file_root"); value != "" {
		cfg.RootDir = value
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
	return &LocalAssetStore{cfg: cfg}, nil
}

func (cfg AssetStoreConfig) withDefaults() AssetStoreConfig {
	defaults := DefaultAssetStoreConfig()
	if strings.TrimSpace(cfg.RootDir) == "" {
		cfg.RootDir = defaults.RootDir
	}
	if strings.TrimSpace(cfg.URLPrefix) == "" {
		cfg.URLPrefix = defaults.URLPrefix
	}
	if cfg.MaxAssetBytes <= 0 {
		cfg.MaxAssetBytes = defaults.MaxAssetBytes
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
	dir := filepath.Join(s.cfg.RootDir, "users", userID)
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
	root := filepath.Join(s.cfg.RootDir, "users", userID)
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
