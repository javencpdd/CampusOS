package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrEndpointNotFound = errors.New("webhook endpoint not found")

const (
	webhookSignatureVersion  = "v1"
	maxWebhookRedirects      = 3
	maxWebhookResponseBody   = 64 << 10
	maxWebhookResponseHeader = 32 << 10
)

var errWebhookResponseTooLarge = errors.New("webhook response body exceeds the 64 KiB safety limit")

type Endpoint struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Secret     string   `json:"-"`
	Events     []string `json:"events"`
	Enabled    bool     `json:"enabled"`
	MaxRetries int      `json:"max_retries"`
	TimeoutMS  int      `json:"timeout_ms"`
	// MaxConcurrent and RateLimitPerMinute apply to one endpoint. They are
	// intentionally bounded by Service before use; a webhook configuration
	// cannot turn the worker into an unbounded outbound traffic generator.
	MaxConcurrent      int       `json:"max_concurrent"`
	RateLimitPerMinute int       `json:"rate_limit_per_minute"`
	CreatedBy          string    `json:"created_by"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// EgressPolicy is intentionally restrictive by default. It guards both
// endpoint registration and every outbound dial so DNS rebinding cannot turn a
// previously valid hostname into an internal-network request.
type EgressPolicy struct {
	AllowedHosts        []string
	AllowPrivateNetwork bool
}

func (p EgressPolicy) normalizedAllowedHosts() []string {
	items := make([]string, 0, len(p.AllowedHosts))
	for _, host := range p.AllowedHosts {
		host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
		if host != "" {
			items = append(items, host)
		}
	}
	return items
}

type Delivery struct {
	ID             string     `json:"id"`
	EndpointID     string     `json:"endpoint_id"`
	EventID        string     `json:"event_id"`
	OutboxEventID  string     `json:"outbox_event_id,omitempty"`
	DeliveryKey    string     `json:"delivery_key,omitempty"`
	EventType      string     `json:"event_type"`
	TargetURL      string     `json:"target_url"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	ResponseStatus int        `json:"response_status"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	DeadLetteredAt *time.Time `json:"dead_lettered_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Summary struct {
	EndpointTotal    int64 `json:"endpoint_total"`
	EnabledTotal     int64 `json:"enabled_total"`
	DeliveryTotal    int64 `json:"delivery_total"`
	SuccessTotal     int64 `json:"success_total"`
	FailedTotal      int64 `json:"failed_total"`
	LastFailureTotal int64 `json:"last_failure_total"`
}

type Store interface {
	CreateEndpoint(ctx context.Context, endpoint *Endpoint) error
	ListEndpoints(ctx context.Context) ([]*Endpoint, error)
	GetEndpoint(ctx context.Context, id string) (*Endpoint, error)
	UpdateEndpointEnabled(ctx context.Context, id string, enabled bool) error
	SaveDelivery(ctx context.Context, delivery *Delivery) error
	ListDeliveries(ctx context.Context, endpointID string, limit int) ([]*Delivery, error)
	Summary(ctx context.Context) (*Summary, error)
}

type Service struct {
	store       Store
	metrics     *observability.Collector
	reliability *reliability.Service
	egress      EgressPolicy

	limiterMu sync.Mutex
	limiters  map[string]*endpointLimiter
}

type endpointLimiter struct {
	maxConcurrent int
	ratePerMinute int
	inFlight      int
	windowStarted time.Time
	windowCount   int
}

func NewService(store Store, metrics *observability.Collector, reliable ...*reliability.Service) *Service {
	service := &Service{store: store, metrics: metrics, limiters: make(map[string]*endpointLimiter)}
	if len(reliable) > 0 {
		service.reliability = reliable[0]
	}
	return service
}

func (s *Service) SetEgressPolicy(policy EgressPolicy) {
	policy.AllowedHosts = policy.normalizedAllowedHosts()
	s.egress = policy
}

func (s *Service) Register(bus eventbus.EventBus) error {
	if s == nil {
		return nil
	}
	eventTypes := []string{
		eventbus.EventUserCreated,
		eventbus.EventThreadCreated,
		eventbus.EventThreadUpdated,
		eventbus.EventThreadDeleted,
		eventbus.EventPostCreated,
		eventbus.EventCategoryCreated,
	}
	if s.reliability != nil {
		for _, eventType := range eventTypes {
			s.reliability.RegisterConsumer(eventType, "webhook.fanout", func(ctx context.Context, event reliability.Event) error {
				legacy := eventbus.NewEvent(event.Type, "campusos.reliability", event.AggregateType+"."+event.AggregateID, nil)
				legacy.ID = event.ID
				legacy.Time = event.CreatedAt.UTC().Format(time.RFC3339Nano)
				if len(event.Payload) > 0 {
					if err := json.Unmarshal(event.Payload, &legacy.Data); err != nil {
						return reliability.Permanent(fmt.Errorf("decode webhook fan-out payload: %w", err))
					}
				}
				return s.DeliverEvent(ctx, legacy)
			})
		}
		s.reliability.RegisterHandler("webhook.delivery", s.handleQueuedDelivery)
	}
	if bus == nil {
		return nil
	}
	for _, eventType := range eventTypes {
		if err := bus.Subscribe(eventType, func(ctx context.Context, event eventbus.Event) error {
			return s.DeliverEvent(ctx, event)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CreateEndpoint(ctx context.Context, endpoint *Endpoint) error {
	if endpoint == nil {
		return errors.New("endpoint is required")
	}
	endpoint.Name = strings.TrimSpace(endpoint.Name)
	endpoint.URL = strings.TrimSpace(endpoint.URL)
	if endpoint.Name == "" {
		return errors.New("name is required")
	}
	if err := validateURLWithPolicy(endpoint.URL, s.egress); err != nil {
		return err
	}
	if endpoint.ID == "" {
		endpoint.ID = fmt.Sprintf("%d", idgen.New())
	}
	if endpoint.MaxRetries < 0 {
		endpoint.MaxRetries = 0
	}
	if endpoint.MaxRetries == 0 {
		endpoint.MaxRetries = 2
	}
	if endpoint.TimeoutMS <= 0 {
		endpoint.TimeoutMS = 5000
	}
	if endpoint.TimeoutMS > 30000 {
		endpoint.TimeoutMS = 30000
	}
	if endpoint.MaxConcurrent <= 0 {
		endpoint.MaxConcurrent = 2
	}
	if endpoint.MaxConcurrent > 16 {
		endpoint.MaxConcurrent = 16
	}
	if endpoint.RateLimitPerMinute <= 0 {
		endpoint.RateLimitPerMinute = 60
	}
	if endpoint.RateLimitPerMinute > 600 {
		endpoint.RateLimitPerMinute = 600
	}
	now := time.Now().UTC()
	if endpoint.CreatedAt.IsZero() {
		endpoint.CreatedAt = now
	}
	endpoint.UpdatedAt = now
	endpoint.Events = normalizeEvents(endpoint.Events)
	return s.store.CreateEndpoint(ctx, endpoint)
}

func (s *Service) ListEndpoints(ctx context.Context) ([]*Endpoint, error) {
	return s.store.ListEndpoints(ctx)
}

func (s *Service) Summary(ctx context.Context) (*Summary, error) {
	return s.store.Summary(ctx)
}

func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) error {
	return s.store.UpdateEndpointEnabled(ctx, id, enabled)
}

func (s *Service) ListDeliveries(ctx context.Context, endpointID string, limit int) ([]*Delivery, error) {
	return s.store.ListDeliveries(ctx, endpointID, limit)
}

func (s *Service) TestEndpoint(ctx context.Context, endpointID string) (*Delivery, error) {
	endpoint, err := s.store.GetEndpoint(ctx, endpointID)
	if err != nil {
		return nil, err
	}
	event := eventbus.NewEvent("webhook.test", "campusos.webhook", "webhook."+endpointID, map[string]interface{}{
		"message": "CampusOS webhook test event",
	})
	return s.deliverToEndpoint(ctx, endpoint, event, "", 1, 0)
}

func (s *Service) DeliverEvent(ctx context.Context, event eventbus.Event) error {
	endpoints, err := s.store.ListEndpoints(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, endpoint := range endpoints {
		if endpoint.Enabled && endpointMatches(endpoint, event.Type) {
			if s.reliability == nil {
				if _, err := s.deliverToEndpoint(ctx, endpoint, event, "", 1, 0); err != nil && firstErr == nil {
					firstErr = err
				}
				continue
			}
			if err := s.enqueueDelivery(ctx, endpoint, event); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

type queuedDelivery struct {
	EndpointID string         `json:"endpoint_id"`
	Event      eventbus.Event `json:"event"`
}

func (s *Service) enqueueDelivery(ctx context.Context, endpoint *Endpoint, event eventbus.Event) error {
	durable, err := reliability.NewEvent("webhook.delivery", "webhook_endpoint", endpoint.ID, queuedDelivery{EndpointID: endpoint.ID, Event: event})
	if err != nil {
		return err
	}
	durable.IdempotencyKey = "webhook.delivery:" + endpoint.ID + ":" + event.ID
	durable.MaxAttempts = endpoint.MaxRetries + 1
	_, err = s.reliability.Enqueue(ctx, durable)
	return err
}

func (s *Service) handleQueuedDelivery(ctx context.Context, durable reliability.Event) error {
	var queued queuedDelivery
	if err := json.Unmarshal(durable.Payload, &queued); err != nil {
		return reliability.Permanent(fmt.Errorf("decode webhook delivery payload: %w", err))
	}
	endpoint, err := s.store.GetEndpoint(ctx, queued.EndpointID)
	if errors.Is(err, ErrEndpointNotFound) {
		return nil // endpoint was removed or unavailable after the event was queued
	}
	if err != nil {
		return reliability.Retryable(err, 0)
	}
	if !endpoint.Enabled || !endpointMatches(endpoint, queued.Event.Type) {
		return nil
	}
	_, err = s.deliverToEndpoint(ctx, endpoint, queued.Event, durable.ID, durable.Attempts, durable.MaxAttempts)
	return err
}

func (s *Service) deliverToEndpoint(ctx context.Context, endpoint *Endpoint, event eventbus.Event, outboxEventID string, attempts, maxAttempts int) (*Delivery, error) {
	now := time.Now().UTC()
	if attempts <= 0 {
		attempts = 1
	}
	delivery := &Delivery{
		ID:            fmt.Sprintf("%d", idgen.New()),
		EndpointID:    endpoint.ID,
		EventID:       event.ID,
		OutboxEventID: outboxEventID,
		DeliveryKey:   endpoint.ID + ":" + event.ID,
		EventType:     event.Type,
		TargetURL:     endpoint.URL,
		Status:        "pending",
		Attempts:      attempts,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	body, err := json.Marshal(event)
	if err != nil {
		delivery.Status = "failed"
		delivery.ErrorMessage = err.Error()
		_ = s.store.SaveDelivery(ctx, delivery)
		return delivery, err
	}

	release, wait, limited := s.acquireEndpoint(endpoint, now)
	if limited {
		delivery.Status = "retry"
		next := now.Add(wait)
		delivery.NextAttemptAt = &next
		delivery.ErrorMessage = "webhook endpoint concurrency or rate limit reached"
		delivery.UpdatedAt = now
		_ = s.store.SaveDelivery(ctx, delivery)
		return delivery, reliability.Retryable(errors.New(delivery.ErrorMessage), wait)
	}
	defer release()

	statusCode, retryAfter, sendErr := postSignedJSON(ctx, endpoint, event.ID, body, s.egress)
	delivery.ResponseStatus = statusCode
	if sendErr == nil && statusCode >= 200 && statusCode < 300 {
		delivery.Status = "success"
		delivery.ErrorMessage = ""
		delivery.UpdatedAt = time.Now().UTC()
		_ = s.store.SaveDelivery(ctx, delivery)
		if s.metrics != nil {
			s.metrics.RecordExternal("webhook.delivery", true)
		}
		return delivery, nil
	}
	if sendErr == nil {
		sendErr = fmt.Errorf("unexpected status %d", statusCode)
	}
	delivery.ErrorMessage = sendErr.Error()
	if retryableHTTPStatus(statusCode) && (maxAttempts <= 0 || attempts < maxAttempts) {
		delivery.Status = "retry"
		if retryAfter <= 0 {
			retryAfter = time.Second * time.Duration(1<<min(attempts-1, 6))
		}
		next := time.Now().UTC().Add(retryAfter)
		delivery.NextAttemptAt = &next
	} else {
		delivery.Status = "failed"
		if outboxEventID != "" {
			dead := time.Now().UTC()
			delivery.DeadLetteredAt = &dead
		}
	}
	delivery.UpdatedAt = time.Now().UTC()
	_ = s.store.SaveDelivery(ctx, delivery)
	if s.metrics != nil {
		s.metrics.RecordExternal("webhook.delivery", false)
	}
	if delivery.Status == "retry" {
		return delivery, reliability.Retryable(sendErr, time.Until(*delivery.NextAttemptAt))
	}
	return delivery, reliability.Permanent(sendErr)
}

func SignPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// SignPayloadV1 covers a timestamp and the body. Receivers reject stale
// timestamps and deduplicate X-CampusOS-Event-ID; the legacy SignPayload
// helper remains for compatibility with existing receiver tests only.
func SignPayloadV1(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return webhookSignatureVersion + "=" + hex.EncodeToString(mac.Sum(nil))
}

func postSignedJSON(ctx context.Context, endpoint *Endpoint, eventID string, body []byte, policy EgressPolicy) (int, time.Duration, error) {
	if endpoint == nil {
		return 0, 0, errors.New("webhook endpoint is required")
	}
	if err := validateURLWithPolicy(endpoint.URL, policy); err != nil {
		return 0, 0, err
	}
	timeout := time.Duration(endpoint.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	transport := newSafeWebhookTransport(policy, timeout)
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxWebhookRedirects {
				return fmt.Errorf("webhook redirect limit of %d exceeded", maxWebhookRedirects)
			}
			return validateURLWithPolicy(request.URL.String(), policy)
		},
	}
	req, err := newSignedRequest(ctx, endpoint, eventID, body)
	if err != nil {
		return 0, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if err := readWebhookResponseBody(resp.Body); err != nil {
		return resp.StatusCode, 0, err
	}
	return resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After"), time.Now().UTC()), nil
}

func newSafeWebhookTransport(policy EgressPolicy, timeout time.Duration) *http.Transport {
	return &http.Transport{
		// Do not inherit HTTP proxy environment variables: a proxy would bypass
		// the destination resolver and reopen the SSRF boundary this transport
		// deliberately enforces.
		Proxy:                  nil,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           16,
		IdleConnTimeout:        30 * time.Second,
		TLSHandshakeTimeout:    timeout,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: maxWebhookResponseHeader,
		DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			return safeDialContextWithPolicy(dialCtx, network, address, policy)
		},
	}
}

func newSignedRequest(ctx context.Context, endpoint *Endpoint, eventID string, body []byte) (*http.Request, error) {
	if endpoint == nil {
		return nil, errors.New("webhook endpoint is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CampusOS-Webhook/0.11")
	req.Header.Set("X-CampusOS-Signature-Version", webhookSignatureVersion)
	req.Header.Set("X-CampusOS-Timestamp", strconv.FormatInt(time.Now().UTC().Unix(), 10))
	req.Header.Set("X-CampusOS-Event-ID", eventID)
	if endpoint.Secret != "" {
		req.Header.Set("X-CampusOS-Signature", SignPayloadV1(endpoint.Secret, req.Header.Get("X-CampusOS-Timestamp"), body))
	}
	return req, nil
}

func readWebhookResponseBody(reader io.Reader) error {
	responseBody, err := io.ReadAll(io.LimitReader(reader, maxWebhookResponseBody+1))
	if err != nil {
		return err
	}
	if len(responseBody) > maxWebhookResponseBody {
		return errWebhookResponseTooLarge
	}
	return nil
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	const maxRetryAfter = 24 * time.Hour
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds > 0 {
		value := time.Duration(seconds) * time.Second
		if value > maxRetryAfter {
			return maxRetryAfter
		}
		return value
	}
	if at, err := http.ParseTime(raw); err == nil && at.After(now) {
		value := at.Sub(now)
		if value > maxRetryAfter {
			return maxRetryAfter
		}
		return value
	}
	return 0
}

func (s *Service) acquireEndpoint(endpoint *Endpoint, now time.Time) (func(), time.Duration, bool) {
	if endpoint == nil {
		return func() {}, time.Second, true
	}
	maxConcurrent := endpoint.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	ratePerMinute := endpoint.RateLimitPerMinute
	if ratePerMinute <= 0 {
		ratePerMinute = 60
	}
	s.limiterMu.Lock()
	defer s.limiterMu.Unlock()
	limiter := s.limiters[endpoint.ID]
	if limiter == nil || limiter.maxConcurrent != maxConcurrent || limiter.ratePerMinute != ratePerMinute {
		limiter = &endpointLimiter{maxConcurrent: maxConcurrent, ratePerMinute: ratePerMinute, windowStarted: now}
		s.limiters[endpoint.ID] = limiter
	}
	if now.Sub(limiter.windowStarted) >= time.Minute {
		limiter.windowStarted = now
		limiter.windowCount = 0
	}
	if limiter.inFlight >= limiter.maxConcurrent {
		return func() {}, time.Second, true
	}
	if limiter.windowCount >= limiter.ratePerMinute {
		wait := time.Until(limiter.windowStarted.Add(time.Minute))
		if wait <= 0 {
			wait = time.Second
		}
		return func() {}, wait, true
	}
	limiter.inFlight++
	limiter.windowCount++
	return func() {
		s.limiterMu.Lock()
		defer s.limiterMu.Unlock()
		if current := s.limiters[endpoint.ID]; current == limiter && current.inFlight > 0 {
			current.inFlight--
		}
	}, 0, false
}

func retryableHTTPStatus(status int) bool {
	return status == 0 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return safeDialContextWithPolicy(ctx, network, address, EgressPolicy{})
}

func safeDialContextWithPolicy(ctx context.Context, network, address string, policy EgressPolicy) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve webhook host: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("webhook host resolved to no addresses")
	}
	dialer := &net.Dialer{}
	if err := policy.validateHost(host); err != nil {
		return nil, err
	}
	var lastErr error
	for _, address := range addresses {
		ip := address.IP
		if !policy.AllowPrivateNetwork && unsafeWebhookAddress(ip) {
			lastErr = fmt.Errorf("webhook destination %s is not allowed", ip.String())
			continue
		}
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = errors.New("webhook destination is not allowed")
	}
	return nil, lastErr
}

func endpointMatches(endpoint *Endpoint, eventType string) bool {
	if len(endpoint.Events) == 0 {
		return true
	}
	for _, value := range endpoint.Events {
		if value == "*" || value == eventType {
			return true
		}
	}
	return false
}

func normalizeEvents(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	events := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		events = append(events, value)
	}
	return events
}

func validateURL(raw string) error {
	return validateURLWithPolicy(raw, EgressPolicy{})
}

func validateURLWithPolicy(raw string, policy EgressPolicy) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("webhook url must use http or https")
	}
	if parsed.Host == "" {
		return errors.New("webhook url host is required")
	}
	if parsed.User != nil {
		return errors.New("webhook url must not contain user information")
	}
	if parsed.Fragment != "" {
		return errors.New("webhook url must not contain a fragment")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" {
		return errors.New("webhook url hostname is required")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		if !policy.AllowPrivateNetwork {
			return errors.New("webhook url host is not allowed")
		}
	}
	if err := policy.validateHost(host); err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil && !policy.AllowPrivateNetwork && unsafeWebhookAddress(ip) {
		return errors.New("webhook url private or loopback address is not allowed")
	}
	if port := parsed.Port(); port != "" {
		value, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || value == 0 {
			return errors.New("webhook url port is invalid")
		}
	}
	return nil
}

func (p EgressPolicy) validateHost(host string) error {
	allowed := p.normalizedAllowedHosts()
	if len(allowed) == 0 {
		return nil
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	for _, candidate := range allowed {
		if host == candidate || strings.HasSuffix(host, "."+candidate) {
			return nil
		}
	}
	return fmt.Errorf("webhook host %q is not allowed by egress policy", host)
}

func unsafeWebhookAddress(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return true
	}
	return false
}

type MemoryStore struct {
	mu            sync.RWMutex
	endpoints     map[string]*Endpoint
	deliveries    []*Delivery
	deliveryIndex map[string]int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{endpoints: make(map[string]*Endpoint), deliveryIndex: make(map[string]int)}
}

func (s *MemoryStore) CreateEndpoint(_ context.Context, endpoint *Endpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endpoints[endpoint.ID] = cloneEndpoint(endpoint)
	return nil
}

func (s *MemoryStore) ListEndpoints(_ context.Context) ([]*Endpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*Endpoint, 0, len(s.endpoints))
	for _, endpoint := range s.endpoints {
		items = append(items, cloneEndpoint(endpoint))
	}
	return items, nil
}

func (s *MemoryStore) GetEndpoint(_ context.Context, id string) (*Endpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	endpoint, ok := s.endpoints[id]
	if !ok {
		return nil, ErrEndpointNotFound
	}
	return cloneEndpoint(endpoint), nil
}

func (s *MemoryStore) UpdateEndpointEnabled(_ context.Context, id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	endpoint, ok := s.endpoints[id]
	if !ok {
		return ErrEndpointNotFound
	}
	endpoint.Enabled = enabled
	endpoint.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryStore) SaveDelivery(_ context.Context, delivery *Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := delivery.DeliveryKey
	if key == "" {
		key = delivery.ID
	}
	if index, ok := s.deliveryIndex[key]; ok {
		s.deliveries[index] = cloneDelivery(delivery)
		return nil
	}
	s.deliveryIndex[key] = len(s.deliveries)
	s.deliveries = append(s.deliveries, cloneDelivery(delivery))
	return nil
}

func (s *MemoryStore) ListDeliveries(_ context.Context, endpointID string, limit int) ([]*Delivery, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	items := make([]*Delivery, 0, limit)
	for i := len(s.deliveries) - 1; i >= 0 && len(items) < limit; i-- {
		delivery := s.deliveries[i]
		if endpointID != "" && delivery.EndpointID != endpointID {
			continue
		}
		items = append(items, cloneDelivery(delivery))
	}
	return items, nil
}

func (s *MemoryStore) Summary(_ context.Context) (*Summary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	summary := &Summary{EndpointTotal: int64(len(s.endpoints)), DeliveryTotal: int64(len(s.deliveries))}
	for _, endpoint := range s.endpoints {
		if endpoint.Enabled {
			summary.EnabledTotal++
		}
	}
	for _, delivery := range s.deliveries {
		if delivery.Status == "success" {
			summary.SuccessTotal++
		}
		if delivery.Status == "failed" {
			summary.FailedTotal++
		}
	}
	summary.LastFailureTotal = summary.FailedTotal
	return summary, nil
}

type PgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) *PgStore {
	return &PgStore{pool: pool}
}

func (s *PgStore) CreateEndpoint(ctx context.Context, endpoint *Endpoint) error {
	query := `INSERT INTO webhook_endpoints (
			id, name, url, secret, events, enabled, max_retries, timeout_ms, max_concurrent, rate_limit_per_minute, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
	_, err := s.pool.Exec(ctx, query,
		endpoint.ID, endpoint.Name, endpoint.URL, endpoint.Secret, endpoint.Events,
		endpoint.Enabled, endpoint.MaxRetries, endpoint.TimeoutMS, endpoint.MaxConcurrent, endpoint.RateLimitPerMinute,
		endpoint.CreatedBy, endpoint.CreatedAt, endpoint.UpdatedAt,
	)
	return err
}

func (s *PgStore) ListEndpoints(ctx context.Context) ([]*Endpoint, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, url, secret, events, enabled, max_retries, timeout_ms, max_concurrent, rate_limit_per_minute, created_by, created_at, updated_at
		FROM webhook_endpoints WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*Endpoint, 0)
	for rows.Next() {
		endpoint := &Endpoint{}
		if err := rows.Scan(&endpoint.ID, &endpoint.Name, &endpoint.URL, &endpoint.Secret,
			&endpoint.Events, &endpoint.Enabled, &endpoint.MaxRetries, &endpoint.TimeoutMS, &endpoint.MaxConcurrent, &endpoint.RateLimitPerMinute,
			&endpoint.CreatedBy, &endpoint.CreatedAt, &endpoint.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, endpoint)
	}
	return items, rows.Err()
}

func (s *PgStore) GetEndpoint(ctx context.Context, id string) (*Endpoint, error) {
	endpoint := &Endpoint{}
	err := s.pool.QueryRow(ctx, `SELECT id, name, url, secret, events, enabled, max_retries, timeout_ms, max_concurrent, rate_limit_per_minute, created_by, created_at, updated_at
		FROM webhook_endpoints WHERE id = $1 AND deleted_at IS NULL`, id).Scan(
		&endpoint.ID, &endpoint.Name, &endpoint.URL, &endpoint.Secret, &endpoint.Events,
		&endpoint.Enabled, &endpoint.MaxRetries, &endpoint.TimeoutMS, &endpoint.MaxConcurrent, &endpoint.RateLimitPerMinute, &endpoint.CreatedBy,
		&endpoint.CreatedAt, &endpoint.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEndpointNotFound
		}
		return nil, err
	}
	return endpoint, nil
}

func (s *PgStore) UpdateEndpointEnabled(ctx context.Context, id string, enabled bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE webhook_endpoints SET enabled = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, id, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrEndpointNotFound
	}
	return nil
}

func (s *PgStore) SaveDelivery(ctx context.Context, delivery *Delivery) error {
	query := `INSERT INTO webhook_deliveries (
			id, endpoint_id, event_id, event_type, target_url, status, attempts, response_status,
			error_message, created_at, updated_at, outbox_event_id, delivery_key, next_attempt_at, dead_lettered_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULLIF($12, ''), NULLIF($13, ''), $14, $15)
		ON CONFLICT (delivery_key) WHERE delivery_key IS NOT NULL DO UPDATE SET
			outbox_event_id = EXCLUDED.outbox_event_id,
			status = EXCLUDED.status, attempts = EXCLUDED.attempts,
			response_status = EXCLUDED.response_status, error_message = EXCLUDED.error_message,
			next_attempt_at = EXCLUDED.next_attempt_at, dead_lettered_at = EXCLUDED.dead_lettered_at,
			updated_at = EXCLUDED.updated_at`
	_, err := s.pool.Exec(ctx, query,
		delivery.ID, delivery.EndpointID, delivery.EventID, delivery.EventType, delivery.TargetURL,
		delivery.Status, delivery.Attempts, delivery.ResponseStatus, delivery.ErrorMessage,
		delivery.CreatedAt, delivery.UpdatedAt, delivery.OutboxEventID, delivery.DeliveryKey,
		delivery.NextAttemptAt, delivery.DeadLetteredAt,
	)
	return err
}

func (s *PgStore) ListDeliveries(ctx context.Context, endpointID string, limit int) ([]*Delivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT id, endpoint_id, event_id, event_type, target_url, status,
			attempts, response_status, error_message, created_at, updated_at,
			COALESCE(outbox_event_id, ''), COALESCE(delivery_key, ''), next_attempt_at, dead_lettered_at
		FROM webhook_deliveries
		WHERE ($1 = '' OR endpoint_id = $1)
		ORDER BY created_at DESC
		LIMIT $2`, endpointID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*Delivery, 0)
	for rows.Next() {
		delivery := &Delivery{}
		if err := rows.Scan(&delivery.ID, &delivery.EndpointID, &delivery.EventID, &delivery.EventType,
			&delivery.TargetURL, &delivery.Status, &delivery.Attempts, &delivery.ResponseStatus,
			&delivery.ErrorMessage, &delivery.CreatedAt, &delivery.UpdatedAt, &delivery.OutboxEventID,
			&delivery.DeliveryKey, &delivery.NextAttemptAt, &delivery.DeadLetteredAt); err != nil {
			return nil, err
		}
		items = append(items, delivery)
	}
	return items, rows.Err()
}

func (s *PgStore) Summary(ctx context.Context) (*Summary, error) {
	summary := &Summary{}
	err := s.pool.QueryRow(ctx, `SELECT
			(SELECT COUNT(*) FROM webhook_endpoints WHERE deleted_at IS NULL),
			(SELECT COUNT(*) FROM webhook_endpoints WHERE deleted_at IS NULL AND enabled = TRUE),
			(SELECT COUNT(*) FROM webhook_deliveries),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'success'),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'failed'),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'failed' AND created_at > NOW() - INTERVAL '24 hours')`).Scan(
		&summary.EndpointTotal, &summary.EnabledTotal, &summary.DeliveryTotal,
		&summary.SuccessTotal, &summary.FailedTotal, &summary.LastFailureTotal,
	)
	return summary, err
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListEndpoints(c *gin.Context) {
	items, err := h.svc.ListEndpoints(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 70001, err.Error())
		return
	}
	response.Success(c, gin.H{"items": redactEndpoints(items), "total": len(items)})
}

func (h *Handler) CreateEndpoint(c *gin.Context) {
	var req struct {
		Name               string   `json:"name" binding:"required"`
		URL                string   `json:"url" binding:"required"`
		Secret             string   `json:"secret"`
		Events             []string `json:"events"`
		Enabled            *bool    `json:"enabled"`
		MaxRetries         int      `json:"max_retries"`
		TimeoutMS          int      `json:"timeout_ms"`
		MaxConcurrent      int      `json:"max_concurrent"`
		RateLimitPerMinute int      `json:"rate_limit_per_minute"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 70002, "invalid request: "+err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	endpoint := &Endpoint{
		Name:               req.Name,
		URL:                req.URL,
		Secret:             req.Secret,
		Events:             req.Events,
		Enabled:            enabled,
		MaxRetries:         req.MaxRetries,
		TimeoutMS:          req.TimeoutMS,
		MaxConcurrent:      req.MaxConcurrent,
		RateLimitPerMinute: req.RateLimitPerMinute,
		CreatedBy:          currentUserID(c),
	}
	if err := h.svc.CreateEndpoint(c.Request.Context(), endpoint); err != nil {
		response.Error(c, http.StatusBadRequest, 70002, err.Error())
		return
	}
	endpoint.Secret = ""
	response.Created(c, endpoint)
}

func (h *Handler) EnableEndpoint(c *gin.Context) {
	h.setEnabled(c, true)
}

func (h *Handler) DisableEndpoint(c *gin.Context) {
	h.setEnabled(c, false)
}

func (h *Handler) setEnabled(c *gin.Context, enabled bool) {
	if err := h.svc.SetEnabled(c.Request.Context(), c.Param("id"), enabled); err != nil {
		writeWebhookError(c, err)
		return
	}
	response.Success(c, gin.H{"enabled": enabled})
}

func (h *Handler) TestEndpoint(c *gin.Context) {
	delivery, err := h.svc.TestEndpoint(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeWebhookError(c, err)
		return
	}
	response.Success(c, delivery)
}

func (h *Handler) ListDeliveries(c *gin.Context) {
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := fmt.Sscanf(raw, "%d", &limit); err != nil || parsed != 1 {
			limit = 100
		}
	}
	items, err := h.svc.ListDeliveries(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		writeWebhookError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) Summary(c *gin.Context) {
	summary, err := h.svc.Summary(c.Request.Context())
	if err != nil {
		writeWebhookError(c, err)
		return
	}
	response.Success(c, summary)
}

func currentUserID(c *gin.Context) string {
	value, ok := c.Get("user_id")
	if !ok {
		return ""
	}
	userID, _ := value.(string)
	return userID
}

func writeWebhookError(c *gin.Context, err error) {
	if errors.Is(err, ErrEndpointNotFound) {
		response.Error(c, http.StatusNotFound, 70004, err.Error())
		return
	}
	response.Error(c, http.StatusInternalServerError, 70001, err.Error())
}

func redactEndpoints(items []*Endpoint) []*Endpoint {
	result := make([]*Endpoint, 0, len(items))
	for _, endpoint := range items {
		clone := cloneEndpoint(endpoint)
		clone.Secret = ""
		result = append(result, clone)
	}
	return result
}

func cloneEndpoint(endpoint *Endpoint) *Endpoint {
	if endpoint == nil {
		return nil
	}
	clone := *endpoint
	clone.Events = append([]string(nil), endpoint.Events...)
	return &clone
}

func cloneDelivery(delivery *Delivery) *Delivery {
	if delivery == nil {
		return nil
	}
	clone := *delivery
	return &clone
}
