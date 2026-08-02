package space

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

const (
	defaultSpaceFileRoot       = "data/personal-space"
	legacySpaceFileRoot        = "data/images/personal-space"
	defaultSpaceFileURLPrefix  = "/api/v1/spaces/files"
	defaultSpaceFileQuotaBytes = corestorage.DefaultQuotaBytes
	defaultAvatarKeepLimit     = 3
	defaultMaxAvatarBytes      = int64(2 * 1024 * 1024)
)

const (
	PersonalSpaceFileDir  = corestorage.FileDir
	PersonalSpaceImageDir = corestorage.ImageDir
	PersonalSpaceExcelDir = corestorage.ExcelDir
	PersonalSpaceWordDir  = corestorage.WordDir
	PersonalSpacePDFDir   = corestorage.PDFDir
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
	UserID            string     `json:"user_id"`
	QuotaBytes        int64      `json:"quota_bytes"`
	DefaultQuotaBytes int64      `json:"default_quota_bytes"`
	UsedBytes         int64      `json:"used_bytes"`
	AvailableBytes    int64      `json:"available_bytes"`
	MaxAvatarBytes    int64      `json:"max_avatar_bytes"`
	AvatarKeepLimit   int        `json:"avatar_keep_limit"`
	CustomQuota       bool       `json:"custom_quota"`
	QuotaUpdatedBy    string     `json:"quota_updated_by,omitempty"`
	QuotaUpdatedAt    *time.Time `json:"quota_updated_at,omitempty"`
}

type AvatarUploadResult struct {
	FileName string              `json:"file_name"`
	URL      string              `json:"url"`
	Size     int64               `json:"size"`
	Storage  SpaceStorageStatus  `json:"storage"`
	Owner    Owner               `json:"owner"`
	Space    *Space              `json:"space"`
	Avatars  []AvatarHistoryItem `json:"avatars"`
}

type AvatarHistoryItem struct {
	FileName   string    `json:"file_name"`
	URL        string    `json:"url"`
	Size       int64     `json:"size"`
	UploadedAt time.Time `json:"uploaded_at"`
	Active     bool      `json:"active"`
}

type AvatarHistory struct {
	Items   []AvatarHistoryItem `json:"items"`
	Storage SpaceStorageStatus  `json:"storage"`
}

type LocalFileStore struct {
	cfg   FileStorageConfig
	quota corestorage.Quota
	mu    sync.Mutex
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
		cfg.RootDir = NormalizePersonalSpaceFileRoot(value)
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
	return newLocalFileStore(cfg)
}

// NewLocalFileStoreWithStorage uses the User Storage Core default root and
// quota while preserving the existing personal-space URL/layout contract.
func NewLocalFileStoreWithStorage(cfg FileStorageConfig, storage corestorage.Port) (*LocalFileStore, error) {
	if storage == nil {
		return nil, ErrSpaceFileStoreUnavailable
	}
	cfg.RootDir = storage.Root()
	quota, ok := storage.(corestorage.Quota)
	if ok {
		cfg.DefaultQuotaBytes = quota.QuotaBytes("")
	}
	store, err := newLocalFileStore(cfg)
	if err == nil && ok {
		store.quota = quota
	}
	return store, err
}

func newLocalFileStore(cfg FileStorageConfig) (*LocalFileStore, error) {
	cfg = cfg.withDefaults()
	root, err := filepath.Abs(filepath.Clean(cfg.RootDir))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	cfg.RootDir = root
	if err := MigrateLegacyPersonalSpaceStorage(root); err != nil {
		return nil, err
	}
	return &LocalFileStore{cfg: cfg}, nil
}

func (cfg FileStorageConfig) withDefaults() FileStorageConfig {
	defaults := DefaultFileStorageConfig()
	if strings.TrimSpace(cfg.RootDir) == "" {
		cfg.RootDir = defaults.RootDir
	}
	cfg.RootDir = NormalizePersonalSpaceFileRoot(cfg.RootDir)
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

// NormalizePersonalSpaceFileRoot moves the former default root to the current
// data/personal-space root while leaving an explicitly configured custom root intact.
func NormalizePersonalSpaceFileRoot(root string) string {
	return corestorage.NormalizeRoot(root)
}

// PersonalSpaceUserDir returns the root directory reserved for one user's data.
func PersonalSpaceUserDir(rootDir, userID string) (string, error) {
	path, err := corestorage.UserDir(rootDir, userID)
	if err != nil {
		return "", errors.Join(ErrSpaceFileInvalidName, err)
	}
	return path, nil
}

// PersonalSpacePath builds a checked path below one user's personal-space root.
func PersonalSpacePath(rootDir, userID string, parts ...string) (string, error) {
	path, err := corestorage.UserPath(rootDir, userID, parts...)
	if err != nil {
		return "", errors.Join(ErrSpaceFileInvalidName, err)
	}
	return path, nil
}

// EnsurePersonalSpaceLayout creates the standard directories for one user.
func EnsurePersonalSpaceLayout(rootDir, userID string) (string, error) {
	storage, err := corestorage.NewLocalAdapter(rootDir)
	if err != nil {
		return "", err
	}
	return storage.EnsureLayout(userID)
}

// FileCategoryForName assigns ordinary uploaded files to a stable user directory.
func FileCategoryForName(fileName string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg":
		return PersonalSpaceImageDir
	case ".xls", ".xlsx", ".csv", ".ods":
		return PersonalSpaceExcelDir
	case ".doc", ".docx", ".odt", ".rtf":
		return PersonalSpaceWordDir
	case ".pdf":
		return PersonalSpacePDFDir
	default:
		return PersonalSpaceFileDir
	}
}

func (s *LocalFileStore) CategorizedFileDir(userID, fileName string) (string, error) {
	if s == nil {
		return "", ErrSpaceFileStoreUnavailable
	}
	return PersonalSpacePath(s.cfg.RootDir, userID, FileCategoryForName(fileName))
}

// MigrateLegacyPersonalSpaceStorage migrates the old default layout once.
// Custom storage roots are deliberately left untouched.
func MigrateLegacyPersonalSpaceStorage(targetRoot string) error {
	if !sameStoragePath(targetRoot, defaultSpaceFileRoot) {
		return nil
	}
	legacyRoot, err := filepath.Abs(filepath.Clean(legacySpaceFileRoot))
	if err != nil {
		return err
	}
	legacyUsers := filepath.Join(legacyRoot, "users")
	entries, err := os.ReadDir(legacyUsers)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || validateStorageSegment(entry.Name()) != nil {
			continue
		}
		legacyUserDir := filepath.Join(legacyUsers, entry.Name())
		userDir, err := PersonalSpaceUserDir(targetRoot, entry.Name())
		if err != nil {
			return err
		}
		if err := migrateLegacyUserDir(legacyUserDir, userDir); err != nil {
			return err
		}
		if _, err := EnsurePersonalSpaceLayout(targetRoot, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyUserDir(legacyUserDir, userDir string) error {
	entries, err := os.ReadDir(legacyUserDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := validateStorageSegment(entry.Name()); err != nil {
			continue
		}
		destination := legacyDestination(userDir, entry.Name())
		if err := moveLegacyPath(filepath.Join(legacyUserDir, entry.Name()), destination); err != nil {
			return err
		}
	}
	// Retaining an empty legacy directory is harmless; it keeps migration tolerant
	// of unexpected entries that should not be moved automatically.
	_ = os.Remove(legacyUserDir)
	return nil
}

func legacyDestination(userDir, name string) string {
	switch name {
	case "avatars":
		return filepath.Join(userDir, PersonalSpaceImageDir, "avatars")
	case "schedule":
		return filepath.Join(userDir, PersonalSpaceFileDir, "schedule")
	case "file", "files":
		return filepath.Join(userDir, PersonalSpaceFileDir)
	case "img", "images":
		return filepath.Join(userDir, PersonalSpaceImageDir)
	case "excel", "execl":
		return filepath.Join(userDir, PersonalSpaceExcelDir)
	case "word":
		return filepath.Join(userDir, PersonalSpaceWordDir)
	case "pdf":
		return filepath.Join(userDir, PersonalSpacePDFDir)
	default:
		return filepath.Join(userDir, PersonalSpaceFileDir, "legacy", name)
	}
}

func moveLegacyPath(source, target string) error {
	info, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return moveLegacyFile(source, target)
	}
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.Rename(source, target); err == nil {
			return nil
		}
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := moveLegacyPath(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
			return err
		}
	}
	return os.Remove(source)
}

func moveLegacyFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
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

func (s *LocalFileStore) Status(userID string) (*SpaceStorageStatus, error) {
	if s == nil {
		return nil, ErrSpaceFileStoreUnavailable
	}
	usage, err := s.userUsage(userID)
	if err != nil {
		return nil, err
	}
	quotaBytes := s.quotaBytes(userID)
	available := quotaBytes - usage
	if available < 0 {
		available = 0
	}
	return &SpaceStorageStatus{
		UserID:            userID,
		QuotaBytes:        quotaBytes,
		DefaultQuotaBytes: s.cfg.DefaultQuotaBytes,
		UsedBytes:         usage,
		AvailableBytes:    available,
		MaxAvatarBytes:    s.cfg.MaxAvatarBytes,
		AvatarKeepLimit:   s.cfg.AvatarKeepLimit,
	}, nil
}

func (s *LocalFileStore) MaxAvatarBytes() int64 {
	if s == nil || s.cfg.MaxAvatarBytes <= 0 {
		return defaultMaxAvatarBytes
	}
	return s.cfg.MaxAvatarBytes
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := EnsurePersonalSpaceLayout(s.cfg.RootDir, userID); err != nil {
		return "", 0, err
	}
	avatarDir, err := s.avatarDir(userID)
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		return "", 0, err
	}

	limit := s.cfg.MaxAvatarBytes
	quotaBytes := s.quotaBytes(userID)
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return "", 0, err
	}
	if int64(len(data)) > limit {
		return "", 0, ErrSpaceFileTooLarge
	}
	optimized, err := corestorage.OptimizeImage(data)
	if err != nil {
		return "", 0, ErrSpaceFileUnsupportedType
	}
	data = optimized.Data
	if int64(len(data)) > s.cfg.MaxAvatarBytes {
		return "", 0, ErrSpaceFileTooLarge
	}
	ext := optimized.Extension
	usage, err := s.userUsage(userID)
	if err != nil {
		return "", 0, err
	}
	reclaim, err := s.prunableAvatarBytes(userID, s.cfg.AvatarKeepLimit-1)
	if err != nil {
		return "", 0, err
	}
	if usage-reclaim+int64(len(data)) > quotaBytes {
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

func (s *LocalFileStore) quotaBytes(userID string) int64 {
	if s.quota != nil {
		return s.quota.QuotaBytes(userID)
	}
	return s.cfg.DefaultQuotaBytes
}

func (s *LocalFileStore) AvatarURL(userID, fileName string) string {
	return fmt.Sprintf("%s/%s/avatars/%s", s.cfg.URLPrefix, userID, fileName)
}

func (s *LocalFileStore) ListAvatars(userID, activeURL string) ([]AvatarHistoryItem, error) {
	if s == nil {
		return nil, ErrSpaceFileStoreUnavailable
	}
	if err := validateStorageSegment(userID); err != nil {
		return nil, err
	}
	files, err := s.avatarFiles(userID)
	if err != nil {
		return nil, err
	}
	items := make([]AvatarHistoryItem, 0, len(files))
	for _, file := range files {
		url := s.AvatarURL(userID, file.name)
		items = append(items, AvatarHistoryItem{
			FileName: file.name, URL: url, Size: file.size, UploadedAt: file.modTime,
			Active: strings.TrimSpace(activeURL) == url,
		})
	}
	return items, nil
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
	return PersonalSpaceUserDir(s.cfg.RootDir, userID)
}

func (s *LocalFileStore) avatarDir(userID string) (string, error) {
	return PersonalSpacePath(s.cfg.RootDir, userID, PersonalSpaceImageDir, "avatars")
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

func sameStoragePath(a, b string) bool {
	aAbs, aErr := filepath.Abs(filepath.Clean(a))
	bAbs, bErr := filepath.Abs(filepath.Clean(b))
	return aErr == nil && bErr == nil && aAbs == bAbs
}
