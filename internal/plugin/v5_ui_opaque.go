package plugin

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (h *V5UIHost) assetURL(id string, lease v5UILease) string {
	if lease.facts.Artifact.Isolation == "opaque" {
		return lease.parent + "/api/v1/plugin-v5/ui-assets/" + id + "/" + lease.facts.Artifact.Entrypoint
	}
	return h.leaseOrigin(id) + "/ui/" + id + "/" + lease.facts.Artifact.Entrypoint
}

// OpaqueUIAsset serves only a signed, current lease. Both the response CSP and
// iframe sandbox force a unique opaque origin: plugin JS cannot acquire the
// application's origin, cookies, storage, DOM or service-worker authority.
// The CORS header permits module assets from Origin:null, without credentials.
func (h *V5HTTPHandler) OpaqueUIAsset(c *gin.Context) {
	if h == nil || h.UIHost == nil {
		c.Status(http.StatusNotFound)
		return
	}
	h.UIHost.serveOpaque(c.Writer, c.Request, c.Param("lease"), strings.TrimPrefix(c.Param("asset"), "/"))
}

func (h *V5UIHost) serveOpaque(w http.ResponseWriter, r *http.Request, id, asset string) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	// Enforce sandbox in the HTTP response as well, even if an embedding page
	// omits the iframe attribute. No allow-same-origin or persistent worker.
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src data: blob:; connect-src 'none'; worker-src 'none'; form-action 'none'; base-uri 'none'; object-src 'none'; frame-src 'none'; frame-ancestors "+h.ancestors)
	if (r.Method != "GET" && r.Method != "HEAD") || len(r.URL.RequestURI()) > 4096 || len(id) != 32 ||
		r.Header.Get("Sec-Fetch-Dest") == "document" || r.Header.Get("Sec-Fetch-Dest") == "serviceworker" || r.Header.Get("Service-Worker") != "" {
		http.NotFound(w, r)
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		http.Error(w, "unavailable", 503)
		return
	}
	h.mu.Lock()
	lease, ok := h.leases[id]
	if ok && !h.now().Before(lease.expires) {
		delete(h.leases, id)
		ok = false
	}
	h.mu.Unlock()
	if !ok || lease.facts.Artifact.Isolation != "opaque" {
		http.NotFound(w, r)
		return
	}
	raw, mime, err := h.platform.UIAsset(r.Context(), lease.facts, asset, lease.admin, lease.resolve)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.WriteHeader(http.StatusOK)
	if r.Method != "HEAD" {
		_, _ = w.Write(raw)
	}
}
