package plugin

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	pluginv4 "github.com/campusos/CampusOS/internal/plugin/v4"
)

const PDFViewerV4PluginName = "campusos.pdf-viewer"

// V4PluginsDirFromEnv is intentionally separate from PLUGINS_DIR. The latter
// is a legacy development package directory; v4 only discovers immutable,
// verified releases beneath .installed and never executes repository source.
func V4PluginsDirFromEnv() string {
	if value := strings.TrimSpace(os.Getenv("CAMPUSOS_PLUGIN_V4_DIR")); value != "" {
		return value
	}
	return "plugins"
}

// V4UIOriginFromEnv is the public, cross-origin static asset origin. The host
// browser must never iframe its own origin: that would let an untrusted plugin
// page share the host's DOM origin.
func V4UIOriginFromEnv() string {
	if value := strings.TrimRight(strings.TrimSpace(os.Getenv("CAMPUSOS_PLUGIN_UI_ORIGIN")), "/"); value != "" {
		return value
	}
	return "http://{host}:3003"
}

func V4DevelopmentSourceEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("CAMPUSOS_PLUGIN_V4_DEV_SOURCE")), "true")
}

type V4Release struct {
	Release    pluginv4.InstalledRelease
	PublicPath string
	UIOrigin   string
}

// v4Catalog stores release metadata separately from the legacy manifest
// catalog. The legacy catalog only receives a tiny authorization adapter; it
// never receives v4 UI source, entry URLs or executable callbacks.
type v4Catalog struct {
	mu       sync.RWMutex
	releases map[string]V4Release
}

func newV4Catalog() *v4Catalog { return &v4Catalog{releases: map[string]V4Release{}} }

func (c *v4Catalog) list() []V4Release {
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := make([]V4Release, 0, len(c.releases))
	for _, release := range c.releases {
		items = append(items, release)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Release.Manifest.Key < items[j].Release.Manifest.Key })
	return items
}

func (c *v4Catalog) get(key string) (V4Release, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	release, ok := c.releases[key]
	return release, ok
}

func (c *v4Catalog) put(release V4Release) error {
	if release.Release.Manifest == nil {
		return fmt.Errorf("v4 release manifest is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.releases[release.Release.Manifest.Key]; ok && existing.Release.Digest != release.Release.Digest {
		return fmt.Errorf("v4 plugin %q has more than one installed active release", release.Release.Manifest.Key)
	}
	c.releases[release.Release.Manifest.Key] = release
	return nil
}

func (m *Manager) V4Releases() []V4Release {
	if m == nil || m.v4 == nil {
		return []V4Release{}
	}
	return m.v4.list()
}

func (m *Manager) V4Release(key string) (V4Release, bool) {
	if m == nil || m.v4 == nil {
		return V4Release{}, false
	}
	return m.v4.get(key)
}

// LoadV4Releases discovers verified immutable packages. It only walks the
// .installed tree so a repository checkout, temporary staging directory, or
// user-writable configuration path can never become executable plugin UI.
func (m *Manager) LoadV4Releases(root, publicOrigin string) error {
	if m == nil {
		return fmt.Errorf("plugin manager is required")
	}
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("v4 plugin root is required")
	}
	publicOrigin = strings.TrimRight(strings.TrimSpace(publicOrigin), "/")
	parsedOrigin, err := url.Parse(strings.Replace(publicOrigin, "{host}", "localhost", 1))
	if err != nil || parsedOrigin.Scheme == "" || parsedOrigin.Host == "" {
		return fmt.Errorf("v4 plugin UI origin is invalid")
	}
	releases, err := pluginv4.DiscoverInstalledReleases(root)
	if err != nil {
		return err
	}
	for _, release := range releases {
		base := filepath.Base(release.Path)
		publicPath := path.Join("plugins", release.Manifest.Key, base)
		if _, err := m.RegisterV4Release(V4Release{Release: release, PublicPath: publicPath, UIOrigin: publicOrigin}); err != nil {
			return err
		}
	}
	return nil
}

// InstallV4DevelopmentSources is an explicit Docker-development convenience,
// not production discovery. It builds a signed-contract release from already
// compiled source artifacts and installs it through the same checksum and
// atomic staging path as an uploaded package. A source edit that changes an
// immutable version is rejected on the next restart instead of silently
// replacing a release with inherited authorization decisions.
func (m *Manager) InstallV4DevelopmentSources(root string) error {
	if m == nil {
		return fmt.Errorf("plugin manager is required")
	}
	sources, err := pluginv4.FindSourcePackages(root)
	if err != nil {
		return err
	}
	for _, source := range sources {
		manifest, err := pluginv4.ValidateSourceDirectory(source)
		if err != nil {
			return fmt.Errorf("validate v4 development source %s: %w", source, err)
		}
		stage, err := os.MkdirTemp(filepath.Join(root, ".staging"), "dev-build-")
		if err != nil {
			if mkErr := os.MkdirAll(filepath.Join(root, ".staging"), 0o700); mkErr != nil {
				return mkErr
			}
			stage, err = os.MkdirTemp(filepath.Join(root, ".staging"), "dev-build-")
		}
		if err != nil {
			return err
		}
		func() {
			defer os.RemoveAll(stage)
			releaseDir := filepath.Join(stage, "release")
			if _, err = pluginv4.PrepareReleaseDirectory(source, releaseDir); err != nil {
				return
			}
			archivePath := filepath.Join(stage, manifest.Key+"-"+manifest.Version+".tar.gz")
			if _, err = pluginv4.BuildReleaseArchive(releaseDir, archivePath); err != nil {
				return
			}
			_, err = pluginv4.InstallReleaseArchive(archivePath, pluginv4.InstallOptions{RootDir: root})
		}()
		if err != nil {
			return fmt.Errorf("install v4 development source %s: %w", source, err)
		}
	}
	return nil
}

// RegisterV4Release registers an already-verified immutable release. Its v3
// adapter exists solely for the existing capability/decision store; v4 UI
// metadata remains in the isolated catalog and is projected by RuntimeManifest.
func (m *Manager) RegisterV4Release(release V4Release) (*Plugin, error) {
	if m == nil || m.v4 == nil || release.Release.Manifest == nil {
		return nil, fmt.Errorf("v4 release manifest is required")
	}
	manifest := release.Release.Manifest
	if err := manifest.ValidateCapabilities(func(code string) (pluginv4.CapabilityDescriptor, bool) {
		capability, ok := CapabilityByCode(code)
		return pluginv4.CapabilityDescriptor{Code: capability.Code, Scope: capability.Scope}, ok
	}); err != nil {
		return nil, err
	}
	adapter := v4AuthorizationManifest(manifest)
	if err := adapter.Validate(); err != nil {
		return nil, fmt.Errorf("v4 authorization adapter: %w", err)
	}
	m.catalog.mu.Lock()
	checksum := strings.TrimPrefix(release.Release.Digest, "sha256:")
	if existing, exists := m.catalog.plugins[adapter.Name]; exists {
		if existing.Checksum != checksum || existing.Manifest == nil || existing.Manifest.Version != adapter.Version {
			m.catalog.mu.Unlock()
			return nil, fmt.Errorf("v4 plugin %q conflicts with an existing installed plugin", adapter.Name)
		}
		m.catalog.mu.Unlock()
		if err := m.v4.put(release); err != nil {
			return nil, err
		}
		return clonePlugin(existing), nil
	}
	p := &Plugin{
		ID: adapter.Name, Manifest: adapter, Status: StatusInstalled,
		BackendState: BackendInstalled, FrontendState: FrontendLoaded,
		Health: HealthUnknown, DesiredEnabled: true, Directory: release.Release.Path,
		InstalledBy: "system", Checksum: checksum, IsolatedUI: true,
	}
	m.catalog.plugins[p.ID] = p
	m.ui.Bump()
	m.catalog.mu.Unlock()
	if err := m.v4.put(release); err != nil {
		return nil, err
	}
	if err := m.packages.syncPluginRecord(context.Background(), p); err != nil {
		return nil, err
	}
	return clonePlugin(p), nil
}

func v4AuthorizationManifest(source *pluginv4.Manifest) *Manifest {
	capabilities := make([]CapabilityRequest, 0, len(source.Capabilities))
	for _, capability := range source.Capabilities {
		capabilities = append(capabilities, CapabilityRequest{Code: capability.Code, Required: capability.Required, Purpose: capability.Purpose, Scope: capability.Scope})
	}
	return &Manifest{
		APIVersion: ManifestAPIVersionV3, HostAPIVersion: HostAPIVersionV3,
		Name: source.Key, DisplayName: source.DisplayName, Version: source.Version,
		Description: source.Description, Author: source.Publisher.Name, Runtime: "none",
		Scope: ScopeSystem, Type: PluginTypeExternal, CapabilityDeclarations: capabilities,
		Lifecycle: LifecycleConfig{Backend: BackendLifecycleConfig{ActivationMode: ActivationHot}, Frontend: FrontendLifecycleConfig{ActivationMode: ActivationHot}},
	}
}
