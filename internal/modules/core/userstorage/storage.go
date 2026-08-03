package storage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	DefaultQuotaBytes = int64(50 * 1024 * 1024)
	DefaultRoot       = "data/personal-space"
	LegacyRoot        = "data/images/personal-space"
	FileDir           = "file"
	ImageDir          = "img"
	ExcelDir          = "excel"
	WordDir           = "word"
	PDFDir            = "pdf"
)

var ErrUnsafePath = errors.New("user storage path is unsafe")
var ErrQuotaExceeded = errors.New("user storage quota exceeded")

// Port is the core boundary used by features that need user-owned files.
type Port interface {
	Root() string
	EnsureLayout(userID string) (string, error)
	Path(userID string, parts ...string) (string, error)
	Usage(userID string) (int64, error)
}

type Quota interface {
	QuotaBytes(userID string) int64
	CheckQuota(userID string, incoming int64) error
}

type QuotaManager interface {
	Quota
	DefaultQuotaBytes() int64
	QuotaOverride(userID string) (QuotaRecord, bool)
	SetQuota(context.Context, string, int64, string) (QuotaRecord, error)
}

type SafePath interface {
	Path(userID string, parts ...string) (string, error)
}

type AssetStore interface {
	Path(userID, fileName string) (string, error)
}

type Provider interface {
	Name() string
	Available() bool
}

type LocalAdapter struct {
	root            string
	quotaBytes      int64
	quotaRepository QuotaRepository
	quotaMu         sync.RWMutex
	quotaOverrides  map[string]QuotaRecord
}

func NewLocalAdapter(root string) (*LocalAdapter, error) {
	return NewLocalAdapterWithQuota(root, DefaultQuotaBytes)
}

func NewLocalAdapterWithQuota(root string, quotaBytes int64) (*LocalAdapter, error) {
	return NewLocalAdapterWithQuotaRepository(root, quotaBytes, NewMemoryQuotaRepository())
}

func NewLocalAdapterWithQuotaRepository(root string, quotaBytes int64, quotaRepository QuotaRepository) (*LocalAdapter, error) {
	root, err := filepath.Abs(filepath.Clean(NormalizeRoot(root)))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	if quotaBytes <= 0 {
		quotaBytes = DefaultQuotaBytes
	}
	if quotaRepository == nil {
		quotaRepository = NewMemoryQuotaRepository()
	}
	items, err := quotaRepository.List(context.Background())
	if err != nil {
		return nil, err
	}
	adapter := &LocalAdapter{
		root: root, quotaBytes: quotaBytes, quotaRepository: quotaRepository,
		quotaOverrides: make(map[string]QuotaRecord, len(items)),
	}
	for _, item := range items {
		if item.QuotaBytes > 0 {
			adapter.quotaOverrides[item.UserID] = item
		}
	}
	return adapter, nil
}

func (a *LocalAdapter) DefaultQuotaBytes() int64 { return a.quotaBytes }
func (a *LocalAdapter) QuotaBytes(userID string) int64 {
	a.quotaMu.RLock()
	defer a.quotaMu.RUnlock()
	if item, ok := a.quotaOverrides[strings.TrimSpace(userID)]; ok && item.QuotaBytes > 0 {
		return item.QuotaBytes
	}
	return a.quotaBytes
}
func (a *LocalAdapter) QuotaOverride(userID string) (QuotaRecord, bool) {
	a.quotaMu.RLock()
	defer a.quotaMu.RUnlock()
	item, ok := a.quotaOverrides[strings.TrimSpace(userID)]
	return item, ok
}
func (a *LocalAdapter) SetQuota(ctx context.Context, userID string, quotaBytes int64, actorID string) (QuotaRecord, error) {
	userID = strings.TrimSpace(userID)
	if !SafeSegment(userID) || quotaBytes <= 0 {
		return QuotaRecord{}, ErrUnsafePath
	}
	item, err := a.quotaRepository.Upsert(ctx, QuotaRecord{UserID: userID, QuotaBytes: quotaBytes, UpdatedBy: strings.TrimSpace(actorID)})
	if err != nil {
		return QuotaRecord{}, err
	}
	a.quotaMu.Lock()
	a.quotaOverrides[userID] = item
	a.quotaMu.Unlock()
	return item, nil
}
func (a *LocalAdapter) CheckQuota(userID string, incoming int64) error {
	usage, err := a.Usage(userID)
	if err != nil {
		return err
	}
	if incoming < 0 || usage+incoming > a.QuotaBytes(userID) {
		return ErrQuotaExceeded
	}
	return nil
}

func (a *LocalAdapter) Root() string { return a.root }
func (a *LocalAdapter) EnsureLayout(userID string) (string, error) {
	dir, err := UserDir(a.root, userID)
	if err != nil {
		return "", err
	}
	for _, name := range []string{FileDir, ImageDir, ExcelDir, WordDir, PDFDir} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			return "", err
		}
	}
	return dir, nil
}
func (a *LocalAdapter) Path(userID string, parts ...string) (string, error) {
	return UserPath(a.root, userID, parts...)
}
func (a *LocalAdapter) Usage(userID string) (int64, error) {
	dir, err := UserDir(a.root, userID)
	if err != nil {
		return 0, err
	}
	var total int64
	err = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			total += info.Size()
		}
		return err
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return total, err
}

func NormalizeRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return DefaultRoot
	}
	legacy, _ := filepath.Abs(filepath.Clean(LegacyRoot))
	value, _ := filepath.Abs(filepath.Clean(root))
	if legacy == value {
		return DefaultRoot
	}
	return root
}

func UserDir(root, userID string) (string, error) { return UserPath(root, userID) }

func UserPath(root, userID string, parts ...string) (string, error) {
	if !SafeSegment(userID) {
		return "", ErrUnsafePath
	}
	base, err := filepath.Abs(filepath.Join(filepath.Clean(NormalizeRoot(root)), userID))
	if err != nil {
		return "", err
	}
	target := base
	for _, part := range parts {
		if !SafeSegment(part) {
			return "", ErrUnsafePath
		}
		target = filepath.Join(target, part)
	}
	target, err = filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	if target != base && !strings.HasPrefix(target, base+string(os.PathSeparator)) {
		return "", ErrUnsafePath
	}
	return target, nil
}

func SafeSegment(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, `/\\`)
}
