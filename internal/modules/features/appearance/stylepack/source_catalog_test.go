package stylepack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpaceStyleCatalogDirectoryReaderRejectsEscapesAndHidesHostErrors(t *testing.T) {
	for _, mode := range []string{"missing_root", "external_link", "broken_link"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "source")
			if mode != "missing_root" {
				if err := os.Mkdir(dir, 0755); err != nil {
					t.Fatal(err)
				}
				bundle := BuildExample("personal-space", "link-test", "Link test", "Title", "Bio", "#2563eb", "#ffffff", "#f8fafc")
				for name, data := range bundle.Files {
					file := filepath.Join(dir, filepath.FromSlash(name))
					if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, []byte(data), 0644); err != nil {
						t.Fatal(err)
					}
				}
				external := filepath.Join(root, "outside.html")
				if mode == "external_link" {
					if err := os.WriteFile(external, []byte("private external bytes"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				preview := filepath.Join(dir, filepath.FromSlash(bundle.Package.Manifest.PreviewImages.Desktop))
				if err := os.Remove(preview); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, preview); err != nil {
					t.Fatal(err)
				}
			}
			pack, result := LoadDirStrict(dir)
			raw, err := json.Marshal(result)
			if err != nil || pack != nil || result.Valid || strings.Contains(string(raw), root) || strings.Contains(string(raw), "private external bytes") {
				t.Fatal("directory reader exposed host path or followed source link", pack, string(raw), err)
			}
		})
	}
}
