package platformlog

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultLogDir = ".campusos/logs"
	maxTailBytes  = 1024 * 1024
)

var ErrUnknownSource = errors.New("unknown platform log source")

type Source struct {
	Key        string     `json:"key"`
	Label      string     `json:"label"`
	Path       string     `json:"path"`
	Exists     bool       `json:"exists"`
	Size       int64      `json:"size"`
	ModifiedAt *time.Time `json:"modified_at,omitempty"`
}

type Line struct {
	Source string `json:"source"`
	Line   string `json:"line"`
}

type Service struct {
	logDir  string
	sources []Source
}

func NewServiceFromEnv() *Service {
	logDir := strings.TrimSpace(os.Getenv("CAMPUSOS_LOG_DIR"))
	if logDir == "" {
		logDir = defaultLogDir
	}
	return NewService(logDir)
}

func NewService(logDir string) *Service {
	if logDir == "" {
		logDir = defaultLogDir
	}
	return &Service{
		logDir: filepath.Clean(logDir),
		sources: []Source{
			{Key: "api", Label: "后端 API", Path: "api.log"},
			{Key: "web", Label: "用户前台 web", Path: "web.log"},
			{Key: "admin", Label: "管理后台 admin", Path: "admin.log"},
			{Key: "docs", Label: "官方文档 docs", Path: "docs.log"},
		},
	}
}

func (s *Service) Sources() []Source {
	sources := make([]Source, 0, len(s.sources))
	for _, source := range s.sources {
		source.Path = s.pathFor(source.Path)
		if info, err := os.Stat(source.Path); err == nil {
			source.Exists = true
			source.Size = info.Size()
			modifiedAt := info.ModTime()
			source.ModifiedAt = &modifiedAt
		}
		sources = append(sources, source)
	}
	return sources
}

func (s *Service) Stream(ctx context.Context, sourceKey string, lines int, follow bool, emit func(Line) error) error {
	source, ok := s.source(sourceKey)
	if !ok {
		return ErrUnknownSource
	}
	path := s.pathFor(source.Path)
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emit(Line{Source: sourceKey, Line: "日志文件还不存在：" + path})
		}
		return err
	}
	defer file.Close()

	if lines < 1 {
		lines = 200
	}
	if lines > 1000 {
		lines = 1000
	}
	if err := emitTail(file, sourceKey, lines, emit); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	return followFile(ctx, file, sourceKey, emit)
}

func (s *Service) source(key string) (Source, bool) {
	for _, source := range s.sources {
		if source.Key == key {
			return source, true
		}
	}
	return Source{}, false
}

func (s *Service) pathFor(name string) string {
	return filepath.Clean(filepath.Join(s.logDir, name))
}

func emitTail(file *os.File, source string, lines int, emit func(Line) error) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	offset := info.Size() - maxTailBytes
	if offset < 0 {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	parts := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if offset > 0 && len(parts) > 0 {
		parts = parts[1:]
	}
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	for _, line := range parts {
		if err := emit(Line{Source: source, Line: line}); err != nil {
			return err
		}
	}
	return nil
}

func followFile(ctx context.Context, file *os.File, source string, emit func(Line) error) error {
	reader := bufio.NewReader(file)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for {
				line, err := reader.ReadString('\n')
				if line != "" {
					if err := emit(Line{Source: source, Line: strings.TrimRight(line, "\n")}); err != nil {
						return err
					}
				}
				if err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					return err
				}
			}
		}
	}
}
