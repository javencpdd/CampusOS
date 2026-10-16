package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	PluginV5ManifestFile    = "plugin.json"
	PluginV5MaxPackageBytes = 64 << 20
	PluginV5MaxFileBytes    = 32 << 20
	PluginV5MaxPackageFiles = 1024
)

var (
	ErrPluginV5PackageInvalid   = errors.New("plugin.package_invalid")
	ErrPluginV5PackageDigest    = errors.New("plugin.package_digest_mismatch")
	ErrPluginV5PackageSignature = errors.New("plugin.package_signature_invalid")
	ErrPluginV5PackageConflict  = errors.New("plugin.release_digest_conflict")
	ErrPluginV5LegacyProtocol   = errors.New("plugin.legacy_protocol_removed")
)

// PluginV5Package is a captured byte snapshot. It grants no installation,
// runtime, UI, Host API or resource access. The file map never escapes.
type PluginV5Package struct {
	Manifest  *PluginV5Manifest `json:"manifest"`
	Digest    string            `json:"package_digest"`
	Size      int               `json:"size"`
	FileCount int               `json:"file_count"`
	files     map[string][]byte
}

func v5BytesDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func v5ReleasePath(name string) bool {
	return v5PackagePath(name) && (name == PluginV5ManifestFile || name == "README.md" || name == "LICENSE" || strings.HasPrefix(name, "dist/") || strings.HasPrefix(name, "schemas/"))
}

// CapturePluginV5Directory ships only the manifest, dist, schemas and optional
// documentation. Source/dependencies/local configuration are never archived.
func CapturePluginV5Directory(directory string) (map[string][]byte, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrPluginV5PackageInvalid
	}
	defer root.Close()
	files := map[string][]byte{}
	total := int64(0)
	entriesSeen := 0
	for _, name := range []string{PluginV5ManifestFile, "README.md", "LICENSE", "dist", "schemas"} {
		info, err := root.Lstat(name)
		if os.IsNotExist(err) && name != PluginV5ManifestFile {
			continue
		}
		if err != nil {
			return nil, ErrPluginV5PackageInvalid
		}
		var walk func(string, os.FileInfo) error
		walk = func(rel string, info os.FileInfo) error {
			entriesSeen++
			if entriesSeen > 4096 || !v5PackagePath(rel) || strings.Count(rel, "/") > 32 {
				return ErrPluginV5PackageInvalid
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return ErrPluginV5PackageInvalid
			}
			if info.IsDir() {
				f, err := root.Open(rel)
				if err != nil {
					return ErrPluginV5PackageInvalid
				}
				actual, err := f.Stat()
				if err != nil || !os.SameFile(info, actual) {
					f.Close()
					return ErrPluginV5PackageInvalid
				}
				entries, err := f.ReadDir(PluginV5MaxPackageFiles + 1)
				f.Close()
				if err != nil && err != io.EOF {
					return ErrPluginV5PackageInvalid
				}
				if len(entries) > PluginV5MaxPackageFiles {
					return ErrPluginV5PackageInvalid
				}
				for _, entry := range entries {
					child := rel + "/" + entry.Name()
					childInfo, err := root.Lstat(child)
					if err != nil {
						return ErrPluginV5PackageInvalid
					}
					if err = walk(child, childInfo); err != nil {
						return err
					}
				}
				return nil
			}
			if !info.Mode().IsRegular() || !v5ReleasePath(rel) || len(files) >= PluginV5MaxPackageFiles || info.Size() > PluginV5MaxFileBytes {
				return ErrPluginV5PackageInvalid
			}
			f, err := root.Open(rel)
			if err != nil {
				return ErrPluginV5PackageInvalid
			}
			defer f.Close()
			actual, err := f.Stat()
			if err != nil || !os.SameFile(info, actual) || !actual.Mode().IsRegular() {
				return ErrPluginV5PackageInvalid
			}
			raw, err := io.ReadAll(io.LimitReader(f, PluginV5MaxFileBytes+1))
			if err != nil || len(raw) > PluginV5MaxFileBytes {
				return ErrPluginV5PackageInvalid
			}
			total += int64(len(raw)) + 512 + int64((512-len(raw)%512)%512)
			if total > PluginV5MaxPackageBytes-2048 {
				return ErrPluginV5PackageInvalid
			}
			files[rel] = raw
			return nil
		}
		if err = walk(name, info); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func v5CheckPackageFiles(files map[string][]byte) (*PluginV5Manifest, error) {
	m, err := ValidatePluginV5SourceDeclaration(files[PluginV5ManifestFile])
	if err != nil {
		return nil, err
	}
	check := func(path, digest string) error {
		if !v5ReleasePath(path) || path == PluginV5ManifestFile {
			return ErrPluginV5PackageInvalid
		}
		raw, ok := files[path]
		if !ok || len(raw) == 0 {
			return ErrPluginV5PackageInvalid
		}
		if v5BytesDigest(raw) != digest {
			return ErrPluginV5PackageDigest
		}
		return nil
	}
	for _, ui := range m.UI {
		if err = check(ui.Entrypoint, ui.Digest); err != nil {
			return nil, err
		}
	}
	if m.Backend != nil && m.Backend.Artifact.Kind == "wasm" {
		a := m.Backend.Artifact
		if err = check(a.Path, a.Digest); err != nil {
			return nil, err
		}
		if !bytes.HasPrefix(files[a.Path], []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}) {
			return nil, ErrPluginV5PackageInvalid
		}
	}
	for _, p := range m.Provides {
		if p.ConfigSchema != nil {
			if err = check(p.ConfigSchema.Path, p.ConfigSchema.Digest); err != nil {
				return nil, err
			}
			value, ok := v5JSON(files[p.ConfigSchema.Path])
			if !ok || !v5ConfigurationStructure(map[string]any{"contract": "campusos.plugin-config/v1", "version": "v1", "schema": value, "defaults": map[string]any{}, "selectors": []any{}, "secret_names": []any{}}) {
				return nil, ErrPluginV5PackageInvalid
			}
			wire, err := json.Marshal(v5CanonicalNumbers(value))
			if err != nil {
				return nil, ErrPluginV5PackageInvalid
			}
			var schema pluginV5ConfigSchema
			if json.Unmarshal(wire, &schema) != nil {
				return nil, ErrPluginV5PackageInvalid
			}
			for _, field := range schema.Properties {
				if !validPluginV5ConfigField(field) {
					return nil, ErrPluginV5PackageInvalid
				}
			}
			for _, name := range schema.Required {
				if _, ok := schema.Properties[name]; !ok {
					return nil, ErrPluginV5PackageInvalid
				}
			}
		}
	}
	return m, nil
}

func BuildPluginV5Package(directory string) ([]byte, *PluginV5Package, error) {
	files, err := CapturePluginV5Directory(directory)
	if err != nil {
		return nil, nil, err
	}
	m, err := v5CheckPackageFiles(files)
	if err != nil {
		return nil, nil, err
	}
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw := files[name]
		if err = tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(raw)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			return nil, nil, ErrPluginV5PackageInvalid
		}
		if _, err = tw.Write(raw); err != nil {
			return nil, nil, ErrPluginV5PackageInvalid
		}
	}
	if tw.Close() != nil || gz.Close() != nil || output.Len() > PluginV5MaxPackageBytes {
		return nil, nil, ErrPluginV5PackageInvalid
	}
	raw := output.Bytes()
	return raw, &PluginV5Package{Manifest: m, Digest: v5BytesDigest(raw), Size: len(raw), FileCount: len(files), files: files}, nil
}

func InspectPluginV5Package(raw []byte) (*PluginV5Package, error) {
	if len(raw) == 0 || len(raw) > PluginV5MaxPackageBytes {
		return nil, ErrPluginV5PackageInvalid
	}
	input := bytes.NewReader(raw)
	gz, err := gzip.NewReader(input)
	if err != nil {
		return nil, ErrPluginV5PackageInvalid
	}
	defer gz.Close()
	gz.Multistream(false)
	limited := &io.LimitedReader{R: gz, N: PluginV5MaxPackageBytes + 1}
	tr := tar.NewReader(limited)
	files := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrPluginV5PackageInvalid
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA || h.Size < 0 || h.Size > PluginV5MaxFileBytes || !v5ReleasePath(h.Name) || len(files) >= PluginV5MaxPackageFiles || len(h.PAXRecords) != 0 || h.Linkname != "" {
			return nil, ErrPluginV5PackageInvalid
		}
		if _, duplicate := files[h.Name]; duplicate {
			return nil, ErrPluginV5PackageInvalid
		}
		body, err := io.ReadAll(io.LimitReader(tr, PluginV5MaxFileBytes+1))
		if err != nil || int64(len(body)) != h.Size {
			return nil, ErrPluginV5PackageInvalid
		}
		files[h.Name] = body
	}
	// Drain to verify gzip CRC and reject hidden bytes after tar end markers,
	// concatenated streams and trailing compressed data.
	padding, err := io.ReadAll(limited)
	if err != nil || limited.N <= 0 || input.Len() != 0 || len(bytes.Trim(padding, "\x00")) != 0 {
		return nil, ErrPluginV5PackageInvalid
	}
	m, err := v5CheckPackageFiles(files)
	if err != nil {
		return nil, err
	}
	return &PluginV5Package{Manifest: m, Digest: v5BytesDigest(raw), Size: len(raw), FileCount: len(files), files: files}, nil
}

func ReadPluginV5PackageFile(path string) ([]byte, error) {
	return readPluginV5BoundedFile(path, PluginV5MaxPackageBytes)
}

func ReadPluginV5DocumentFile(path string) ([]byte, error) {
	return readPluginV5BoundedFile(path, PluginV5ManifestMaxBytes)
}

func readPluginV5BoundedFile(path string, limit int64) ([]byte, error) {
	before, err := os.Lstat(filepath.Clean(path))
	if err != nil || !before.Mode().IsRegular() || before.Size() > limit {
		return nil, ErrPluginV5PackageInvalid
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, ErrPluginV5PackageInvalid
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit || !os.SameFile(before, info) {
		return nil, ErrPluginV5PackageInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, ErrPluginV5PackageInvalid
	}
	return raw, nil
}
