package port

import (
	"context"
	"fmt"
	pluginpkg "github.com/campusos/CampusOS/internal/plugin"
)

type CatalogAdapter struct{ catalog *pluginpkg.PluginCatalog }

func NewCatalogAdapter(catalog *pluginpkg.PluginCatalog) *CatalogAdapter {
	return &CatalogAdapter{catalog: catalog}
}
func (a *CatalogAdapter) List(context.Context) ([]Descriptor, error) {
	plugins := a.catalog.ListExternal()
	result := make([]Descriptor, 0, len(plugins))
	for _, p := range plugins {
		result = append(result, Descriptor{ID: p.ID, Version: p.Manifest.Version, Runtime: p.Manifest.Runtime})
	}
	return result, nil
}
func (a *CatalogAdapter) Get(_ context.Context, id string) (Descriptor, error) {
	p, ok := a.catalog.Get(id)
	if !ok || a.catalog.Classify(p.Manifest) != pluginpkg.ExternalPlugin {
		return Descriptor{}, fmt.Errorf("external plugin %q not found", id)
	}
	return Descriptor{ID: p.ID, Version: p.Manifest.Version, Runtime: p.Manifest.Runtime}, nil
}
