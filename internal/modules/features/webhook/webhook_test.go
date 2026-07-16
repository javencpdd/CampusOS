package webhook

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

func TestValidateURLRejectsPrivateDestinations(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/hook",
		"http://[::1]/hook",
		"http://localhost/hook",
		"file:///tmp/hook",
	} {
		if err := validateURL(raw); err == nil {
			t.Fatalf("expected private/invalid URL %q to be rejected", raw)
		}
	}
	if err := validateURL("https://hooks.example.com/campusos"); err != nil {
		t.Fatalf("expected public URL to pass syntax validation: %v", err)
	}
}

func TestDurableWebhookQueueUsesIdempotencyKey(t *testing.T) {
	store := NewMemoryStore()
	endpoint := &Endpoint{ID: "11", Name: "example", URL: "https://hooks.example.com/campusos", Enabled: true, MaxRetries: 2}
	if err := store.CreateEndpoint(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	service := NewService(store, nil, reliable)
	event := eventbus.NewEvent(eventbus.EventThreadUpdated, "test", "thread.1", map[string]string{"id": "1"})
	if err := service.DeliverEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := service.DeliverEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	items, err := reliable.List(context.Background(), reliability.EventFilter{Type: "webhook.delivery"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one idempotent queued delivery, got %d", len(items))
	}
	if items[0].MaxAttempts != 3 {
		t.Fatalf("max attempts = %d, want 3", items[0].MaxAttempts)
	}
}

func TestWebhookFanoutIsAcknowledgedBeforeLegacyBusFallback(t *testing.T) {
	store := NewMemoryStore()
	endpoint := &Endpoint{ID: "12", Name: "fanout", URL: "https://hooks.example.com/campusos", Enabled: true, MaxRetries: 2}
	if err := store.CreateEndpoint(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	service := NewService(store, nil, reliable)
	if err := service.Register(nil); err != nil {
		t.Fatal(err)
	}
	root, err := reliability.NewEvent(eventbus.EventThreadUpdated, "thread", "1", map[string]string{"id": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reliable.Enqueue(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := reliable.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	queued, err := reliable.List(context.Background(), reliability.EventFilter{Type: "webhook.delivery"})
	if err != nil || len(queued) != 1 {
		t.Fatalf("expected one durable webhook child event, queued=%+v err=%v", queued, err)
	}
}

func TestMemoryDeliverySaveUpsertsDeliveryKey(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	if err := store.SaveDelivery(context.Background(), &Delivery{ID: "1", EndpointID: "10", DeliveryKey: "10:event", Status: "retry", Attempts: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDelivery(context.Background(), &Delivery{ID: "2", EndpointID: "10", DeliveryKey: "10:event", Status: "success", Attempts: 2, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListDeliveries(context.Background(), "10", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != "success" || items[0].Attempts != 2 {
		t.Fatalf("delivery upsert failed: %+v", items)
	}
}

func TestPostSignedJSONUsesVersionedReplayHeaders(t *testing.T) {
	body := []byte(`{"ok":true}`)
	endpoint := &Endpoint{URL: "https://hooks.example.com/campusos", TimeoutMS: 1000, Secret: "secret"}
	request, err := newSignedRequest(context.Background(), endpoint, "event-1", body)
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("X-CampusOS-Signature-Version"); got != webhookSignatureVersion {
		t.Errorf("signature version = %q", got)
	}
	if got := request.Header.Get("X-CampusOS-Event-ID"); got != "event-1" {
		t.Errorf("event id = %q", got)
	}
	timestamp := request.Header.Get("X-CampusOS-Timestamp")
	if timestamp == "" {
		t.Fatal("missing signature timestamp")
	}
	if got, want := request.Header.Get("X-CampusOS-Signature"), SignPayloadV1("secret", timestamp, body); got != want {
		t.Errorf("signature = %q, want %q", got, want)
	}
}

func TestPostSignedJSONBoundsResponseBody(t *testing.T) {
	err := readWebhookResponseBody(bytes.NewReader(make([]byte, maxWebhookResponseBody+1)))
	if !errors.Is(err, errWebhookResponseTooLarge) {
		t.Fatalf("expected bounded response error, got %v", err)
	}
}

func TestWebhookTransportBoundsResponseHeaders(t *testing.T) {
	transport := newSafeWebhookTransport(EgressPolicy{}, time.Second)
	if transport.MaxResponseHeaderBytes != maxWebhookResponseHeader {
		t.Fatalf("response header limit = %d, want %d", transport.MaxResponseHeaderBytes, maxWebhookResponseHeader)
	}
	if transport.Proxy != nil {
		t.Fatal("webhook transport must not inherit proxy settings")
	}
}

func TestWebhookRetryAfterAndEndpointRateLimit(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store, nil)
	endpoint := &Endpoint{ID: "rate", URL: "https://hooks.example.com/campusos", MaxConcurrent: 1, RateLimitPerMinute: 1}
	release, _, limited := service.acquireEndpoint(endpoint, time.Now().UTC())
	if limited {
		t.Fatal("first endpoint admission was unexpectedly limited")
	}
	release()
	_, wait, limited := service.acquireEndpoint(endpoint, time.Now().UTC())
	if !limited || wait <= 0 {
		t.Fatalf("expected endpoint rate limiter to defer the next delivery: limited=%v wait=%s", limited, wait)
	}
	if retryAfter := parseRetryAfter("3", time.Now().UTC()); retryAfter != 3*time.Second {
		t.Fatalf("retry-after duration = %s", retryAfter)
	}
}
