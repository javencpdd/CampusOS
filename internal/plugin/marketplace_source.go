package plugin

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
)

const (
	MarketplaceSourceEnabled  = "enabled"
	MarketplaceSourceDisabled = "disabled"
	marketplaceCatalogVersion = "campusos.plugin-market/v1"
	maxMarketplaceCatalogSize = int64(2 * 1024 * 1024)
)

// MarketplaceSource is a host-governed allowlist entry. Its public key is a
// trust certificate, not a Secret; it is intentionally omitted from ordinary
// API responses so users only need to know the source identity and state.
type MarketplaceSource struct {
	ID             string    `json:"id"`
	DisplayName    string    `json:"display_name"`
	CatalogURL     string    `json:"catalog_url"`
	PublicKey      string    `json:"-"`
	KeyFingerprint string    `json:"key_fingerprint"`
	Status         string    `json:"status"`
	CreatedBy      string    `json:"created_by,omitempty"`
	UpdatedBy      string    `json:"updated_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// MarketplaceListing is a signed, source-owned catalog item. It is metadata
// only: an approval never downloads or executes its package. Administrators
// still use the normal verified package import flow to install a release.
type MarketplaceListing struct {
	SourceID    string `json:"source_id"`
	PluginID    string `json:"plugin_id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Publisher   string `json:"publisher"`
	ListingURL  string `json:"listing_url"`
	PackageURL  string `json:"package_url"`
}

// MarketplaceCatalogClient fetches a signed catalog only from a persisted
// allowlisted source. It is a small port so tests never need a real network.
type MarketplaceCatalogClient interface {
	Search(ctx context.Context, source MarketplaceSource, query string) ([]MarketplaceListing, error)
}

// UserInstallProvisioner creates host-owned per-user setup after the user
// explicitly adds a plugin. It receives no plugin-controlled filesystem path.
type UserInstallProvisioner func(ctx context.Context, pluginName, userID string) error

type httpsMarketplaceCatalogClient struct{ client *http.Client }

func NewHTTPSMarketplaceCatalogClient() MarketplaceCatalogClient {
	return &httpsMarketplaceCatalogClient{client: &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (c *httpsMarketplaceCatalogClient) Search(ctx context.Context, source MarketplaceSource, query string) ([]MarketplaceListing, error) {
	if err := validateMarketplaceSource(source); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(source.CatalogURL)
	if err != nil {
		return nil, fmt.Errorf("%w: trusted market catalog URL is invalid", ErrMarketInvalidInput)
	}
	values := endpoint.Query()
	values.Set("q", strings.TrimSpace(query))
	endpoint.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: trusted market cannot be reached", ErrMarketNotFound)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: trusted market returned HTTP %d", ErrMarketNotFound, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMarketplaceCatalogSize+1))
	if err != nil || int64(len(body)) > maxMarketplaceCatalogSize {
		return nil, fmt.Errorf("%w: trusted market catalog response is invalid", ErrMarketInvalidInput)
	}
	publicKey, _ := base64.StdEncoding.DecodeString(source.PublicKey)
	signature, signatureErr := base64.StdEncoding.DecodeString(strings.TrimSpace(response.Header.Get("X-CampusOS-Market-Signature")))
	if signatureErr != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), body, signature) {
		return nil, fmt.Errorf("%w: trusted market catalog signature verification failed", ErrMarketDenied)
	}
	var payload struct {
		APIVersion string               `json:"api_version"`
		SourceID   string               `json:"source_id"`
		Items      []MarketplaceListing `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.APIVersion != marketplaceCatalogVersion || payload.SourceID != source.ID {
		return nil, fmt.Errorf("%w: trusted market catalog contract is invalid", ErrMarketInvalidInput)
	}
	items := make([]MarketplaceListing, 0, len(payload.Items))
	for _, item := range payload.Items {
		item.SourceID = source.ID
		if err := validateMarketplaceListing(source, item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *MarketService) SaveMarketplaceSource(ctx context.Context, source MarketplaceSource, actorID string) (MarketplaceSource, error) {
	if !s.Available() {
		return MarketplaceSource{}, ErrMarketUnsupported
	}
	if err := validateMarketplaceSource(source); err != nil {
		return MarketplaceSource{}, err
	}
	now := s.now()
	if source.CreatedAt.IsZero() {
		source.CreatedAt = now
	}
	if source.CreatedBy == "" {
		source.CreatedBy = actorID
	}
	source.UpdatedAt, source.UpdatedBy = now, actorID
	source.KeyFingerprint = marketplaceKeyFingerprint(source.PublicKey)
	saved, err := s.store.UpsertMarketplaceSource(ctx, source)
	if err == nil {
		s.audit(ctx, "market:"+saved.ID, actorID, "marketplace.source.save", "success", map[string]interface{}{"status": saved.Status, "catalog_url": saved.CatalogURL})
	}
	return saved, err
}

func (s *MarketService) MarketplaceSources(ctx context.Context, enabledOnly bool) ([]MarketplaceSource, error) {
	if !s.Available() {
		return nil, ErrMarketUnsupported
	}
	items, err := s.store.ListMarketplaceSources(ctx, enabledOnly)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].KeyFingerprint = marketplaceKeyFingerprint(items[index].PublicKey)
	}
	return items, nil
}

func (s *MarketService) DeleteMarketplaceSource(ctx context.Context, sourceID, actorID string) error {
	if !s.Available() {
		return ErrMarketUnsupported
	}
	if err := ValidatePluginName(strings.TrimSpace(sourceID)); err != nil {
		return fmt.Errorf("%w: invalid trusted market ID", ErrMarketInvalidInput)
	}
	if err := s.store.DeleteMarketplaceSource(ctx, sourceID); err != nil {
		return err
	}
	s.audit(ctx, "market:"+sourceID, actorID, "marketplace.source.delete", "success", nil)
	return nil
}

func (s *MarketService) SearchMarketplace(ctx context.Context, sourceID, query, actorID string) ([]MarketplaceListing, error) {
	source, err := s.enabledMarketplaceSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if len(query) > 128 {
		return nil, fmt.Errorf("%w: market search text is too long", ErrMarketInvalidInput)
	}
	items, err := s.marketplaces.Search(ctx, source, query)
	if err != nil {
		s.audit(ctx, "market:"+sourceID, actorID, "marketplace.search", "failed", map[string]interface{}{"query": query})
		return nil, err
	}
	if len(items) > 100 {
		return nil, fmt.Errorf("%w: trusted market returned too many listings", ErrMarketInvalidInput)
	}
	for index := range items {
		// The transport validates this too. Keep the service-level validation so
		// an alternative platform integration cannot accidentally reintroduce an
		// arbitrary URL or cross-marketplace listing bypass.
		items[index].SourceID = source.ID
		if err := validateMarketplaceListing(source, items[index]); err != nil {
			return nil, err
		}
	}
	s.audit(ctx, "market:"+sourceID, actorID, "marketplace.search", "success", map[string]interface{}{"query": query, "count": len(items)})
	return items, nil
}

// RequestMarketplaceInstall resolves a fresh signed source listing before
// persisting it. A client-provided link is never trusted or stored.
func (s *MarketService) RequestMarketplaceInstall(ctx context.Context, sourceID, pluginID, userID, message string) (InstallRequest, error) {
	if err := ValidatePluginName(strings.TrimSpace(pluginID)); err != nil {
		return InstallRequest{}, fmt.Errorf("%w: invalid requested plugin ID", ErrMarketInvalidInput)
	}
	message = strings.TrimSpace(message)
	if len(message) > 1000 {
		return InstallRequest{}, fmt.Errorf("%w: request message is too long", ErrMarketInvalidInput)
	}
	items, err := s.SearchMarketplace(ctx, sourceID, pluginID, userID)
	if err != nil {
		return InstallRequest{}, err
	}
	var selected *MarketplaceListing
	for index := range items {
		if items[index].PluginID == pluginID {
			selected = &items[index]
			break
		}
	}
	if selected == nil {
		return InstallRequest{}, ErrMarketNotFound
	}
	request := InstallRequest{
		ID: idgen.New(), PluginName: selected.PluginID, MarketSourceID: sourceID, MarketPluginID: selected.PluginID,
		MarketListingURL: selected.ListingURL, MarketPackageURL: selected.PackageURL, MarketVersion: selected.Version,
		MarketPublisher: selected.Publisher, UserID: userID, Message: message, Status: RequestPending, CreatedAt: s.now(),
	}
	created, err := s.store.CreateInstallRequest(ctx, request)
	if err == nil {
		s.audit(ctx, "market:"+sourceID, userID, "marketplace.request", "success", map[string]interface{}{"plugin_id": selected.PluginID, "version": selected.Version})
	}
	return created, err
}

func (s *MarketService) enabledMarketplaceSource(ctx context.Context, sourceID string) (MarketplaceSource, error) {
	if !s.Available() || s.marketplaces == nil {
		return MarketplaceSource{}, ErrMarketUnsupported
	}
	if err := ValidatePluginName(strings.TrimSpace(sourceID)); err != nil {
		return MarketplaceSource{}, fmt.Errorf("%w: invalid trusted market ID", ErrMarketInvalidInput)
	}
	source, err := s.store.GetMarketplaceSource(ctx, sourceID)
	if err != nil {
		return MarketplaceSource{}, err
	}
	if source.Status != MarketplaceSourceEnabled {
		return MarketplaceSource{}, ErrMarketDenied
	}
	return source, nil
}

func validateMarketplaceSource(source MarketplaceSource) error {
	if err := ValidatePluginName(strings.TrimSpace(source.ID)); err != nil {
		return fmt.Errorf("%w: trusted market ID is invalid", ErrMarketInvalidInput)
	}
	if strings.TrimSpace(source.DisplayName) == "" || len([]rune(source.DisplayName)) > 120 {
		return fmt.Errorf("%w: trusted market display name is invalid", ErrMarketInvalidInput)
	}
	if source.Status != MarketplaceSourceEnabled && source.Status != MarketplaceSourceDisabled {
		return fmt.Errorf("%w: trusted market status is invalid", ErrMarketInvalidInput)
	}
	endpoint, err := url.Parse(source.CatalogURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return fmt.Errorf("%w: trusted market catalog must use HTTPS", ErrMarketInvalidInput)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(source.PublicKey))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: trusted market certificate must be an Ed25519 public key encoded as Base64", ErrMarketInvalidInput)
	}
	return nil
}

func validateMarketplaceListing(source MarketplaceSource, item MarketplaceListing) error {
	if err := ValidatePluginName(strings.TrimSpace(item.PluginID)); err != nil || strings.TrimSpace(item.DisplayName) == "" || len([]rune(item.Description)) > 2000 || len(item.Version) > 64 || strings.TrimSpace(item.Version) == "" {
		return fmt.Errorf("%w: trusted market listing is invalid", ErrMarketInvalidInput)
	}
	for _, value := range []string{item.ListingURL, item.PackageURL} {
		candidate, err := url.Parse(value)
		base, baseErr := url.Parse(source.CatalogURL)
		if err != nil || baseErr != nil || candidate.Scheme != "https" || candidate.Host != base.Host || candidate.User != nil || candidate.Fragment != "" {
			return fmt.Errorf("%w: trusted market listing URL escapes the configured source", ErrMarketInvalidInput)
		}
	}
	return nil
}

func marketplaceKeyFingerprint(encoded string) string {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(key) == 0 {
		return ""
	}
	sum := sha256.Sum256(key)
	return "sha256:" + hex.EncodeToString(sum[:8])
}
