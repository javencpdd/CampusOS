package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultRoot = "data/personal-space"
	LegacyRoot  = "data/images/personal-space"
	FileDir     = "file"
	ImageDir    = "img"
	ExcelDir    = "excel"
	WordDir     = "word"
	PDFDir      = "pdf"
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
	root       string
	quotaBytes int64
}

func NewLocalAdapter(root string) (*LocalAdapter, error) {
	return NewLocalAdapterWithQuota(root, 10*1024*1024)
}

func NewLocalAdapterWithQuota(root string, quotaBytes int64) (*LocalAdapter, error) {
	root, err := filepath.Abs(filepath.Clean(NormalizeRoot(root)))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	if quotaBytes <= 0 {
		quotaBytes = 10 * 1024 * 1024
	}
	return &LocalAdapter{root: root, quotaBytes: quotaBytes}, nil
}
func (a *LocalAdapter) QuotaBytes(string) int64 { return a.quotaBytes }
func (a *LocalAdapter) CheckQuota(userID string, incoming int64) error {
	usage, err := a.Usage(userID)
	if err != nil {
		return err
	}
	if incoming < 0 || usage+incoming > a.quotaBytes {
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
