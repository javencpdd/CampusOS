package space

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSpaceFileRoot       = "data/images/personal-space"
	defaultSpaceFileURLPrefix  = "/api/v1/spaces/files"
	defaultSpaceFileQuotaBytes = int64(10 * 1024 * 1024)
	defaultAvatarKeepLimit     = 3
	defaultMaxAvatarBytes      = int64(2 * 1024 * 1024)
)

var (
	ErrSpaceFileStoreUnavailable = errors.New("space file store is not configured")
	ErrSpaceFileInvalidName      = errors.New("invalid space file name")
	ErrSpaceFileNotFound         = errors.New("space file not found")
	ErrSpaceFileTooLarge         = errors.New("space file exceeds allowed size")
	ErrSpaceFileQuotaExceeded    = errors.New("space file quota exceeded")
	ErrSpaceFileUnsupportedType  = errors.New("unsupported space file type")
)

type FileStorageConfig struct {
	RootDir           string
	URLPrefix         string
	DefaultQuotaBytes int64
	AvatarKeepLimit   int
	MaxAvatarBytes    int64
}

type SpaceStorageStatus struct {
	UserID          string `json:"user_id"`
	QuotaBytes      int64  `json:"quota_bytes"`
	UsedBytes       int64  `json:"used_bytes"`
	AvailableBytes  int64  `json:"available_bytes"`
	AvatarKeepLimit int    `json:"avatar_keep_limit"`
}

type AvatarUploadResult struct {
	FileName string             `json:"file_name"`
	URL      string             `json:"url"`
	Size     int64              `json:"size"`
	Storage  SpaceStorageStatus `json:"storage"`
	Owner    Owner              `json:"owner"`
	Space    *Space             `json:"space"`
}

type LocalFileStore struct {
	cfg FileStorageConfig
}

func DefaultFileStorageConfig() FileStorageConfig {
	return FileStorageConfig{
		RootDir:           defaultSpaceFileRoot,
		URLPrefix:         defaultSpaceFileURLPrefix,
		DefaultQuotaBytes: defaultSpaceFileQuotaBytes,
		AvatarKeepLimit:   defaultAvatarKeepLimit,
		MaxAvatarBytes:    defaultMaxAvatarBytes,
	}
}

func FileStorageConfigFromPluginConfig(raw map[string]interface{}) FileStorageConfig {
	cfg := DefaultFileStorageConfig()
	if raw == nil {
		return cfg
	}
	if value := stringConfig(raw, "file_root"); value != "" {
		cfg.RootDir = value
	}
	if value := stringConfig(raw, "file_url_prefix"); value != "" {
		cfg.URLPrefix = "/" + strings.Trim(strings.TrimSpace(value), "/")
	}
	if value := int64Config(raw, "default_quota_bytes"); value > 0 {
		cfg.DefaultQuotaBytes = value
	}
	if value := int64Config(raw, "default_quota_mb"); value > 0 {
		cfg.DefaultQuotaBytes = value * 1024 * 1024
	}
	if value := intConfig(raw, "avatar_keep_limit"); value > 0 {
		cfg.AvatarKeepLimit = value
	}
	if value := int64Config(raw, "max_avatar_bytes"); value > 0 {
		cfg.MaxAvatarBytes = value
	}
	if value := int64Config(raw, "max_avatar_mb"); value > 0 {
		cfg.MaxAvatarBytes = value * 1024 * 1024
	}
	return cfg.withDefaults()
}

func NewLocalFileStore(cfg FileStorageConfig) (*LocalFileStore, error) {
	cfg = cfg.withDefaults()
	root, err := filepath.Abs(filepath.Clean(cfg.RootDir))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	cfg.RootDir = root
	return &LocalFileStore{cfg: cfg}, nil
}

func (cfg FileStorageConfig) withDefaults() FileStorageConfig {
	defaults := DefaultFileStorageConfig()
	if strings.TrimSpace(cfg.RootDir) == "" {
		cfg.RootDir = defaults.RootDir
	}
	if strings.TrimSpace(cfg.URLPrefix) == "" {
		cfg.URLPrefix = defaults.URLPrefix
	}
	cfg.URLPrefix = "/" + strings.Trim(strings.TrimSpace(cfg.URLPrefix), "/")
	if cfg.DefaultQuotaBytes <= 0 {
		cfg.DefaultQuotaBytes = defaults.DefaultQuotaBytes
	}
	if cfg.AvatarKeepLimit <= 0 {
		cfg.AvatarKeepLimit = defaults.AvatarKeepLimit
	}
	if cfg.MaxAvatarBytes <= 0 {
		cfg.MaxAvatarBytes = defaults.MaxAvatarBytes
	}
	return cfg
}

func (s *LocalFileStore) Status(userID string) (*SpaceStorageStatus, error) {
	if s == nil {
		return nil, ErrSpaceFileStoreUnavailable
	}
	usage, err := s.userUsage(userID)
	if err != nil {
		return nil, err
	}
	available := s.cfg.DefaultQuotaBytes - usage
	if available < 0 {
		available = 0
	}
	return &SpaceStorageStatus{
		UserID:          userID,
		QuotaBytes:      s.cfg.DefaultQuotaBytes,
		UsedBytes:       usage,
		AvailableBytes:  available,
		AvatarKeepLimit: s.cfg.AvatarKeepLimit,
	}, nil
}

func (s *LocalFileStore) SaveAvatar(userID, originalName string, reader io.Reader) (string, int64, error) {
	if s == nil {
		return "", 0, ErrSpaceFileStoreUnavailable
	}
	if err := validateStorageSegment(userID); err != nil {
		return "", 0, err
	}
	if reader == nil {
		return "", 0, ErrSpaceFileInvalidName
	}
	avatarDir, err := s.avatarDir(userID)
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		return "", 0, err
	}

	limit := s.cfg.MaxAvatarBytes
	if limit > s.cfg.DefaultQuotaBytes {
		limit = s.cfg.DefaultQuotaBytes
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return "", 0, err
	}
	if int64(len(data)) > limit {
		return "", 0, ErrSpaceFileTooLarge
	}
	ext, err := avatarExtension(originalName, data)
	if err != nil {
		return "", 0, err
	}
	usage, err := s.userUsage(userID)
	if err != nil {
		return "", 0, err
	}
	reclaim, err := s.prunableAvatarBytes(userID, s.cfg.AvatarKeepLimit-1)
	if err != nil {
		return "", 0, err
	}
	if usage-reclaim+int64(len(data)) > s.cfg.DefaultQuotaBytes {
		return "", 0, ErrSpaceFileQuotaExceeded
	}

	fileName := fmt.Sprintf("%d%s", time.Now().UTC().UnixNano(), ext)
	if err := validateStorageSegment(fileName); err != nil {
		return "", 0, err
	}
	target := filepath.Join(avatarDir, fileName)
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", 0, err
	}
	if err := s.pruneAvatars(userID, s.cfg.AvatarKeepLimit); err != nil {
		return "", 0, err
	}
	return fileName, int64(len(data)), nil
}

func (s *LocalFileStore) AvatarURL(userID, fileName string) string {
	return fmt.Sprintf("%s/%s/avatars/%s", s.cfg.URLPrefix, userID, fileName)
}

func (s *LocalFileStore) AvatarPath(userID, fileName string) (string, error) {
	if s == nil {
		return "", ErrSpaceFileStoreUnavailable
	}
	if err := validateStorageSegment(userID); err != nil {
		return "", err
	}
	if err := validateStorageSegment(fileName); err != nil {
		return "", err
	}
	avatarDir, err := s.avatarDir(userID)
	if err != nil {
		return "", err
	}
	target := filepath.Join(avatarDir, fileName)
	rootAbs, err := filepath.Abs(filepath.Clean(avatarDir))
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(os.PathSeparator)) {
		return "", ErrSpaceFileInvalidName
	}
	if _, err := os.Stat(targetAbs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrSpaceFileNotFound
		}
		return "", err
	}
	return targetAbs, nil
}

func (s *LocalFileStore) userUsage(userID string) (int64, error) {
	userRoot, err := s.userDir(userID)
	if err != nil {
		return 0, err
	}
	var usage int64
	err = filepath.WalkDir(userRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		usage += info.Size()
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return usage, err
}

func (s *LocalFileStore) pruneAvatars(userID string, keep int) error {
	files, err := s.avatarFiles(userID)
	if err != nil {
		return err
	}
	for _, file := range files[normalizeKeepLimit(keep, len(files)):] {
		if err := os.Remove(filepath.Join(file.dir, file.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *LocalFileStore) prunableAvatarBytes(userID string, keep int) (int64, error) {
	files, err := s.avatarFiles(userID)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, file := range files[normalizeKeepLimit(keep, len(files)):] {
		total += file.size
	}
	return total, nil
}

type avatarFile struct {
	dir     string
	name    string
	size    int64
	modTime time.Time
}

func (s *LocalFileStore) avatarFiles(userID string) ([]avatarFile, error) {
	avatarDir, err := s.avatarDir(userID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(avatarDir)
	if errors.Is(err, os.ErrNotExist) {
		return []avatarFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]avatarFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		files = append(files, avatarFile{
			dir:     avatarDir,
			name:    entry.Name(),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].name > files[j].name
		}
		return files[i].modTime.After(files[j].modTime)
	})
	return files, nil
}

func normalizeKeepLimit(keep, length int) int {
	if keep < 0 {
		return 0
	}
	if keep > length {
		return length
	}
	return keep
}

func (s *LocalFileStore) userDir(userID string) (string, error) {
	if err := validateStorageSegment(userID); err != nil {
		return "", err
	}
	root, err := filepath.Abs(filepath.Clean(s.cfg.RootDir))
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, "users", userID)
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	if targetAbs != root && !strings.HasPrefix(targetAbs, root+string(os.PathSeparator)) {
		return "", ErrSpaceFileInvalidName
	}
	return targetAbs, nil
}

func (s *LocalFileStore) avatarDir(userID string) (string, error) {
	userDir, err := s.userDir(userID)
	if err != nil {
		return "", err
	}
	return filepath.Join(userDir, "avatars"), nil
}

func avatarExtension(originalName string, data []byte) (string, error) {
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return "", ErrSpaceFileUnsupportedType
	}
	ext := strings.ToLower(filepath.Ext(originalName))
	if ext == "" {
		exts, _ := mime.ExtensionsByType(contentType)
		if len(exts) > 0 {
			ext = exts[0]
		}
	}
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return ext, nil
	default:
		return "", ErrSpaceFileUnsupportedType
	}
}

func validateStorageSegment(value string) error {
	if value == "" || value == "." || value == ".." {
		return ErrSpaceFileInvalidName
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return ErrSpaceFileInvalidName
	}
	return nil
}

func stringConfig(raw map[string]interface{}, key string) string {
	value, ok := raw[key]
	if !ok {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func intConfig(raw map[string]interface{}, key string) int {
	value := int64Config(raw, key)
	if value > int64(^uint(0)>>1) {
		return 0
	}
	return int(value)
}

func int64Config(raw map[string]interface{}, key string) int64 {
	value, ok := raw[key]
	if !ok {
		return 0
	}
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return parsed
	default:
		return 0
	}
}
