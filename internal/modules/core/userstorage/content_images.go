package storage

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

const (
	ContentImageDir        = "content"
	DefaultMaxContentImage = int64(5 * 1024 * 1024)
	ContentImageURLPrefix  = "/api/v1/content/assets/images"
	contentImageFormSlack  = int64(64 * 1024)
)

var (
	ErrImageTooLarge    = errors.New("content image exceeds the size limit")
	ErrImageUnsupported = errors.New("content image type is unsupported")
	ErrImageNotFound    = errors.New("content image was not found")
)

type ContentImage struct {
	FileURL  string `json:"file_url"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
	MimeType string `json:"mime_type"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
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
	mimeType := http.DetectContentType(data)
	ext, err := canonicalImageExtension(originalName, mimeType)
	if err != nil {
		return nil, err
	}
	width, height := 0, 0
	if mimeType != "image/webp" {
		config, _, decodeErr := image.DecodeConfig(bytes.NewReader(data))
		if decodeErr != nil {
			return nil, ErrImageUnsupported
		}
		width, height = config.Width, config.Height
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
	name, err := randomImageName(ext)
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
		FileURL:  ContentImageURLPrefix + "/" + userID + "/" + name,
		FileName: name,
		FileSize: int64(len(data)),
		MimeType: mimeType,
		Width:    width,
		Height:   height,
	}, nil
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
