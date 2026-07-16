package appearance

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
	"github.com/campusos/CampusOS/internal/platform/resource"
)

type AppearanceCatalog interface {
	List(resource.Type) ([]resource.Item, error)
	Get(resource.Type, string) (resource.Item, error)
}
type PackageValidator interface {
	Validate(string, resource.Manifest) error
}
type PreferenceStore interface {
	Get(context.Context, string, resource.Type) (string, error)
	Set(context.Context, string, resource.Type, string) error
}
type AppearancePolicy interface {
	CanApply(context.Context, string, resource.Item) error
}

type Facade struct {
	Catalog     AppearanceCatalog
	Validator   PackageValidator
	Preferences PreferenceStore
	Policy      AppearancePolicy
}

func (f Facade) Apply(ctx context.Context, userID string, kind resource.Type, id string) error {
	if f.Catalog == nil || f.Preferences == nil || f.Policy == nil {
		return errors.New("appearance facade is incomplete")
	}
	item, err := f.Catalog.Get(kind, id)
	if err != nil {
		return err
	}
	if err = f.Policy.CanApply(ctx, userID, item); err != nil {
		return err
	}
	return f.Preferences.Set(ctx, userID, kind, id)
}

type ValidatorAdapter struct{}

func (ValidatorAdapter) Validate(dir string, manifest resource.Manifest) error {
	return resource.Validate(dir, manifest)
}

type CompatibilityPolicy struct{ roots []string }

func NewCompatibilityPolicy(roots ...string) *CompatibilityPolicy {
	return &CompatibilityPolicy{roots: roots}
}
func (p *CompatibilityPolicy) CanApply(_ context.Context, actor string, item resource.Item) error {
	if strings.TrimSpace(actor) == "" {
		return errors.New("appearance actor is required")
	}
	if item.Manifest.Type == resource.HomepagePack && actor != "system" {
		return errors.New("homepage packages require system administration")
	}
	target, err := filepath.Abs(item.Directory)
	if err != nil {
		return err
	}
	for _, root := range p.roots {
		base, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if target == base || strings.HasPrefix(target, base+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("appearance package is outside managed repositories")
}

type MemoryPreferenceStore struct {
	mu     sync.RWMutex
	values map[string]string
}

func NewMemoryPreferenceStore() *MemoryPreferenceStore {
	return &MemoryPreferenceStore{values: map[string]string{}}
}
func (p *MemoryPreferenceStore) Get(_ context.Context, user string, kind resource.Type) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.values[user+":"+string(kind)], nil
}
func (p *MemoryPreferenceStore) Set(_ context.Context, user string, kind resource.Type, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.values[user+":"+string(kind)] = id
	return nil
}
func NewFacade() *Facade {
	root := stylepack.ResourceDir()
	catalog := resource.NewFileRepository(root, nil)
	return &Facade{Catalog: catalog, Validator: ValidatorAdapter{}, Preferences: NewMemoryPreferenceStore(), Policy: NewCompatibilityPolicy(root)}
}

// NewCompatibilityFacade remains source-compatible for callers compiled
// during the v0.7-v0.10 transition. It no longer scans plugin_data.
func NewCompatibilityFacade() *Facade { return NewFacade() }
