package plugin

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/campusos/CampusOS/internal/platform/webcompanion"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

const V5WebLocalRead = "web.local.read"
const V5WebPublicRead = "web.public.read"
const V5ArticleCompose = "community.article.compose"

var publicReadSlots = make(chan struct{}, 4)

type V5UIActionRequest struct {
	ClientID string `json:"client_id,omitempty"`
	Proof    string `json:"proof"`
	Action   string `json:"action"`
	URL      string `json:"url,omitempty"`
	Kind     string `json:"kind,omitempty"`
}
type V5UIActionResult struct {
	Clients        []webcompanion.Reader `json:"clients,omitempty"`
	Scope          string                `json:"scope,omitempty"`
	LocalAvailable bool                  `json:"local_available,omitempty"`
	PairingCode    string                `json:"pairing_code,omitempty"`
	ExpiresAt      int64                 `json:"expires_at,omitempty"`
	Authorized     bool                  `json:"authorized,omitempty"`
	Bytes          []byte                `json:"bytes,omitempty"`
	MIME           string                `json:"mime,omitempty"`
}

// The platform enforces a published capability, never a plugin name, feed
// schema, parsing rule or publisher-specific business operation.
func (s *V5Platform) authorizeUIAction(ctx context.Context, coordinate V5Coordinate, proof, capability string, resolve identity.SessionPrincipalResolver) error {
	principal, err := resolve(ctx)
	if err != nil || principal.Actor.Kind != "user" || principal.Audience != "user" {
		return ErrV5Denied
	}
	bound := func(check context.Context) (identity.PrincipalContext, error) {
		current, err := resolve(check)
		if err != nil || !reflect.DeepEqual(current, principal) {
			return identity.PrincipalContext{}, ErrV5Denied
		}
		return current, nil
	}
	return s.withUI(ctx, coordinate, false, bound, func(facts V5UIFacts, pkg *PluginV5Package) error {
		if facts.Proof != proof {
			return ErrV5Denied
		}
		grant, err := s.grant(ctx, coordinate)
		if err != nil {
			return err
		}
		declared, approved := false, false
		for _, item := range pkg.Manifest.Consumes {
			declared = declared || item.Capability == capability && item.Scope.Kind == "self"
		}
		for _, item := range grant.Consumes {
			approved = approved || item.Capability == capability && item.Scope.Kind == "self"
		}
		if !declared || !approved {
			return ErrV5Denied
		}
		return nil
	})
}

func readPublicResource(ctx context.Context, target string, max int64) ([]byte, string, error) {
	return readPublicResourceVia(ctx, target, max, nil, "")
}
func readPublicResourceVia(ctx context.Context, target string, max int64, proxies *security.ProxyRegistry, profile string) ([]byte, string, error) {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || len(target) > 2048 {
		return nil, "", ErrV5Invalid
	}
	broker, err := proxies.NewBroker(security.EgressPolicy{AllowedOrigins: []string{u.Scheme + "://" + u.Host}, MaxRequestBytes: 1024, MaxResponseBytes: max, Timeout: 8 * time.Second, MaxRedirects: 0}, profile)
	if err != nil {
		return nil, "", ErrV5Invalid
	}
	result, err := broker.Do(ctx, security.EgressRequest{Method: "GET", URL: target, DenyRedirects: true, Headers: http.Header{"Accept": []string{"application/xml, text/xml, text/plain, image/png, image/jpeg, image/gif, image/webp"}, "User-Agent": []string{"CampusOS-PublicResource/1.0"}}})
	if err != nil || result.StatusCode != http.StatusOK {
		return nil, "", ErrV5PlatformUnavailable
	}
	return result.Body, http.DetectContentType(result.Body), nil
}

func (h *V5HTTPHandler) uiAction(ctx context.Context, c V5Coordinate, q V5UIActionRequest, resolve identity.SessionPrincipalResolver) (V5UIActionResult, error) {
	var empty V5UIActionResult
	if !v5Digest(q.Proof) {
		return empty, ErrV5Invalid
	}
	if strings.HasPrefix(q.Action, "web.clients.") {
		return h.clientAction(ctx, c, q, resolve)
	}
	capability := V5WebPublicRead
	if q.Action == "editor.article.import" {
		if q.URL != "" || q.Kind != "" {
			return empty, ErrV5Invalid
		}
		capability = V5ArticleCompose
	} else if q.Action != "web.read" || (q.Kind != "document" && q.Kind != "image") || len(q.URL) == 0 || len(q.URL) > 2048 {
		return empty, ErrV5Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := h.Platform.authorizeUIAction(ctx, c, q.Proof, capability, resolve); err != nil {
		return empty, err
	}
	if q.ClientID != "" {
		if err := h.Platform.authorizeUIAction(ctx, c, q.Proof, V5WebLocalRead, resolve); err != nil {
			return empty, err
		}
	}
	if q.Action == "editor.article.import" {
		return V5UIActionResult{Authorized: true}, nil
	}
	select {
	case publicReadSlots <- struct{}{}:
		defer func() { <-publicReadSlots }()
	default:
		return empty, ErrV5PlatformUnavailable
	}
	fetch := h.publicRead
	if q.ClientID != "" {
		fetch = func(ctx context.Context, target string, max int64) ([]byte, string, error) {
			owner, err := h.clientOwner(ctx, c, resolve)
			if err != nil {
				return nil, "", err
			}
			checkTarget := h.clientTarget
			if checkTarget == nil {
				checkTarget = webcompanion.ValidateTarget
			}
			if err := checkTarget(ctx, target); err != nil {
				return nil, "", ErrV5Invalid
			}
			raw, err := h.clients().Fetch(ctx, owner, q.ClientID, target, max, func(check context.Context) bool {
				return h.Platform.authorizeUIAction(check, c, q.Proof, V5WebLocalRead, resolve) == nil && h.Platform.authorizeUIAction(check, c, q.Proof, V5WebPublicRead, resolve) == nil
			})
			if err != nil {
				return nil, "", ErrV5PlatformUnavailable
			}
			return raw, http.DetectContentType(raw), nil
		}
	} else if fetch == nil {
		fetch = func(ctx context.Context, target string, max int64) ([]byte, string, error) {
			return readPublicResourceVia(ctx, target, max, h.WebProxies, h.WebProxyProfile)
		}
	}
	raw, mime, err := fetch(ctx, q.URL, 1<<20)
	if err != nil {
		return empty, err
	}
	if len(raw) > 1<<20 {
		return empty, ErrV5Invalid
	}
	if q.Kind == "image" {
		if h.ValidatePublicImage == nil {
			return empty, ErrV5PlatformUnavailable
		}
		raw, mime, err = h.ValidatePublicImage(raw)
		if err != nil || len(raw) > 1<<20 {
			return empty, ErrV5Invalid
		}
	}
	if err = h.Platform.authorizeUIAction(ctx, c, q.Proof, capability, resolve); err != nil {
		return empty, err
	}
	if q.ClientID != "" {
		if err = h.Platform.authorizeUIAction(ctx, c, q.Proof, V5WebLocalRead, resolve); err != nil {
			return empty, err
		}
	}
	return V5UIActionResult{Bytes: raw, MIME: mime}, nil
}

func (h *V5HTTPHandler) UIAction(c *gin.Context) {
	if !v5HTTPStart(c) || !v5CSRF(c, false) {
		return
	}
	if h == nil || h.Platform == nil {
		v5HTTPError(c, ErrV5PlatformUnavailable)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	raw, err := io.ReadAll(c.Request.Body)
	value, ok := v5JSON(raw)
	shape := v5Object("proof action", map[string]v5Check{"client_id": v5String(32, 32, `^[a-f0-9]{32}$`), "proof": v5Digest, "action": v5Enum("web.read", "editor.article.import", "web.clients.status", "web.clients.pair", "web.clients.select", "web.clients.disconnect"), "url": v5String(1, 2048, `(?s)^.*$`), "kind": v5Enum("document", "image")})
	var q V5UIActionRequest
	if err != nil || !ok || !shape(value) || strictPluginV5JSON(raw, &q) != nil || c.Request.URL.RawQuery != "" {
		v5HTTPError(c, ErrV5Invalid)
		return
	}
	result, err := h.uiAction(c.Request.Context(), V5Coordinate{c.Param("publisher"), c.Param("key"), c.Param("version")}, q, h.resolver(c, false))
	if err != nil {
		v5HTTPError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, result)
}
