package resource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Type string

const (
	Theme             Type = "theme"
	HomepagePack      Type = "homepage-pack"
	SpaceStylePack    Type = "space-style-pack"
	Skill             Type = "skill"
	Prompt            Type = "prompt"
	Persona           Type = "persona"
	KnowledgeMetadata Type = "knowledge-metadata"
)

type Manifest struct {
	Schema        string `json:"schema"`
	ID            string `json:"id"`
	Type          Type   `json:"type"`
	Version       string `json:"version"`
	Compatibility string `json:"compatibility"`
	Entry         string `json:"entry"`
	Checksum      string `json:"checksum"`
	Source        string `json:"source"`
}
type Item struct {
	Manifest  Manifest `json:"manifest"`
	Directory string   `json:"directory"`
	Legacy    bool     `json:"legacy"`
}
type Repository interface {
	List(Type) ([]Item, error)
	Get(Type, string) (Item, error)
}

type FileRepository struct {
	root   string
	legacy map[Type][]string
}

func NewFileRepository(root string, legacy map[Type][]string) *FileRepository {
	return &FileRepository{root: root, legacy: legacy}
}
func (r *FileRepository) List(kind Type) ([]Item, error) {
	if TypeDirectory(kind) == "" {
		return nil, fmt.Errorf("unsupported resource type %q", kind)
	}
	var result []Item
	root := filepath.Join(r.root, TypeDirectory(kind))
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			item, loadErr := loadItem(filepath.Join(root, entry.Name()), false)
			if loadErr != nil {
				return nil, fmt.Errorf("resource %s: %w", entry.Name(), loadErr)
			}
			if item.Manifest.Type != kind {
				return nil, fmt.Errorf("resource %s declares type %q under %q", entry.Name(), item.Manifest.Type, kind)
			}
			result = append(result, item)
		}
	}
	for _, legacyRoot := range r.legacy[kind] {
		entries, _ := os.ReadDir(legacyRoot)
		for _, entry := range entries {
			if entry.IsDir() {
				if _, err := os.Stat(filepath.Join(legacyRoot, entry.Name(), "style.yaml")); err != nil {
					continue
				}
				result = append(result, legacyItem(kind, filepath.Join(legacyRoot, entry.Name()), entry.Name()))
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Manifest.ID == result[j].Manifest.ID {
			if result[i].Legacy != result[j].Legacy {
				return !result[i].Legacy
			}
			return result[i].Directory < result[j].Directory
		}
		return result[i].Manifest.ID < result[j].Manifest.ID
	})
	deduplicated := make([]Item, 0, len(result))
	seen := map[string]bool{}
	for _, item := range result {
		if !seen[item.Manifest.ID] {
			seen[item.Manifest.ID] = true
			deduplicated = append(deduplicated, item)
		}
	}
	return deduplicated, nil
}
func (r *FileRepository) Get(kind Type, id string) (Item, error) {
	items, err := r.List(kind)
	if err != nil {
		return Item{}, err
	}
	for _, item := range items {
		if item.Manifest.ID == id {
			return item, nil
		}
	}
	return Item{}, fs.ErrNotExist
}

func Validate(dir string, manifest Manifest) error {
	if manifest.Schema != "campusos.resource/v1" || !SafeID(manifest.ID) || manifest.Version == "" || manifest.Entry == "" || manifest.Compatibility == "" || manifest.Checksum == "" || manifest.Source == "" {
		return errors.New("resource manifest requires schema, safe id, version, compatibility, entry, checksum and source")
	}
	entry, err := safeRelativePath(dir, manifest.Entry)
	if err != nil {
		return err
	}
	if TypeDirectory(manifest.Type) == "" {
		return fmt.Errorf("unsupported resource type %q", manifest.Type)
	}
	forbiddenNames := map[string]bool{"plugin.yaml": true, "migration.sql": true, "run.sh": true}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("resource package contains symbolic link %q", entry.Name())
		}
		name := strings.ToLower(entry.Name())
		if forbiddenNames[name] || name == "migrations" || (!entry.IsDir() && strings.HasSuffix(name, ".go")) {
			return fmt.Errorf("resource package contains forbidden runtime artifact %q", entry.Name())
		}
		return nil
	})
	if err != nil {
		return err
	}
	info, err := os.Stat(entry)
	if err != nil {
		return fmt.Errorf("resource entry: %w", err)
	}
	if info.IsDir() {
		return errors.New("resource entry must be a file")
	}
	actual, err := DirectoryChecksum(dir)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, manifest.Checksum) {
		return errors.New("resource checksum mismatch")
	}
	return nil
}

func safeRelativePath(root, value string) (string, error) {
	if filepath.IsAbs(value) || strings.TrimSpace(value) == "" {
		return "", errors.New("resource path must be relative")
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(base, filepath.Clean(value)))
	if err != nil {
		return "", err
	}
	if target != base && !strings.HasPrefix(target, base+string(os.PathSeparator)) {
		return "", errors.New("resource path escapes package")
	}
	return target, nil
}

func DirectoryChecksum(dir string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() == "resource.json" {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		hash.Write([]byte(filepath.ToSlash(rel)))
		hash.Write([]byte{0})
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash.Write(data)
		hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func TypeDirectory(kind Type) string {
	return map[Type]string{Theme: "themes", HomepagePack: "homepage-packs", SpaceStylePack: "space-style-packs", Skill: "skills", Prompt: "prompts", Persona: "personas", KnowledgeMetadata: "knowledge-metadata"}[kind]
}
func loadItem(dir string, legacy bool) (Item, error) {
	data, err := os.ReadFile(filepath.Join(dir, "resource.json"))
	if err != nil {
		return Item{}, err
	}
	var m Manifest
	if err = json.Unmarshal(data, &m); err != nil {
		return Item{}, err
	}
	if err = Validate(dir, m); err != nil {
		return Item{}, err
	}
	return Item{Manifest: m, Directory: dir, Legacy: legacy}, nil
}
func legacyItem(kind Type, dir, id string) Item {
	checksum, err := DirectoryChecksum(dir)
	if err != nil {
		sum := sha256.Sum256([]byte(filepath.Clean(dir)))
		checksum = hex.EncodeToString(sum[:])
	}
	return Item{Manifest: Manifest{Schema: "campusos.resource/v1", ID: id, Type: kind, Version: "legacy", Compatibility: "v0.6", Entry: "style.yaml", Checksum: checksum, Source: "legacy-plugin-data"}, Directory: dir, Legacy: true}
}
func SafeID(id string) bool {
	return strings.TrimSpace(id) != "" && filepath.Base(id) == id && !strings.ContainsAny(id, `/\\`)
}
