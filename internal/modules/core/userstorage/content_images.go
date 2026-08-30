package storage

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ContentImageDir        = "content"
	DefaultMaxContentImage = int64(5 * 1024 * 1024)
	ContentImageURLPrefix  = "/api/v1/content/assets/images"
	contentImageFormSlack  = int64(64 * 1024)
	maxListedContentImages = 200
)

var (
	ErrImageTooLarge    = errors.New("content image exceeds the size limit")
	ErrImageUnsupported = errors.New("content image type is unsupported")
	ErrImageNotFound    = errors.New("content image was not found")
)

type ContentImage struct {
	FileURL   string    `json:"file_url"`
	FileName  string    `json:"file_name"`
	FileSize  int64     `json:"file_size"`
	MimeType  string    `json:"mime_type"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ContentImageStore is an always-on User Storage capability. Built-in content
// features can use it without depending on another feature's lifecycle.
type ContentImageStore struct {
	storage  Port
	quota    Quota
	maxBytes int64
	mu       sync.Mutex
}

func NewContentImageStore(storage Port, quota Quota, maxBytes int64) (*ContentImageStore, error) {
	if storage == nil || quota == nil {
		return nil, errors.New("user storage and quota ports are required")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxContentImage
	}
	return &ContentImageStore{storage: storage, quota: quota, maxBytes: maxBytes}, nil
}

func (s *ContentImageStore) MaxBytes() int64 {
	if s == nil {
		return 0
	}
	return s.maxBytes
}

func (s *ContentImageStore) Save(userID, originalName string, reader io.Reader) (*ContentImage, error) {
	if s == nil || reader == nil || !SafeSegment(userID) {
		return nil, ErrUnsafePath
	}
	data, err := io.ReadAll(io.LimitReader(reader, s.maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > s.maxBytes {
		return nil, ErrImageTooLarge
	}
	optimized, err := OptimizeImage(data)
	if err != nil {
		return nil, err
	}
	data = optimized.Data
	if int64(len(data)) > s.maxBytes {
		return nil, ErrImageTooLarge
	}

	// Serialize quota check and write for the local Provider so concurrent
	// requests cannot independently pass the same remaining quota.
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.quota.CheckQuota(userID, int64(len(data))); err != nil {
		return nil, err
	}
	if _, err := s.storage.EnsureLayout(userID); err != nil {
		return nil, err
	}
	dir, err := s.storage.Path(userID, ImageDir, ContentImageDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name, err := randomImageName(optimized.Extension)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return &ContentImage{
		FileURL:   ContentImageURLPrefix + "/" + userID + "/" + name,
		FileName:  name,
		FileSize:  int64(len(data)),
		MimeType:  optimized.MimeType,
		Width:     optimized.Width,
		Height:    optimized.Height,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ListOwned returns the newest content images that belong to one authenticated
// owner. Content images are compatibility assets referenced from published
// posts, so this is deliberately an inventory only: it neither moves nor
// deletes files and never exposes local filesystem paths.
func (s *ContentImageStore) ListOwned(userID string) ([]ContentImage, error) {
	if s == nil || !SafeSegment(userID) {
		return nil, ErrUnsafePath
	}
	dir, err := s.storage.Path(userID, ImageDir, ContentImageDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []ContentImage{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]ContentImage, 0, len(entries))
	for _, entry := range entries {
		// Do not follow links or expose unrecognised files that might have been
		// placed in a legacy directory outside the upload flow.
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !SafeSegment(entry.Name()) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		mimeType, ok := contentImageMimeType(entry.Name())
		if !ok {
			continue
		}
		items = append(items, ContentImage{
			FileURL:   ContentImageURLPrefix + "/" + userID + "/" + entry.Name(),
			FileName:  entry.Name(),
			FileSize:  info.Size(),
			MimeType:  mimeType,
			CreatedAt: info.ModTime().UTC(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].FileName > items[j].FileName
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if len(items) > maxListedContentImages {
		items = items[:maxListedContentImages]
	}
	return items, nil
}

func (s *ContentImageStore) Path(userID, fileName string) (string, error) {
	if s == nil || !SafeSegment(userID) || !SafeSegment(fileName) {
		return "", ErrUnsafePath
	}
	path, err := s.storage.Path(userID, ImageDir, ContentImageDir, fileName)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrImageNotFound
		}
		return "", err
	}
	return path, nil
}

func canonicalImageExtension(_ string, mimeType string) (string, error) {
	switch mimeType {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/gif":
		return ".gif", nil
	case "image/webp":
		return ".webp", nil
	default:
		return "", ErrImageUnsupported
	}
}

func randomImageName(ext string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random) + ext, nil
}

func contentImageMimeType(name string) (string, bool) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".png":
		return "image/png", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	default:
		return "", false
	}
}
