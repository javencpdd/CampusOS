package plugin

import (
	"fmt"
	pluginv4 "github.com/campusos/CampusOS/internal/plugin/v4"
	"os"
	"sort"
	"strings"
	"sync"
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
