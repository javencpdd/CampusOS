package plugin

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	modulecatalog "github.com/campusos/CampusOS/modules"
)

const (
	DefaultPluginsDir      = "data/plugins"
	DefaultPluginDataDir   = "data/plugin_data"
	PluginPackageExtension = ".campusos-plugin.tar.gz"
)

type VersionSnapshot struct {
	ID          string    `json:"id"`
	PluginName  string    `json:"plugin_name"`
	Version     string    `json:"version"`
	Checksum    string    `json:"checksum"`
	PackageSize int64     `json:"package_size"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
	PackagePath string    `json:"package_path,omitempty"`
}

type PackageInfo struct {
	Manifest    *Manifest `json:"manifest"`
	PluginDir   string    `json:"plugin_dir,omitempty"`
	PackagePath string    `json:"package_path,omitempty"`
	Checksum    string    `json:"checksum,omitempty"`
	PackageSize int64     `json:"package_size,omitempty"`
}

type PackagePrecheck struct {
	Manifest                *Manifest `json:"manifest,omitempty"`
	Checksum                string    `json:"checksum"`
	PackageSize             int64     `json:"package_size"`
	Files                   []string  `json:"files"`
	Permissions             []string  `json:"permissions"`
	Events                  []string  `json:"events"`
	Conflict                bool      `json:"conflict"`
	TargetDir               string    `json:"target_dir,omitempty"`
	Allowed                 bool      `json:"allowed"`
	Warnings                []string  `json:"warnings,omitempty"`
	Errors                  []string  `json:"errors,omitempty"`
	RiskLevel               string    `json:"risk_level"`
	RiskScore               int       `json:"risk_score"`
	RiskReasons             []string  `json:"risk_reasons,omitempty"`
	ExistingVersion         string    `json:"existing_version,omitempty"`
	ImportVersion           string    `json:"import_version,omitempty"`
	VersionChange           string    `json:"version_change,omitempty"`
	AddedPermissions        []string  `json:"added_permissions,omitempty"`
	RemovedPermissions      []string  `json:"removed_permissions,omitempty"`
	RequiresReauthorization bool      `json:"requires_reauthorization"`
	DataSchemaChange        string    `json:"data_schema_change,omitempty"`
	SignatureStatus         string    `json:"signature_status"`
	SignatureFiles          []string  `json:"signature_files,omitempty"`
}

func PluginsDirFromEnv() string {
	if dir := os.Getenv("PLUGINS_DIR"); dir != "" {
		return dir
	}
	return DefaultPluginsDir
}

func PluginDataDirFromEnv() string {
	if dir := os.Getenv("PLUGIN_DATA_DIR"); dir != "" {
		return dir
	}
	return DefaultPluginDataDir
}

func CreatePluginSnapshot(pluginDir, pluginDataDir, source string) (*VersionSnapshot, error) {
	manifest, err := ValidatePluginPackageDir(pluginDir)
	if err != nil {
		return nil, err
	}
	if pluginDataDir == "" {
		pluginDataDir = PluginDataDirFromEnv()
	}
	id := time.Now().UTC().Format("20060102T150405.000000000Z")
	snapshotDir := filepath.Join(pluginDataDir, manifest.Name, "version-snapshots", id)
	if err := os.MkdirAll(snapshotDir, 0o750); err != nil {
		return nil, err
	}
	packagePath := filepath.Join(snapshotDir, manifest.Name+"-"+manifest.Version+PluginPackageExtension)
	info, err := PackagePlugin(pluginDir, packagePath)
	if err != nil {
		_ = os.RemoveAll(snapshotDir)
		return nil, err
	}
	metadata := &VersionSnapshot{
		ID: id, PluginName: manifest.Name, Version: manifest.Version,
		Checksum: info.Checksum, PackageSize: info.PackageSize, Source: source,
		CreatedAt: time.Now().UTC(), PackagePath: filepath.Base(packagePath),
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "metadata.json"), append(data, '\n'), 0o640); err != nil {
		return nil, err
	}
	return metadata, nil
}

func ListPluginSnapshots(pluginName, pluginDataDir string) ([]VersionSnapshot, error) {
	if err := ValidatePluginName(pluginName); err != nil {
		return nil, err
	}
	if pluginDataDir == "" {
		pluginDataDir = PluginDataDirFromEnv()
	}
	root := filepath.Join(pluginDataDir, pluginName, "version-snapshots")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []VersionSnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]VersionSnapshot, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name(), "metadata.json"))
		if err != nil {
			return nil, fmt.Errorf("read snapshot %s metadata: %w", entry.Name(), err)
		}
		var snapshot VersionSnapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return nil, fmt.Errorf("decode snapshot %s metadata: %w", entry.Name(), err)
		}
		if snapshot.PluginName != pluginName || snapshot.ID != entry.Name() {
			return nil, fmt.Errorf("snapshot %s metadata identity mismatch", entry.Name())
		}
		if snapshot.PackagePath == "" {
			return nil, fmt.Errorf("snapshot %s has no package path", entry.Name())
		}
		snapshotRoot := filepath.Join(root, entry.Name())
		packagePath := snapshot.PackagePath
		if !filepath.IsAbs(packagePath) {
			packagePath = filepath.Join(snapshotRoot, packagePath)
		}
		relPackage, err := filepath.Rel(snapshotRoot, packagePath)
		if err != nil || relPackage == ".." || strings.HasPrefix(relPackage, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("snapshot %s package path escapes its snapshot directory", entry.Name())
		}
		checksum, size, err := FileSHA256(packagePath)
		if err != nil {
			return nil, fmt.Errorf("verify snapshot %s: %w", entry.Name(), err)
		}
		if checksum != snapshot.Checksum || size != snapshot.PackageSize {
			return nil, fmt.Errorf("snapshot %s checksum or size mismatch", entry.Name())
		}
		snapshot.PackagePath = packagePath
		result = append(result, snapshot)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func FindPluginSnapshot(pluginName, snapshotID, pluginDataDir string) (*VersionSnapshot, error) {
	snapshots, err := ListPluginSnapshots(pluginName, pluginDataDir)
	if err != nil {
		return nil, err
	}
	for i := range snapshots {
		if snapshots[i].ID == snapshotID {
			return &snapshots[i], nil
		}
	}
	return nil, fmt.Errorf("snapshot %q not found for plugin %q", snapshotID, pluginName)
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
	checksum, size, err := FileSHA256(outputPath)
	if err != nil {
		return nil, err
	}
	return &PackageInfo{
		Manifest:    manifest,
		PluginDir:   filepath.Clean(pluginDir),
		PackagePath: outputPath,
		Checksum:    checksum,
		PackageSize: size,
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

	checksum, size, err := FileSHA256(packagePath)
	if err != nil {
		return nil, err
	}
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
	backupDir := ""
	if _, err := os.Stat(targetDir); err == nil {
		if !replace {
			return nil, fmt.Errorf("%s already exists; use replace to overwrite", targetDir)
		}
		// Rename the current package out of the way rather than deleting it.
		// A failed replacement can therefore restore the exact prior files.
		backupDir = targetDir + ".rollback-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano())
		if err := os.Rename(targetDir, backupDir); err != nil {
			return nil, err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.Rename(tempDir, targetDir); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, targetDir)
		}
		return nil, err
	}
	keepTemp = true
	if backupDir != "" {
		// The new package is already fully installed. A retained backup is safe
		// and can be cleaned by an operator; never report this atomic install as
		// failed after the target switch succeeded.
		_ = os.RemoveAll(backupDir)
	}

	return &PackageInfo{
		Manifest:    manifest,
		PluginDir:   targetDir,
		PackagePath: filepath.Clean(packagePath),
		Checksum:    checksum,
		PackageSize: size,
	}, nil
}

func PrecheckPluginPackage(packagePath, pluginsDir string) (*PackagePrecheck, error) {
	if pluginsDir == "" {
		pluginsDir = PluginsDirFromEnv()
	}
	checksum, size, err := FileSHA256(packagePath)
	if err != nil {
		return nil, err
	}
	result := &PackagePrecheck{
		Checksum:    checksum,
		PackageSize: size,
		Allowed:     true,
	}

	tempDir, err := os.MkdirTemp("", "campusos-plugin-precheck-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	if err := ExtractPluginPackage(packagePath, tempDir); err != nil {
		result.Allowed = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	files, err := listPackageFiles(tempDir)
	if err != nil {
		result.Allowed = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	result.Files = files
	result.SignatureStatus, result.SignatureFiles = detectSignatureFiles(files)

	manifest, err := ValidatePluginPackageDir(tempDir)
	if err != nil {
		result.Allowed = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	result.Manifest = manifest
	trust, trustErr := LoadPluginTrustStore(filepath.Join("data", "config", "plugin-trust-keys.json"))
	if trustErr != nil {
		result.Allowed = false
		result.Errors = append(result.Errors, fmt.Sprintf("load plugin trust store: %v", trustErr))
		return result, nil
	}
	if status, verifyErr := VerifyPluginDirectorySignature(tempDir, trust); verifyErr != nil {
		result.SignatureStatus = status
		result.Allowed = false
		result.Errors = append(result.Errors, verifyErr.Error())
		return result, nil
	} else {
		result.SignatureStatus = status
	}
	result.Events = append([]string(nil), manifest.Events.Subscribe...)
	result.Permissions = flattenPermissions(manifest.Permissions)
	result.ImportVersion = manifest.Version
	if manifest.IsSystemLevel() {
		result.Allowed = false
		result.Errors = append(result.Errors, "system-level plugins must be deployed with server code and take effect after restart")
	}
	for _, permission := range manifest.Permissions.API {
		for _, action := range permission.Actions {
			if !IsKnownPermission(permission.Resource, action) {
				result.Allowed = false
				result.Errors = append(result.Errors, fmt.Sprintf("permission %s/%s is not in the Host API permission catalog", permission.Resource, action))
			}
		}
	}

	targetDir, err := pluginTargetDir(pluginsDir, manifest.Name)
	if err != nil {
		result.Allowed = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	result.TargetDir = targetDir
	if _, err := os.Stat(targetDir); err == nil {
		result.Conflict = true
		existingManifest, manifestErr := LoadManifest(filepath.Join(targetDir, "plugin.yaml"))
		if manifestErr == nil && existingManifest != nil {
			result.ExistingVersion = existingManifest.Version
			result.AddedPermissions, result.RemovedPermissions = permissionDiff(flattenApprovalPermissions(existingManifest.Permissions), flattenApprovalPermissions(manifest.Permissions))
			for _, permission := range result.AddedPermissions {
				if strings.HasPrefix(permission, "user:") {
					result.RequiresReauthorization = true
					break
				}
			}
			result.DataSchemaChange = schemaVersionChange(existingManifest.Release.DataSchemaVersion, manifest.Release.DataSchemaVersion)
		} else {
			result.ExistingVersion = existingPluginVersion(targetDir)
			result.Warnings = append(result.Warnings, "installed manifest could not be read for permission and data schema diff")
		}
		result.VersionChange = compareVersionChange(result.ExistingVersion, result.ImportVersion)
		result.Warnings = append(result.Warnings, "plugin with the same name already exists; import requires replace=true")
		switch result.VersionChange {
		case "same":
			result.Warnings = append(result.Warnings, "imported plugin version is the same as the installed version")
		case "downgrade":
			result.Warnings = append(result.Warnings, "imported plugin version is older than the installed version")
		}
		if len(result.AddedPermissions) > 0 {
			result.Warnings = append(result.Warnings, "upgrade adds permissions; review the permission diff before import")
		}
		if result.RequiresReauthorization {
			result.Warnings = append(result.Warnings, "upgrade expands user permissions and requires users to authorize the new version")
		}
		if result.DataSchemaChange != "same" && result.DataSchemaChange != "" {
			result.Warnings = append(result.Warnings, "managed data schema version changes from "+result.DataSchemaChange)
		}
	} else if err != nil && !os.IsNotExist(err) {
		result.Allowed = false
		result.Errors = append(result.Errors, err.Error())
	} else {
		result.VersionChange = "new"
	}
	if len(result.Events) == 0 {
		result.Warnings = append(result.Warnings, "plugin does not subscribe to any events")
	}
	if len(result.Permissions) == 0 {
		result.Warnings = append(result.Warnings, "plugin declares no Host API permissions")
	}
	if manifest.Release.SignatureRequired && result.SignatureStatus != "verified" {
		result.Allowed = false
		result.Errors = append(result.Errors, "plugin manifest requires a signature trusted by this CampusOS instance")
	} else if result.SignatureStatus == "unsigned" {
		result.Warnings = append(result.Warnings, "plugin package is unsigned; administrators must review it before import")
	} else if result.SignatureStatus == "untrusted" {
		result.Warnings = append(result.Warnings, "plugin signature is valid in structure but its key is not trusted by this CampusOS instance")
	}
	result.RiskLevel, result.RiskScore, result.RiskReasons = assessPackageRisk(manifest, result.PackageSize, result.Conflict, result.VersionChange)
	if result.RiskLevel == "high" {
		result.Warnings = append(result.Warnings, "high-risk plugin package; review permissions and runtime carefully before import")
	}
	return result, nil
}

func FileSHA256(filePath string) (string, int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), size, nil
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
	if err := ValidateExternalPluginManifest(manifest); err != nil {
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

// ValidateExternalPluginManifest is shared by directory discovery and package
// import. Legacy builtin manifests remain parseable by migration tooling, but
// the external Plugin Platform never installs them.
func ValidateExternalPluginManifest(manifest *Manifest) error {
	if manifest == nil {
		return errors.New("plugin manifest is required")
	}
	if err := ValidatePluginName(manifest.Name); err != nil {
		return err
	}
	if manifest.Runtime == "builtin" {
		return errors.New("runtime builtin is reserved for legacy inspection; use modules/<kind>/<id>/module.yaml for compiled CampusOS modules")
	}
	catalog, err := modulecatalog.Load()
	if err != nil {
		return fmt.Errorf("load compiled module catalog: %w", err)
	}
	if catalog.IsReservedExtensionName(manifest.Name) {
		return fmt.Errorf("plugin name %q is reserved by a compiled CampusOS module or compatibility alias", manifest.Name)
	}
	return nil
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

func listPackageFiles(rootDir string) ([]string, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(rootDir))
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	err = filepath.WalkDir(rootAbs, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, filePath)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files, err
}

func flattenPermissions(perms PermissionsConfig) []string {
	flattened := make([]string, 0)
	for _, perm := range perms.API {
		resource := strings.TrimSpace(perm.Resource)
		if resource == "" {
			continue
		}
		for _, action := range perm.Actions {
			action = strings.TrimSpace(action)
			if action != "" {
				flattened = append(flattened, resource+":"+action)
			}
		}
	}
	sort.Strings(flattened)
	return flattened
}

func flattenApprovalPermissions(perms PermissionsConfig) []string {
	flattened := make([]string, 0)
	for _, permission := range perms.API {
		for _, action := range permission.Actions {
			if resource, value := strings.TrimSpace(permission.Resource), strings.TrimSpace(action); resource != "" && value != "" {
				flattened = append(flattened, "api:"+resource+":"+value)
			}
		}
	}
	for _, permission := range perms.User {
		for _, action := range permission.Actions {
			if resource, value := strings.TrimSpace(permission.Resource), strings.TrimSpace(action); resource != "" && value != "" {
				flattened = append(flattened, "user:"+resource+":"+value)
			}
		}
	}
	return uniqueStrings(flattened)
}

func permissionDiff(existing, incoming []string) (added, removed []string) {
	existingSet := make(map[string]bool, len(existing))
	incomingSet := make(map[string]bool, len(incoming))
	for _, permission := range existing {
		existingSet[permission] = true
	}
	for _, permission := range incoming {
		incomingSet[permission] = true
		if !existingSet[permission] {
			added = append(added, permission)
		}
	}
	for _, permission := range existing {
		if !incomingSet[permission] {
			removed = append(removed, permission)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func schemaVersionChange(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if existing == incoming {
		return "same"
	}
	if existing == "" {
		existing = "unspecified"
	}
	if incoming == "" {
		incoming = "unspecified"
	}
	return existing + " -> " + incoming
}

func detectSignatureFiles(files []string) (string, []string) {
	signatures := []string{}
	for _, file := range files {
		normalized := strings.ToLower(filepath.ToSlash(file))
		switch normalized {
		case "signature", "signature.sig", "plugin.sig", "plugin.yaml.sig", ".campusos/signature.json":
			signatures = append(signatures, file)
		default:
			if strings.HasSuffix(normalized, ".sig") || strings.HasSuffix(normalized, ".minisig") {
				signatures = append(signatures, file)
			}
		}
	}
	sort.Strings(signatures)
	if len(signatures) == 0 {
		return "unsigned", nil
	}
	return "present_unverified", signatures
}

func existingPluginVersion(pluginDir string) string {
	manifest, err := LoadManifest(filepath.Join(pluginDir, "plugin.yaml"))
	if err != nil || manifest == nil {
		return ""
	}
	return manifest.Version
}

func compareVersionChange(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if existing == "" {
		return "new"
	}
	if incoming == "" {
		return "unknown"
	}
	cmp, ok := compareDottedVersion(existing, incoming)
	if !ok {
		return "unknown"
	}
	switch {
	case cmp < 0:
		return "upgrade"
	case cmp > 0:
		return "downgrade"
	default:
		return "same"
	}
}

func compareDottedVersion(left, right string) (int, bool) {
	leftParts, ok := versionParts(left)
	if !ok {
		return 0, false
	}
	rightParts, ok := versionParts(right)
	if !ok {
		return 0, false
	}
	maxLen := len(leftParts)
	if len(rightParts) > maxLen {
		maxLen = len(rightParts)
	}
	for i := 0; i < maxLen; i++ {
		var l, r int
		if i < len(leftParts) {
			l = leftParts[i]
		}
		if i < len(rightParts) {
			r = rightParts[i]
		}
		if l > r {
			return 1, true
		}
		if l < r {
			return -1, true
		}
	}
	return 0, true
}

func versionParts(value string) ([]int, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" {
		return nil, false
	}
	tokens := strings.FieldsFunc(value, func(r rune) bool {
		return r == '.' || r == '-' || r == '_'
	})
	if len(tokens) == 0 {
		return nil, false
	}
	parts := make([]int, 0, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		var parsed int
		if _, err := fmt.Sscanf(token, "%d", &parsed); err != nil {
			return nil, false
		}
		parts = append(parts, parsed)
	}
	return parts, len(parts) > 0
}

func assessPackageRisk(manifest *Manifest, packageSize int64, conflict bool, versionChange string) (string, int, []string) {
	if manifest == nil {
		return "unknown", 0, nil
	}
	score := 0
	reasons := []string{}
	switch manifest.Runtime {
	case "grpc":
		score += 25
		reasons = append(reasons, "grpc runtime can execute a local plugin process")
	case "wasm":
		score += 12
		reasons = append(reasons, "wasm runtime has sandboxing but still needs Host API permission review")
	case "builtin":
		score += 8
		reasons = append(reasons, "builtin runtime runs inside the host feature boundary")
	}
	for _, event := range manifest.Events.Subscribe {
		if strings.HasSuffix(event, ".before") {
			score += 12
			reasons = append(reasons, "before-event subscription can block host workflows: "+event)
		}
	}
	for _, perm := range manifest.Permissions.API {
		resource := strings.TrimSpace(perm.Resource)
		for _, action := range perm.Actions {
			action = strings.TrimSpace(action)
			weight, reason := permissionRisk(resource, action)
			score += weight
			if reason != "" {
				reasons = append(reasons, reason)
			}
		}
	}
	switch strings.TrimSpace(manifest.Storage.Type) {
	case "postgresql":
		score += 20
		reasons = append(reasons, "plugin requests PostgreSQL storage")
	case "sqlite":
		score += 5
		reasons = append(reasons, "plugin requests local SQLite storage")
	}
	if packageSize > 10*1024*1024 {
		score += 8
		reasons = append(reasons, "plugin package is larger than 10 MB")
	}
	if conflict {
		score += 8
		reasons = append(reasons, "plugin import will replace an installed plugin")
	}
	if versionChange == "downgrade" {
		score += 12
		reasons = append(reasons, "plugin import is a version downgrade")
	}
	level := "low"
	switch {
	case score >= 60:
		level = "high"
	case score >= 25:
		level = "medium"
	}
	sort.Strings(reasons)
	return level, score, dedupeStrings(reasons)
}

func permissionRisk(resource, action string) (int, string) {
	if resource == "*" || action == "*" {
		return 50, "wildcard Host API permission: " + resource + ":" + action
	}
	switch action {
	case "delete", "manage", "suspend":
		return 25, "sensitive Host API action: " + resource + ":" + action
	case "write", "update", "create":
		switch resource {
		case "role", "user", "config", "plugin", "homepage", "thread", "richtext_article":
			return 20, "write access to sensitive resource: " + resource + ":" + action
		default:
			return 12, "write Host API permission: " + resource + ":" + action
		}
	case "read":
		switch resource {
		case "role", "user", "config":
			return 8, "read access to sensitive resource: " + resource + ":" + action
		default:
			return 2, ""
		}
	default:
		return 6, "custom Host API action: " + resource + ":" + action
	}
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
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
