package platformlog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSourcesReportsKnownLogFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "api.log"), []byte("ready\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	svc := NewService(dir)

	sources := svc.Sources()
	if len(sources) != 4 {
		t.Fatalf("expected four log sources, got %d", len(sources))
	}
	if sources[0].Key != "api" || !sources[0].Exists || sources[0].Size == 0 {
		t.Fatalf("unexpected api source metadata: %#v", sources[0])
	}
	if sources[3].Key != "docs" || sources[3].Label != "官方文档 docs" {
		t.Fatalf("unexpected docs source metadata: %#v", sources[3])
	}
}

func TestStreamEmitsTailLines(t *testing.T) {
	dir := t.TempDir()
	content := "one\ntwo\nthree\n"
	if err := os.WriteFile(filepath.Join(dir, "api.log"), []byte(content), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	svc := NewService(dir)

	var lines []Line
	err := svc.Stream(context.Background(), "api", 2, false, func(line Line) error {
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatalf("stream log: %v", err)
	}
	if len(lines) != 2 || lines[0].Line != "two" || lines[1].Line != "three" {
		t.Fatalf("unexpected tail lines: %#v", lines)
	}
}
