package v4

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DefaultUserConfigBudget int64 = 2 * 1024 * 1024
	maxDefaultConfigBytes         = 8 * 1024
)

var userIDPattern = regexp.MustCompile(`^[0-9]{1,19}$`)

// UserConfigLayout is host-owned. Runtime processes do not receive this path;
// they must use a later authorized ConfigService/Bridge call instead.
type UserConfigLayout struct {
	RootDir      string
	PluginDir    string
	ConfigDir    string
	PendingDir   string
	SnapshotsDir string
	UserPath     string
	MetaPath     string
}

type UserConfigMeta struct {
	PluginKey     string `json:"plugin_key"`
	PluginVersion string `json:"plugin_version"`
	SchemaVersion string `json:"schema_version"`
	Generation    string `json:"generation"`
	Revision      uint64 `json:"revision"`
	ContentSHA256 string `json:"content_sha256"`
	UpdatedAt     string `json:"updated_at"`
}

type pendingUserConfigWrite struct {
	UserFile string `json:"user_file"`
	MetaFile string `json:"meta_file"`
}

type UserConfigOptions struct {
	PersonalSpaceRoot string
	TotalBudgetBytes  int64
	PluginVersion     string
	Generation        string
	Now               func() time.Time
}

var userConfigLocks sync.Map

// PrepareUserConfigLayout validates the owner and package key before creating
// only host-owned directories beneath personal-space/<user>/plugins/<key>.
func PrepareUserConfigLayout(personalSpaceRoot, userID, pluginKey string) (UserConfigLayout, error) {
	if !userIDPattern.MatchString(userID) {
		return UserConfigLayout{}, errors.New("user plugin configuration requires a decimal user ID")
	}
	if !pluginKeyPattern.MatchString(pluginKey) {
		return UserConfigLayout{}, errors.New("user plugin configuration requires a valid v4 plugin key")
	}
	if strings.TrimSpace(personalSpaceRoot) == "" {
		personalSpaceRoot = "data/personal-space"
	}
	root, err := filepath.Abs(filepath.Clean(personalSpaceRoot))
	if err != nil {
		return UserConfigLayout{}, err
	}
	pluginDir := filepath.Join(root, userID, "plugins", pluginKey)
	if err := ensureContainedPath(filepath.Join(root, userID), pluginDir); err != nil {
		return UserConfigLayout{}, err
	}
	layout := UserConfigLayout{
		RootDir:      root,
		PluginDir:    pluginDir,
		ConfigDir:    filepath.Join(pluginDir, "config"),
		PendingDir:   filepath.Join(pluginDir, ".pending"),
		SnapshotsDir: filepath.Join(pluginDir, ".snapshots"),
	}
	layout.UserPath = filepath.Join(layout.ConfigDir, "user.json")
	layout.MetaPath = filepath.Join(layout.ConfigDir, "meta.json")
	for _, directory := range []string{layout.ConfigDir, layout.PendingDir, layout.SnapshotsDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return UserConfigLayout{}, err
		}
		info, err := os.Lstat(directory)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return UserConfigLayout{}, errors.New("user plugin configuration directory is unsafe")
		}
	}
	if err := recoverPendingUserConfig(layout); err != nil {
		return UserConfigLayout{}, err
	}
	return layout, nil
}

// InitializeUserConfig provisions user.json and meta.json from an immutable
// release's defaults. Repeating the same operation preserves an existing
// value, which makes user-add retry safe before database state is wired in.
func InitializeUserConfig(releaseRoot, userID string, options UserConfigOptions) (UserConfigMeta, bool, error) {
	manifest, err := ValidateReleaseDirectory(releaseRoot)
	if err != nil {
		return UserConfigMeta{}, false, err
	}
	if manifest.Configuration.User == nil {
		return UserConfigMeta{}, false, errors.New("plugin release does not declare user configuration")
	}
	lock := userConfigLock(options.PersonalSpaceRoot, userID, manifest.Key)
	lock.Lock()
	defer lock.Unlock()
	layout, err := PrepareUserConfigLayout(options.PersonalSpaceRoot, userID, manifest.Key)
	if err != nil {
		return UserConfigMeta{}, false, err
	}
	if data, err := os.ReadFile(layout.MetaPath); err == nil {
		meta, err := parseUserConfigMeta(data, manifest.Key)
		return meta, false, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return UserConfigMeta{}, false, err
	}
	defaults, err := readUserConfigDefaults(releaseRoot, manifest)
	if err != nil {
		return UserConfigMeta{}, false, err
	}
	if err := enforceUserConfigBudget(options.PersonalSpaceRoot, userID, options.TotalBudgetBytes, int64(len(defaults))); err != nil {
		return UserConfigMeta{}, false, err
	}
	meta := makeUserConfigMeta(manifest, defaults, options, 1)
	if err := writeUserConfigPair(layout, defaults, meta); err != nil {
		return UserConfigMeta{}, false, err
	}
	return meta, true, nil
}

func ReadUserConfig(personalSpaceRoot, userID, pluginKey string) ([]byte, UserConfigMeta, error) {
	lock := userConfigLock(personalSpaceRoot, userID, pluginKey)
	lock.Lock()
	defer lock.Unlock()
	layout, err := PrepareUserConfigLayout(personalSpaceRoot, userID, pluginKey)
	if err != nil {
		return nil, UserConfigMeta{}, err
	}
	data, err := readSafeJSONFile(layout.UserPath, maxConfigBytes)
	if err != nil {
		return nil, UserConfigMeta{}, err
	}
	metaData, err := readSafeJSONFile(layout.MetaPath, maxConfigBytes)
	if err != nil {
		return nil, UserConfigMeta{}, err
	}
	meta, err := parseUserConfigMeta(metaData, pluginKey)
	if err != nil {
		return nil, UserConfigMeta{}, err
	}
	if meta.ContentSHA256 != jsonDigest(data) {
		return nil, UserConfigMeta{}, errors.New("user plugin configuration digest does not match metadata")
	}
	return append([]byte(nil), data...), meta, nil
}

// UpdateUserConfig uses a compare-and-swap revision. It is an owner-scoped
// host operation; callers must authenticate ownership before reaching it.
func UpdateUserConfig(releaseRoot, userID string, expectedRevision uint64, value []byte, options UserConfigOptions) (UserConfigMeta, error) {
	manifest, err := ValidateReleaseDirectory(releaseRoot)
	if err != nil {
		return UserConfigMeta{}, err
	}
	lock := userConfigLock(options.PersonalSpaceRoot, userID, manifest.Key)
	lock.Lock()
	defer lock.Unlock()
	layout, err := PrepareUserConfigLayout(options.PersonalSpaceRoot, userID, manifest.Key)
	if err != nil {
		return UserConfigMeta{}, err
	}
	current, currentMeta, err := readUserConfigUnlocked(layout, manifest.Key)
	if err != nil {
		return UserConfigMeta{}, err
	}
	if currentMeta.Revision != expectedRevision {
		return UserConfigMeta{}, fmt.Errorf("user plugin configuration revision conflict: current=%d", currentMeta.Revision)
	}
	value, err = validateUserConfigValue(value, releaseRoot, manifest)
	if err != nil {
		return UserConfigMeta{}, err
	}
	if int64(len(value))-int64(len(current)) > 0 {
		if err := enforceUserConfigBudget(options.PersonalSpaceRoot, userID, options.TotalBudgetBytes, int64(len(value))-int64(len(current))); err != nil {
			return UserConfigMeta{}, err
		}
	}
	if err := snapshotUserConfig(layout, currentMeta, current); err != nil {
		return UserConfigMeta{}, err
	}
	next := makeUserConfigMeta(manifest, value, options, currentMeta.Revision+1)
	if err := writeUserConfigPair(layout, value, next); err != nil {
		return UserConfigMeta{}, err
	}
	return next, nil
}

func readUserConfigDefaults(releaseRoot string, manifest *Manifest) ([]byte, error) {
	definition := manifest.Configuration.User
	if definition == nil {
		return nil, errors.New("user configuration is not declared")
	}
	defaults, err := readSafeJSONFile(filepath.Join(releaseRoot, filepath.FromSlash(definition.Defaults)), maxDefaultConfigBytes)
	if err != nil {
		return nil, err
	}
	return validateUserConfigValue(defaults, releaseRoot, manifest)
}

func validateUserConfigValue(data []byte, releaseRoot string, manifest *Manifest) ([]byte, error) {
	limit := manifest.Data.UserConfigMaxBytes
	if limit == 0 {
		limit = maxConfigBytes
	}
	if int64(len(data)) > limit || len(data) == 0 {
		return nil, errors.New("user plugin configuration exceeds declared size limit")
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, errors.New("user plugin configuration must be a JSON object")
	}
	if err := validateConfigValueAgainstSchema(value, filepath.Join(releaseRoot, filepath.FromSlash(manifest.Configuration.User.Schema))); err != nil {
		return nil, err
	}
	return canonicalJSON(value)
}

func validateConfigValueAgainstSchema(value map[string]any, schemaPath string) error {
	schemaData, err := readSafeJSONFile(schemaPath, maxConfigBytes)
	if err != nil {
		return err
	}
	var schema struct {
		Properties           map[string]map[string]any `json:"properties"`
		Required             []string                  `json:"required"`
		AdditionalProperties bool                      `json:"additionalProperties"`
	}
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		return err
	}
	for _, name := range schema.Required {
		if _, ok := value[name]; !ok {
			return fmt.Errorf("user plugin configuration is missing required field %q", name)
		}
	}
	if !schema.AdditionalProperties {
		for name := range value {
			if _, ok := schema.Properties[name]; !ok {
				return fmt.Errorf("user plugin configuration contains undeclared field %q", name)
			}
		}
	}
	for name, definition := range schema.Properties {
		if actual, exists := value[name]; exists {
			if err := validateConfigField(name, actual, definition); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateConfigField(name string, value any, schema map[string]any) error {
	typeName, _ := schema["type"].(string)
	switch typeName {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("user plugin configuration field %q must be a string", name)
		}
	case "number", "integer":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("user plugin configuration field %q must be a number", name)
		}
		parsed, err := number.Float64()
		if err != nil || (typeName == "integer" && strings.Contains(number.String(), ".")) {
			return fmt.Errorf("user plugin configuration field %q has an invalid number", name)
		}
		if minimum, ok := schema["minimum"].(float64); ok && parsed < minimum {
			return fmt.Errorf("user plugin configuration field %q is below the allowed minimum", name)
		}
		if maximum, ok := schema["maximum"].(float64); ok && parsed > maximum {
			return fmt.Errorf("user plugin configuration field %q exceeds the allowed maximum", name)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("user plugin configuration field %q must be a boolean", name)
		}
	}
	if enum, exists := schema["enum"].([]any); exists {
		matched := false
		for _, candidate := range enum {
			if fmt.Sprint(candidate) == fmt.Sprint(value) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("user plugin configuration field %q is not an allowed value", name)
		}
	}
	return nil
}

func makeUserConfigMeta(manifest *Manifest, value []byte, options UserConfigOptions, revision uint64) UserConfigMeta {
	now := time.Now().UTC()
	if options.Now != nil {
		now = options.Now().UTC()
	}
	return UserConfigMeta{
		PluginKey:     manifest.Key,
		PluginVersion: options.PluginVersion,
		SchemaVersion: manifest.Configuration.User.Version,
		Generation:    options.Generation,
		Revision:      revision,
		ContentSHA256: jsonDigest(value),
		UpdatedAt:     now.Format(time.RFC3339Nano),
	}
}

func writeUserConfigPair(layout UserConfigLayout, value []byte, meta UserConfigMeta) error {
	metaData, err := canonicalJSON(meta)
	if err != nil {
		return err
	}
	userName := fmt.Sprintf("user-%020d.json", meta.Revision)
	metaName := fmt.Sprintf("meta-%020d.json", meta.Revision)
	if err := writePendingJSON(layout.PendingDir, userName, value); err != nil {
		return err
	}
	if err := writePendingJSON(layout.PendingDir, metaName, metaData); err != nil {
		return err
	}
	operation, err := canonicalJSON(pendingUserConfigWrite{UserFile: userName, MetaFile: metaName})
	if err != nil {
		return err
	}
	if err := writePendingJSON(layout.PendingDir, "operation.json", operation); err != nil {
		return err
	}
	return recoverPendingUserConfig(layout)
}

func writePendingJSON(pendingDir, name string, data []byte) error {
	if !safeRelativePath(name) || strings.Contains(name, "/") {
		return errors.New("user plugin pending configuration filename is unsafe")
	}
	if err := os.MkdirAll(pendingDir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(pendingDir, ".config-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(pendingDir, name))
}

func recoverPendingUserConfig(layout UserConfigLayout) error {
	operationPath := filepath.Join(layout.PendingDir, "operation.json")
	data, err := os.ReadFile(operationPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var operation pendingUserConfigWrite
	if err := json.Unmarshal(data, &operation); err != nil || !safePendingFilename(operation.UserFile) || !safePendingFilename(operation.MetaFile) {
		return errors.New("user plugin pending configuration operation is invalid")
	}
	for _, item := range []struct {
		staged      string
		destination string
	}{
		{staged: filepath.Join(layout.PendingDir, operation.UserFile), destination: layout.UserPath},
		{staged: filepath.Join(layout.PendingDir, operation.MetaFile), destination: layout.MetaPath},
	} {
		if _, err := os.Stat(item.staged); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if info, err := os.Lstat(item.destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("user plugin configuration path cannot be a symbolic link")
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(item.staged, item.destination); err != nil {
			return err
		}
	}
	return os.Remove(operationPath)
}

func safePendingFilename(value string) bool {
	return safeRelativePath(value) && !strings.Contains(value, "/") && value != "operation.json"
}

func snapshotUserConfig(layout UserConfigLayout, meta UserConfigMeta, data []byte) error {
	if meta.Revision == 0 {
		return nil
	}
	files, err := os.ReadDir(layout.SnapshotsDir)
	if err != nil {
		return err
	}
	if len(files) >= 3 {
		sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
		if err := os.Remove(filepath.Join(layout.SnapshotsDir, files[0].Name())); err != nil {
			return err
		}
	}
	name := fmt.Sprintf("%020d.json", meta.Revision)
	return os.WriteFile(filepath.Join(layout.SnapshotsDir, name), data, 0o600)
}

func readUserConfigUnlocked(layout UserConfigLayout, pluginKey string) ([]byte, UserConfigMeta, error) {
	value, err := readSafeJSONFile(layout.UserPath, maxConfigBytes)
	if err != nil {
		return nil, UserConfigMeta{}, err
	}
	metaData, err := readSafeJSONFile(layout.MetaPath, maxConfigBytes)
	if err != nil {
		return nil, UserConfigMeta{}, err
	}
	meta, err := parseUserConfigMeta(metaData, pluginKey)
	if err != nil {
		return nil, UserConfigMeta{}, err
	}
	if meta.ContentSHA256 != jsonDigest(value) {
		return nil, UserConfigMeta{}, errors.New("user plugin configuration digest does not match metadata")
	}
	return value, meta, nil
}

func readSafeJSONFile(filename string, limit int64) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("user plugin configuration file is unsafe")
	}
	data, err := os.ReadFile(filename)
	if err != nil || !json.Valid(data) {
		return nil, errors.New("user plugin configuration file is not valid JSON")
	}
	return data, nil
}

func parseUserConfigMeta(data []byte, pluginKey string) (UserConfigMeta, error) {
	var meta UserConfigMeta
	if err := json.Unmarshal(data, &meta); err != nil || meta.PluginKey != pluginKey || meta.Revision == 0 || !strings.HasPrefix(meta.ContentSHA256, "sha256:") {
		return UserConfigMeta{}, errors.New("user plugin configuration metadata is invalid")
	}
	return meta, nil
}

func enforceUserConfigBudget(root, userID string, budget, additional int64) error {
	if budget <= 0 {
		budget = DefaultUserConfigBudget
	}
	if additional <= 0 {
		return nil
	}
	base, err := filepath.Abs(filepath.Join(rootOrDefault(root), userID, "plugins"))
	if err != nil {
		return err
	}
	var total int64
	err = filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil || entry.IsDir() || filepath.Base(path) != "user.json" {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if total+additional > budget {
		return fmt.Errorf("user plugin configuration exceeds the total %d-byte budget", budget)
	}
	return nil
}

func rootOrDefault(root string) string {
	if strings.TrimSpace(root) == "" {
		return "data/personal-space"
	}
	return root
}

func jsonDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func userConfigLock(root, userID, pluginKey string) *sync.Mutex {
	key := filepath.Clean(rootOrDefault(root)) + "\x00" + userID + "\x00" + pluginKey
	value, _ := userConfigLocks.LoadOrStore(key, &sync.Mutex{})
	return value.(*sync.Mutex)
}
