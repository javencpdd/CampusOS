package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/webcompanion"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
	"io"
	"mime"
	"net/http"
	"os"
	"reflect"
	"strings"
)

func (h *V5HTTPHandler) clients() *webcompanion.Hub {
	h.companionOnce.Do(func() { h.companionHub = webcompanion.NewHub() })
	return h.companionHub
}
func (h *V5HTTPHandler) clientOwner(ctx context.Context, c V5Coordinate, resolve identity.SessionPrincipalResolver) (string, error) {
	p, err := resolve(ctx)
	if err != nil || p.Actor.Kind != "user" || p.Audience != "user" {
		return "", ErrV5Denied
	}
	raw, _ := json.Marshal([]string{p.Actor.ID, p.CredentialID, c.Publisher, c.Key, c.Version})
	return v5BytesDigest(raw), nil
}
func (h *V5HTTPHandler) clientAction(ctx context.Context, c V5Coordinate, q V5UIActionRequest, resolve identity.SessionPrincipalResolver) (V5UIActionResult, error) {
	var out V5UIActionResult
	if q.URL != "" || q.Kind != "" {
		return out, ErrV5Invalid
	}
	if q.Action != "web.clients.select" && q.Action != "web.clients.disconnect" && q.ClientID != "" {
		return out, ErrV5Invalid
	}
	capability := V5WebLocalRead
	if q.Action == "web.clients.status" || q.Action == "web.clients.select" && q.ClientID == "" {
		capability = V5WebPublicRead
	}
	if err := h.Platform.authorizeUIAction(ctx, c, q.Proof, capability, resolve); err != nil {
		return out, err
	}
	owner, err := h.clientOwner(ctx, c, resolve)
	if err != nil {
		return out, err
	}
	principalForScope, e := resolve(ctx)
	if e != nil {
		return out, ErrV5Denied
	}
	browserScope, _ := json.Marshal([]string{principalForScope.Actor.ID, c.Publisher, c.Key, c.Version})
	out.Scope = v5BytesDigest(browserScope)
	out.LocalAvailable = h.Platform.authorizeUIAction(ctx, c, q.Proof, V5WebLocalRead, resolve) == nil
	switch q.Action {
	case "web.clients.status", "web.clients.select":
		if out.LocalAvailable {
			out.Clients = h.clients().List(owner)
		}
		if q.ClientID != "" {
			found := false
			for _, client := range out.Clients {
				found = found || client.ID == q.ClientID && client.Online
			}
			if !found {
				return V5UIActionResult{}, ErrV5Denied
			}
		}
		return out, nil
	case "web.clients.pair":
		principal, err := resolve(ctx)
		if err != nil {
			return out, ErrV5Denied
		}
		facts, err := h.Platform.UI(ctx, c, false, resolve)
		if err != nil {
			return out, err
		}
		pair, err := h.clients().Begin(owner, func(check context.Context) bool {
			current, e := resolve(check)
			if e != nil || !reflect.DeepEqual(principal, current) {
				return false
			}
			fresh, e := h.Platform.UI(check, c, false, resolve)
			return e == nil && fresh.ClientProof == facts.ClientProof && h.Platform.authorizeUIAction(check, c, fresh.Proof, V5WebLocalRead, resolve) == nil
		})
		if err != nil {
			return out, ErrV5PlatformUnavailable
		}
		out.PairingCode = pair.Code
		out.ExpiresAt = pair.ExpiresAt
		return out, nil
	case "web.clients.disconnect":
		if h.clients().Disconnect(owner, q.ClientID) != nil {
			return out, ErrV5Denied
		}
		out.Authorized = true
		return out, nil
	default:
		return out, ErrV5Invalid
	}
}

func companionJSON(c *gin.Context, max int64, required string, fields map[string]v5Check, out any) bool {
	if !v5HTTPStart(c) || c.Request.URL.RawQuery != "" {
		v5HTTPError(c, ErrV5Invalid)
		return false
	}
	if c.GetHeader("Origin") != "" || c.GetHeader("Sec-Fetch-Site") != "" || c.GetHeader("Cookie") != "" {
		v5HTTPError(c, ErrV5Denied)
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
	raw, err := io.ReadAll(c.Request.Body)
	value, ok := v5JSONAtMost(raw, int(max))
	if err != nil || !ok || !v5Object(required, fields)(value) || strictPluginV5JSON(raw, out) != nil {
		v5HTTPError(c, ErrV5Invalid)
		return false
	}
	return true
}
func companionToken(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(header, "Bearer ")
}
func (h *V5HTTPHandler) CompanionConnect(c *gin.Context) {
	if h == nil || h.Platform == nil {
		v5HTTPError(c, ErrV5PlatformUnavailable)
		return
	}
	var q struct {
		Code      string `json:"pairing_code"`
		Name      string `json:"name"`
		ProxyMode string `json:"proxy_mode"`
	}
	if !companionJSON(c, 4096, "pairing_code name proxy_mode", map[string]v5Check{"pairing_code": v5String(32, 32, `^[a-f0-9]{32}$`), "name": v5String(1, 80, ""), "proxy_mode": v5Enum("direct", "http", "https", "socks5")}, &q) {
		return
	}
	token, client, err := h.clients().ConnectWithMode(c.Request.Context(), q.Code, q.Name, q.ProxyMode)
	if err != nil {
		v5HTTPError(c, ErrV5Denied)
		return
	}
	response.Success(c, gin.H{"token": token, "client": client})
}
func (h *V5HTTPHandler) CompanionPoll(c *gin.Context) {
	if h == nil || h.Platform == nil {
		v5HTTPError(c, ErrV5PlatformUnavailable)
		return
	}
	var q struct{}
	if !companionJSON(c, 64, "", map[string]v5Check{}, &q) {
		return
	}
	job, err := h.clients().Poll(c.Request.Context(), companionToken(c))
	if err != nil {
		v5HTTPError(c, ErrV5Denied)
		return
	}
	response.Success(c, gin.H{"job": job})
}
func (h *V5HTTPHandler) CompanionResult(c *gin.Context) {
	if h == nil || h.Platform == nil {
		v5HTTPError(c, ErrV5PlatformUnavailable)
		return
	}
	if h.clients().Validate(c.Request.Context(), companionToken(c)) != nil {
		v5HTTPError(c, ErrV5Denied)
		return
	}
	var q struct {
		ID     string `json:"job_id"`
		Bytes  []byte `json:"bytes"`
		Failed bool   `json:"failed"`
	}
	if !companionJSON(c, 1500000, "job_id bytes failed", map[string]v5Check{"job_id": v5String(32, 32, `^[a-f0-9]{32}$`), "bytes": v5String(0, 1398104, ""), "failed": func(v any) bool { _, ok := v.(bool); return ok }}, &q) {
		return
	}
	if err := h.clients().Complete(c.Request.Context(), companionToken(c), q.ID, q.Bytes, q.Failed); err != nil {
		v5HTTPError(c, ErrV5Denied)
		return
	}
	response.Success(c, gin.H{"received": true})
}

var companionFiles = map[string]string{"windows-amd64": "campusos-web-companion-windows-amd64.exe", "linux-amd64": "campusos-web-companion-linux-amd64", "linux-arm64": "campusos-web-companion-linux-arm64", "darwin-arm64": "campusos-web-companion-darwin-arm64", "darwin-amd64": "campusos-web-companion-darwin-amd64"}

func (h *V5HTTPHandler) CompanionDownload(c *gin.Context) {
	if _, err := h.resolver(c, false)(c.Request.Context()); err != nil {
		v5HTTPError(c, ErrV5Denied)
		return
	}
	name, ok := companionFiles[c.Param("platform")]
	if !ok || h.CompanionToolsDir == "" || c.Request.URL.RawQuery != "" {
		v5HTTPError(c, ErrV5NotFound)
		return
	}
	root, err := os.OpenRoot(h.CompanionToolsDir)
	if err != nil {
		v5HTTPError(c, ErrV5NotFound)
		return
	}
	defer root.Close()
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > 32<<20 {
		v5HTTPError(c, ErrV5NotFound)
		return
	}
	file, err := root.Open(name)
	if err != nil {
		v5HTTPError(c, ErrV5NotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(before, info) {
		v5HTTPError(c, ErrV5NotFound)
		return
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		v5HTTPError(c, ErrV5PlatformUnavailable)
		return
	}
	if _, err = file.Seek(0, 0); err != nil {
		v5HTTPError(c, ErrV5PlatformUnavailable)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Companion-SHA256", hex.EncodeToString(hash.Sum(nil)))
	c.DataFromReader(http.StatusOK, info.Size(), "application/octet-stream", file, map[string]string{"Content-Disposition": mime.FormatMediaType("attachment", map[string]string{"filename": name})})
}
