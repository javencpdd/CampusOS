package plugin

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type v5UILease struct {
	facts   V5UIFacts
	resolve identity.SessionPrincipalResolver
	admin   bool
	expires time.Time
}

// V5UIHost is served on a dedicated origin/listener. It has no API routes,
// bearer/cookie authentication, arbitrary proxy or platform filesystem mount.
type V5UIHost struct {
	platform  *V5Platform
	origin    string
	parents   map[string]bool
	ancestors string
	mu        sync.Mutex
	leases    map[string]v5UILease
	slots     chan struct{}
	now       func() time.Time
}

var v5UIDNSLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func exactUIOrigin(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.String() != raw {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !v5UIDNSLabel.MatchString(label) {
			return false
		}
	}
	return true
}
func NewV5UIHost(platform *V5Platform, origin string, parents []string) (*V5UIHost, error) {
	probe := strings.Replace(origin, "{isolation}", strings.Repeat("a", 32), 1)
	parsed, _ := url.Parse(probe)
	if platform == nil || strings.Count(origin, "{isolation}") != 1 || !exactUIOrigin(probe) || parsed == nil || !strings.HasPrefix(parsed.Hostname(), strings.Repeat("a", 32)+".") || len(parents) < 1 || len(parents) > 8 {
		return nil, ErrV5Invalid
	}
	h := &V5UIHost{platform: platform, origin: origin, parents: map[string]bool{}, leases: map[string]v5UILease{}, slots: make(chan struct{}, 4), now: time.Now}
	uiDomain := strings.TrimPrefix(parsed.Hostname(), strings.Repeat("a", 32)+".")
	for _, parent := range parents {
		p, _ := url.Parse(parent)
		if !exactUIOrigin(parent) || h.parents[parent] || p.Scheme == parsed.Scheme && p.Port() == parsed.Port() && strings.HasSuffix(p.Hostname(), "."+uiDomain) {
			return nil, ErrV5Invalid
		}
		h.parents[parent] = true
	}
	h.ancestors = strings.Join(parents, " ")
	return h, nil
}
func (h *V5UIHost) leaseOrigin(id string) string {
	return strings.Replace(h.origin, "{isolation}", id, 1)
}
func (h *V5UIHost) framePolicy() string { return strings.Replace(h.origin, "{isolation}", "*", 1) }
func (h *V5UIHost) issue(ctx context.Context, c V5Coordinate, admin bool, parent string, resolve identity.SessionPrincipalResolver) (V5UIFacts, string, error) {
	if h == nil || !h.parents[parent] {
		return V5UIFacts{}, "", ErrV5Denied
	}
	f, err := h.platform.UI(ctx, c, admin, resolve)
	if err != nil {
		return f, "", err
	}
	id := v5RandomID()
	if id == "" {
		return f, "", ErrV5PlatformUnavailable
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for id, lease := range h.leases {
		if lease.facts == f && lease.admin == admin && now.Add(time.Minute).Before(lease.expires) {
			return f, h.leaseOrigin(id) + "/ui/" + id + "/" + f.Artifact.Entrypoint, nil
		}
		if !now.Before(lease.expires) {
			delete(h.leases, id)
		}
	}
	if len(h.leases) >= 2048 {
		return f, "", ErrV5PlatformUnavailable
	}
	h.leases[id] = v5UILease{f, resolve, admin, now.Add(10 * time.Minute)}
	return f, h.leaseOrigin(id) + "/ui/" + id + "/" + f.Artifact.Entrypoint, nil
}
func (h *V5UIHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Origin-Agent-Cluster", "?1")
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; worker-src 'self' blob:; connect-src 'none'; form-action 'none'; base-uri 'none'; object-src 'none'; frame-src 'none'; frame-ancestors "+h.ancestors)
	if len(r.URL.RequestURI()) > 4096 || r.Method != "GET" && r.Method != "HEAD" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Sec-Fetch-Dest") == "document" || r.Header.Get("Sec-Fetch-Dest") == "serviceworker" || r.Header.Get("Service-Worker") == "script" {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/ui/"), "/", 2)
	if !strings.HasPrefix(r.URL.Path, "/ui/") || len(parts) != 2 || len(parts[0]) != 32 {
		http.NotFound(w, r)
		return
	}
	h.mu.Lock()
	lease, ok := h.leases[parts[0]]
	if ok && !h.now().Before(lease.expires) {
		delete(h.leases, parts[0])
		ok = false
	}
	h.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	u, _ := url.Parse(h.leaseOrigin(parts[0]))
	if r.Host != u.Host {
		http.NotFound(w, r)
		return
	}
	raw, mime, err := h.platform.UIAsset(r.Context(), lease.facts, parts[1], lease.admin, lease.resolve)
	if err != nil {
		h.mu.Lock()
		delete(h.leases, parts[0])
		h.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	if r.Method == "GET" {
		_, _ = w.Write(raw)
	}
}

// Authenticated inventory uses host-assigned surface/route names. Packages
// cannot inject platform routes, executable host components or API URLs.
func (h *V5HTTPHandler) UIManifest(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !v5HTTPStart(c) {
			return
		}
		if h == nil || h.Platform == nil {
			v5HTTPError(c, ErrV5PlatformUnavailable)
			return
		}
		if h.UIHost == nil {
			if err := h.Platform.read(c.Request.Context(), admin, h.resolver(c, admin), func(context.Context, v5Actor) error { return nil }); err != nil {
				v5HTTPError(c, err)
				return
			}
			response.Success(c, gin.H{"plugins": []any{}, "next_cursor": "", "frame_src": "'none'"})
			return
		}
		parent := c.Query("parent_origin")
		if !h.UIHost.parents[parent] {
			v5HTTPError(c, ErrV5Denied)
			return
		}
		page, err := h.Platform.Inventory(c.Request.Context(), admin, c.Query("after"), 50, h.resolver(c, admin))
		if err != nil {
			v5HTTPError(c, err)
			return
		}
		plugins := []gin.H{}
		for _, entry := range page.Items {
			f, src, err := h.UIHost.issue(c.Request.Context(), entry.Release.V5Coordinate, admin, parent, h.resolver(c, admin))
			if err != nil {
				if v5UIUnavailable(err) || errors.Is(err, ErrPluginV5PackageSignature) {
					continue
				}
				v5HTTPError(c, err)
				return
			}
			manifest, err := ParsePluginV5Manifest(entry.Release.Manifest)
			if err != nil {
				v5HTTPError(c, err)
				return
			}
			name := f.Publisher + "." + f.Key
			surface := name + ".ui"
			surfaceType := "isolated-plugin"
			if f.Publisher == "campusos" && f.Key == "pdf-viewer" && !admin {
				surface = name + ".preview"
				surfaceType = "document-preview"
			}
			audience, scope, backend := "user", "user", "stopped"
			if admin {
				audience, scope = "admin", "system"
			}
			if manifest.Runtime != "none" {
				backend = "ready"
			}
			navigation := []gin.H{}
			if surfaceType != "document-preview" {
				navigation = append(navigation, gin.H{"id": surface + ".navigation", "label": name, "route_id": surface + ".route", "location": "main"})
			}
			origin, _ := url.Parse(src)
			frame := gin.H{"src": src, "origin": origin.Scheme + "://" + origin.Host, "audience": audience}
			route := gin.H{"id": surface + ".route", "path": "/extensions/" + name + "/ui", "surface_id": surface, "title": name, "requires_auth": true}
			definition := gin.H{
				"id": surface, "version": f.Version, "type": surfaceType,
				"layout_role": "main", "renderer": "isolated-iframe", "frame": frame,
				"presentations": []string{"modal", "drawer", "fullscreen", "new-tab"},
				"regions":       []string{"isolated-frame"}, "context_proof": f.Proof,
			}
			lifecycle := gin.H{
				"scope": scope, "backend_activation_mode": "hot", "frontend_activation_mode": "hot",
				"backend_state": backend, "frontend_state": "loaded", "health": "healthy",
				"desired_enabled": true, "pending_restart": false,
			}
			ui := gin.H{
				"contract_version": "campusos.ui/v3", "routes": []gin.H{route},
				"navigation": navigation, "surfaces": []gin.H{definition}, "actions": []any{}, "slots": []any{},
			}
			plugins = append(plugins, gin.H{"name": name, "version": f.Version, "runtime": manifest.Runtime, "scope": scope, "lifecycle": lifecycle, "ui": ui})
		}
		response.Success(c, gin.H{"plugins": plugins, "next_cursor": page.Next, "frame_src": h.UIHost.framePolicy()})
	}
}

func (h *V5HTTPHandler) UIState(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !v5HTTPStart(c) {
			return
		}
		if h == nil || h.Platform == nil || h.UIHost == nil {
			v5HTTPError(c, ErrV5PlatformUnavailable)
			return
		}
		facts, err := h.Platform.UI(c.Request.Context(), V5Coordinate{c.Param("publisher"), c.Param("key"), c.Param("version")}, admin, h.resolver(c, admin))
		if err != nil {
			v5HTTPError(c, err)
			return
		}
		response.Success(c, facts)
	}
}
