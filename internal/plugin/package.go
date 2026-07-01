package plugin

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	DefaultPluginsDir      = "examples/plugins"
	PluginPackageExtension = ".campusos-plugin.tar.gz"
)

type PackageInfo struct {
	Manifest    *Manifest `json:"manifest"`
	PluginDir   string    `json:"plugin_dir,omitempty"`
	PackagePath string    `json:"package_path,omitempty"`
}

func PluginsDirFromEnv() string {
	if dir := os.Getenv("PLUGINS_DIR"); dir != "" {
		return dir
	}
	return DefaultPluginsDir
}

func PackagePlugin(pluginDir, outputPath string) (*PackageInfo, error) {
	manifest, err := ValidatePluginPackageDir(pluginDir)
	if err != nil {
		return nil, err
	}
	if outputPath == "" {
		outputPath = DefaultPluginPackagePath(pluginDir, manifest)
	}
	if err := createPluginArchive(pluginDir, outputPath); err != nil {
		return nil, err
	}
	return &PackageInfo{
		Manifest:    manifest,
		PluginDir:   filepath.Clean(pluginDir),
		PackagePath: outputPath,
	}, nil
}

func DefaultPluginPackagePath(pluginDir string, manifest *Manifest) string {
	name := "plugin"
	version := "0.0.0"
	if manifest != nil {
		if manifest.Name != "" {
			name = manifest.Name
		}
		if manifest.Version != "" {
			version = manifest.Version
		}
	}
	return filepath.Join(filepath.Dir(filepath.Clean(pluginDir)), fmt.Sprintf("%s-%s%s", name, version, PluginPackageExtension))
}

func InstallPluginPackage(packagePath, pluginsDir string, replace bool) (*PackageInfo, error) {
	if pluginsDir == "" {
		pluginsDir = PluginsDirFromEnv()
	}
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return nil, err
	}

	tempDir, err := os.MkdirTemp(pluginsDir, ".install-*")
	if err != nil {
		return nil, err
	}
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.RemoveAll(tempDir)
		}
	}()

	if err := ExtractPluginPackage(packagePath, tempDir); err != nil {
		return nil, err
	}
	manifest, err := ValidatePluginPackageDir(tempDir)
	if err != nil {
		return nil, err
	}
	targetDir, err := pluginTargetDir(pluginsDir, manifest.Name)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(targetDir); err == nil {
		if !replace {
			return nil, fmt.Errorf("%s already exists; use replace to overwrite", targetDir)
		}
		if err := os.RemoveAll(targetDir); err != nil {
			return nil, err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.Rename(tempDir, targetDir); err != nil {
		return nil, err
	}
	keepTemp = true

	return &PackageInfo{
		Manifest:  manifest,
		PluginDir: targetDir,
	}, nil
}

func InspectPluginPackage(packagePath string) (*Manifest, error) {
	tempDir, err := os.MkdirTemp("", "campusos-plugin-inspect-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	if err := ExtractPluginPackage(packagePath, tempDir); err != nil {
		return nil, err
	}
	return ValidatePluginPackageDir(tempDir)
}

func ExtractPluginPackage(packagePath, targetDir string) error {
	file, err := os.Open(packagePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()

	targetRoot, err := filepath.Abs(filepath.Clean(targetDir))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return err
	}

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		cleanName, err := cleanArchiveName(header.Name)
		if err != nil {
			return err
		}
		if shouldSkipPluginPackagePath(cleanName, header.FileInfo().IsDir()) {
			return fmt.Errorf("archive contains forbidden path: %s", header.Name)
		}
		targetPath := filepath.Join(targetRoot, filepath.FromSlash(cleanName))
		targetAbs, err := filepath.Abs(filepath.Clean(targetPath))
		if err != nil {
			return err
		}
		if targetAbs != targetRoot && !strings.HasPrefix(targetAbs, targetRoot+string(os.PathSeparator)) {
			return fmt.Errorf("archive path escapes target: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetAbs, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(targetAbs, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry type for %s", header.Name)
		}
	}
}

func ValidatePluginPackageDir(pluginDir string) (*Manifest, error) {
	manifest, err := LoadManifest(filepath.Join(pluginDir, "plugin.yaml"))
	if err != nil {
		return nil, err
	}
	if err := ValidatePluginName(manifest.Name); err != nil {
		return nil, err
	}
	if manifest.Runtime == "wasm" {
		modulePath := "plugin.wasm"
		if raw, ok := manifest.Config["module"]; ok {
			if value, ok := raw.(string); ok && value != "" {
				modulePath = value
			}
		}
		if err := requireRelativePathInside(pluginDir, modulePath); err != nil {
			return nil, fmt.Errorf("invalid wasm module path: %w", err)
		}
		if _, err := os.Stat(filepath.Join(pluginDir, modulePath)); err != nil {
			return nil, fmt.Errorf("wasm module %s: %w", modulePath, err)
		}
	}
	return manifest, nil
}

func ValidatePluginName(name string) error {
	if name == "" {
		return errors.New("plugin name is required")
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("plugin name %q can only contain lowercase letters, numbers, hyphen, and underscore", name)
	}
	return nil
}

func createPluginArchive(pluginDir, outputPath string) error {
	pluginRoot, err := filepath.Abs(filepath.Clean(pluginDir))
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(filepath.Clean(outputPath))
	if err != nil {
		return err
	}
	outputFile, err := os.Create(outputAbs)
	if err != nil {
		return err
	}
	defer outputFile.Close()

	gz := gzip.NewWriter(outputFile)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	return filepath.WalkDir(pluginRoot, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		fileAbs, err := filepath.Abs(filePath)
		if err != nil {
			return err
		}
		if fileAbs == outputAbs {
			return nil
		}
		rel, err := filepath.Rel(pluginRoot, fileAbs)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldSkipPluginPackagePath(rel, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(fileAbs)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func cleanArchiveName(name string) (string, error) {
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || clean == ".." || path.IsAbs(clean) {
		return "", fmt.Errorf("unsafe archive path: %s", name)
	}
	return clean, nil
}

func shouldSkipPluginPackagePath(rel string, isDir bool) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case ".git", "data", "node_modules":
		return true
	}
	if !isDir {
		base := filepath.Base(rel)
		if base == ".DS_Store" || strings.HasSuffix(base, ".log") || strings.HasSuffix(base, ".tmp") {
			return true
		}
	}
	return false
}

func requireRelativePathInside(rootDir, relPath string) error {
	if relPath == "" || filepath.IsAbs(relPath) {
		return fmt.Errorf("path must be relative: %s", relPath)
	}
	rootAbs, err := filepath.Abs(filepath.Clean(rootDir))
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(filepath.Clean(filepath.Join(rootAbs, relPath)))
	if err != nil {
		return err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(os.PathSeparator)) {
		return fmt.Errorf("path escapes plugin directory: %s", relPath)
	}
	return nil
}

func pluginTargetDir(pluginsDir, pluginName string) (string, error) {
	if err := ValidatePluginName(pluginName); err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(filepath.Clean(pluginsDir))
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Clean(filepath.Join(rootAbs, pluginName)))
	if err != nil {
		return "", err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("plugin target escapes plugins directory: %s", pluginName)
	}
	return targetAbs, nil
}
