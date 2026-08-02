package homepage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
)

const (
	LogoURL                 = "/api/v1/home/logo"
	DefaultLogoMaxBytes     = int64(2 * 1024 * 1024)
	DefaultLogoMaxDimension = 1024
	logoFileConfigKey       = "logo_file"
	logoVersionConfigKey    = "logo_version"
	logoMIMEConfigKey       = "logo_mime_type"
	logoSizeConfigKey       = "logo_size_bytes"
	logoWidthConfigKey      = "logo_width"
	logoHeightConfigKey     = "logo_height"
)

var (
	ErrLogoTooLarge       = errors.New("system logo exceeds allowed size")
	ErrLogoUnsupported    = errors.New("system logo type is unsupported")
	ErrLogoAssetNotFound  = errors.New("system logo asset was not found")
	ErrLogoStoreFailed    = errors.New("system logo could not be stored")
	ErrLogoConfigDisabled = errors.New("appearance config is unavailable")
)

type LogoInfo struct {
	URL       string `json:"url"`
	Custom    bool   `json:"custom"`
	MIMEType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	MaxBytes  int64  `json:"max_bytes"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type LogoAsset struct {
	Data     []byte
	MIMEType string
	Version  string
}

func (s *Service) LogoInfo() LogoInfo {
	info := LogoInfo{
		URL:       logoURL("default"),
		MIMEType:  "image/png",
		MaxBytes:  DefaultLogoMaxBytes,
		SizeBytes: 0,
	}
	if s == nil || s.config == nil {
		populateLogoMetadata(&info, defaultLogoPath())
		return info
	}
	raw := s.config.Config()
	fileName := safeManagedLogoName(stringConfig(raw, logoFileConfigKey, ""))
	version := stringConfig(raw, logoVersionConfigKey, "default")
	info.Custom = fileName != ""
	info.URL = logoURL(version)
	info.MIMEType = stringConfig(raw, logoMIMEConfigKey, info.MIMEType)
	info.SizeBytes = int64ConfigValue(raw, logoSizeConfigKey)
	info.Width = int(int64ConfigValue(raw, logoWidthConfigKey))
	info.Height = int(int64ConfigValue(raw, logoHeightConfigKey))
	if info.Custom && version != "default" {
		if nanos, err := strconv.ParseInt(version, 10, 64); err == nil {
			info.UpdatedAt = time.Unix(0, nanos).UTC().Format(time.RFC3339)
		}
	}
	if !info.Custom {
		populateLogoMetadata(&info, defaultLogoPath())
	}
	return info
}

func populateLogoMetadata(info *LogoInfo, path string) {
	if info == nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	info.SizeBytes = int64(len(data))
	if config, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		info.Width = config.Width
		info.Height = config.Height
	}
}

func (s *Service) SaveLogo(_ context.Context, reader io.Reader) (*LogoInfo, error) {
	if s == nil || s.config == nil || !s.config.Enabled() {
		return nil, ErrLogoConfigDisabled
	}
	if reader == nil {
		return nil, ErrLogoUnsupported
	}
	data, err := io.ReadAll(io.LimitReader(reader, DefaultLogoMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLogoStoreFailed, err)
	}
	if int64(len(data)) > DefaultLogoMaxBytes {
		return nil, ErrLogoTooLarge
	}
	optimized, err := corestorage.OptimizeImageWithin(data, DefaultLogoMaxDimension)
	if err != nil || (optimized.MimeType != "image/png" && optimized.MimeType != "image/jpeg") {
		return nil, ErrLogoUnsupported
	}
	if int64(len(optimized.Data)) > DefaultLogoMaxBytes {
		return nil, ErrLogoTooLarge
	}

	version := strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	fileName := "logo-" + version + optimized.Extension
	logoDir := managedLogoDir()
	if err := os.MkdirAll(logoDir, 0o755); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLogoStoreFailed, err)
	}
	target := filepath.Join(logoDir, fileName)
	if err := writeLogoAtomically(target, optimized.Data); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLogoStoreFailed, err)
	}

	current := s.config.Config()
	previous := safeManagedLogoName(stringConfig(current, logoFileConfigKey, ""))
	next := copyConfig(current)
	next[logoFileConfigKey] = fileName
	next[logoVersionConfigKey] = version
	next[logoMIMEConfigKey] = optimized.MimeType
	next[logoSizeConfigKey] = int64(len(optimized.Data))
	next[logoWidthConfigKey] = optimized.Width
	next[logoHeightConfigKey] = optimized.Height
	if _, err := s.config.Update(next); err != nil {
		_ = os.Remove(target)
		return nil, fmt.Errorf("%w: %v", ErrLogoStoreFailed, err)
	}
	if previous != "" && previous != fileName {
		_ = os.Remove(filepath.Join(logoDir, previous))
	}
	info := s.LogoInfo()
	return &info, nil
}

func (s *Service) ResetLogo(_ context.Context) (*LogoInfo, error) {
	if s == nil || s.config == nil || !s.config.Enabled() {
		return nil, ErrLogoConfigDisabled
	}
	current := s.config.Config()
	previous := safeManagedLogoName(stringConfig(current, logoFileConfigKey, ""))
	next := copyConfig(current)
	for _, key := range []string{logoFileConfigKey, logoVersionConfigKey, logoMIMEConfigKey, logoSizeConfigKey, logoWidthConfigKey, logoHeightConfigKey} {
		delete(next, key)
	}
	if _, err := s.config.Update(next); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLogoStoreFailed, err)
	}
	if previous != "" {
		_ = os.Remove(filepath.Join(managedLogoDir(), previous))
	}
	info := s.LogoInfo()
	return &info, nil
}

func (s *Service) LogoAsset(_ context.Context) (*LogoAsset, error) {
	info := s.LogoInfo()
	path := defaultLogoPath()
	version := "default"
	if s != nil && s.config != nil && info.Custom {
		fileName := safeManagedLogoName(stringConfig(s.config.Config(), logoFileConfigKey, ""))
		if fileName != "" {
			path = filepath.Join(managedLogoDir(), fileName)
			version = stringConfig(s.config.Config(), logoVersionConfigKey, "custom")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil && info.Custom {
		path, version = defaultLogoPath(), "default"
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, ErrLogoAssetNotFound
	}
	mimeType := "image/png"
	if strings.EqualFold(filepath.Ext(path), ".jpg") || strings.EqualFold(filepath.Ext(path), ".jpeg") {
		mimeType = "image/jpeg"
	}
	return &LogoAsset{Data: data, MIMEType: mimeType, Version: version}, nil
}

func logoURL(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "default"
	}
	return LogoURL + "?v=" + version
}

func defaultLogoPath() string {
	return filepath.Join(stylepack.ResourceDir(), "branding", "default-logo.png")
}

func managedLogoDir() string {
	return filepath.Join(filepath.Dir(stylepack.ResourceDir()), "config", "branding")
}

func safeManagedLogoName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(value))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		return ""
	}
	return value
}

func writeLogoAtomically(target string, data []byte) error {
	dir := filepath.Dir(target)
	temporary, err := os.CreateTemp(dir, ".campusos-logo-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, target)
}

func int64ConfigValue(raw map[string]interface{}, key string) int64 {
	value, ok := raw[key]
	if !ok {
		return 0
	}
	switch parsed := value.(type) {
	case int:
		return int64(parsed)
	case int64:
		return parsed
	case float64:
		return int64(parsed)
	case string:
		result, _ := strconv.ParseInt(strings.TrimSpace(parsed), 10, 64)
		return result
	default:
		return 0
	}
}
