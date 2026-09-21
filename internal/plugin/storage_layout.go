package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// StorageLayout is the only filesystem layout CampusOS assigns to an external
// plugin. It is deliberately separate from plugin packages and platform data:
// a plugin gets one private directory, an optional SQLite database and one
// non-secret config file, never a platform database path or credential.
type StorageLayout struct {
	RootDir    string
	DataDir    string
	SQLitePath string
	ConfigPath string
}

// PreparePluginStorage creates a private, root-contained storage layout. The
// caller must still apply OS/container isolation for process plugins; this
// helper prevents accidental path traversal in the host-managed layout.
func PreparePluginStorage(rootDir, pluginName string) (StorageLayout, error) {
	if err := ValidatePluginStorageName(pluginName); err != nil {
		return StorageLayout{}, err
	}
	if strings.TrimSpace(rootDir) == "" {
		rootDir = "data/plugin_data"
	}
	root, err := filepath.Abs(filepath.Clean(rootDir))
	if err != nil {
		return StorageLayout{}, err
	}
	dataDir, err := containedPluginStoragePath(root, pluginName)
	if err != nil {
		return StorageLayout{}, err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return StorageLayout{}, err
	}
	if info, err := os.Lstat(dataDir); err != nil {
		return StorageLayout{}, err
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return StorageLayout{}, fmt.Errorf("plugin storage directory is not a real directory: %s", pluginName)
	}
	// Best effort on Windows; meaningful on Unix. Do not fail merely because a
	// mounted development volume cannot represent Unix permissions.
	_ = os.Chmod(root, 0o700)
	_ = os.Chmod(dataDir, 0o700)
	return StorageLayout{
		RootDir:    root,
		DataDir:    dataDir,
		SQLitePath: filepath.Join(dataDir, "plugin.db"),
		ConfigPath: filepath.Join(dataDir, "config.json"),
	}, nil
}

const maxPluginConfigBytes = 256 * 1024

// ReadPluginFileConfig reads the optional, non-secret JSON configuration of
// one plugin. Secrets deliberately belong to the platform Secret service, not
// to this file. A symlink is rejected so an external package cannot redirect
// its standard config path to platform configuration.
func ReadPluginFileConfig(rootDir, pluginName string) ([]byte, bool, error) {
	layout, err := PreparePluginStorage(rootDir, pluginName)
	if err != nil {
		return nil, false, err
	}
	info, err := os.Lstat(layout.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxPluginConfigBytes {
		return nil, false, fmt.Errorf("plugin config is not a safe regular JSON file")
	}
	data, err := os.ReadFile(layout.ConfigPath)
	if err != nil {
		return nil, false, err
	}
	if !json.Valid(data) {
		return nil, false, errors.New("plugin config is not valid JSON")
	}
	return append([]byte(nil), data...), true, nil
}

// WritePluginFileConfig atomically replaces the standard non-secret JSON
// configuration. The size limit and JSON validation keep this path from
// becoming an unbounded arbitrary-file channel.
func WritePluginFileConfig(rootDir, pluginName string, data []byte) error {
	if len(data) > maxPluginConfigBytes || !json.Valid(data) {
		return errors.New("plugin config must be valid JSON no larger than 256 KiB")
	}
	layout, err := PreparePluginStorage(rootDir, pluginName)
	if err != nil {
		return err
	}
	if info, statErr := os.Lstat(layout.ConfigPath); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("plugin config path cannot be a symlink")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	file, err := os.CreateTemp(layout.DataDir, ".config-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(bytes.TrimSpace(data)); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, layout.ConfigPath); err != nil {
		return err
	}
	_ = os.Chmod(layout.ConfigPath, 0o600)
	return nil
}

func containedPluginStoragePath(rootDir, pluginName string) (string, error) {
	target := filepath.Join(rootDir, pluginName)
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	if targetAbs != rootDir && !strings.HasPrefix(targetAbs, rootDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("plugin storage path escapes root: %s", pluginName)
	}
	return targetAbs, nil
}

// ValidatePluginStorageName permits the manifest names used by CampusOS (for
// example campusos.pdf-viewer) while rejecting separators, traversal and names
// that are unsafe as a single directory component on Windows or Linux.
func ValidatePluginStorageName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("plugin name is required")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid plugin name for storage: %q", name)
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("invalid plugin name for storage: %q", name)
	}
	return nil
}
