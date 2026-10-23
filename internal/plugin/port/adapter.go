package port

import (
	"context"
	"fmt"
	pluginpkg "github.com/campusos/CampusOS/internal/plugin"
)

type CatalogAdapter struct{ catalog *pluginpkg.PluginCatalog }

type V5CatalogAdapter struct{ Current func() *pluginpkg.V5Platform }

func (a *V5CatalogAdapter) List(ctx context.Context) ([]Descriptor, error) {
	out := []Descriptor{}
	after := ""
	for {
		entries, next, err := a.Current().HostCatalogPage(ctx, after)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			out = append(out, Descriptor{ID: e.ID, Version: e.Version, Runtime: e.Runtime, Status: e.Status})
		}
		if next == "" {
			return out, nil
		}
		after = next
	}
}
func (a *V5CatalogAdapter) Get(ctx context.Context, id string) (Descriptor, error) {
	entries, err := a.List(ctx)
	if err != nil {
		return Descriptor{}, err
	}
	for _, e := range entries {
		if e.ID == id {
			return e, nil
		}
	}
	return Descriptor{}, pluginpkg.ErrV5NotFound
}

func NewCatalogAdapter(catalog *pluginpkg.PluginCatalog) *CatalogAdapter {
	return &CatalogAdapter{catalog: catalog}
}
func (a *CatalogAdapter) List(context.Context) ([]Descriptor, error) {
	plugins := a.catalog.ListExternal()
	result := make([]Descriptor, 0, len(plugins))
	for _, p := range plugins {
		result = append(result, Descriptor{ID: p.ID, Version: p.Manifest.Version, Runtime: p.Manifest.Runtime, Status: string(p.Status)})
	}
	return result, nil
}
func (a *CatalogAdapter) Get(_ context.Context, id string) (Descriptor, error) {
	p, ok := a.catalog.Get(id)
	if !ok || a.catalog.Classify(p.Manifest) != pluginpkg.ExternalPlugin {
		return Descriptor{}, fmt.Errorf("external plugin %q not found", id)
	}
	return Descriptor{ID: p.ID, Version: p.Manifest.Version, Runtime: p.Manifest.Runtime, Status: string(p.Status)}, nil
}
