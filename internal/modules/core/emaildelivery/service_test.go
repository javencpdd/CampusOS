package emaildelivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

type stubDispatchReader struct {
	dispatch identityport.ChallengeDispatch
	err      error
}

func (s stubDispatchReader) Dispatch(context.Context, string) (identityport.ChallengeDispatch, error) {
	return s.dispatch, s.err
}

type failingSender struct{}

func (failingSender) Provider() string { return "smtp" }
func (failingSender) Send(context.Context, Message) error {
	return errors.New("smtp.example.test rejected recipient@example.test with code 654321")
}
func (failingSender) Health(context.Context) ProviderHealth {
	return ProviderHealth{Provider: "smtp", State: "healthy"}
}

func testEvent(t *testing.T, payload any) reliability.Event {
	t.Helper()
	event, err := reliability.NewEvent(ChallengeRequestedEvent, "identity_email_challenge", "challenge-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func testService(t *testing.T, dispatch identityport.ChallengeDispatchReader, sender Sender) *Service {
	t.Helper()
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	service, err := NewService(dispatch, reliable, sender)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestDeliverChallengeUsesEphemeralDispatchAndSafeStatus(t *testing.T) {
	fake := NewFakeSender()
	service := testService(t, stubDispatchReader{dispatch: identityport.ChallengeDispatch{
		ChallengeID: "challenge-1", Purpose: "registration", Email: "recipient@example.test", Code: "654321", ExpiresAt: time.Now().Add(time.Minute),
	}}, fake)

	if err := service.DeliverChallenge(context.Background(), testEvent(t, map[string]string{"challenge_id": "challenge-1"})); err != nil {
		t.Fatal(err)
	}
	if fake.DeliveryCount() != 1 {
		t.Fatalf("fake delivery count = %d, want 1", fake.DeliveryCount())
	}
	status := service.Status()
	if status.Delivered != 1 || status.State != "healthy" {
		t.Fatalf("unexpected delivery status: %#v", status)
	}
	serialized := status.Module + status.Provider + status.State + status.LastError
	if strings.Contains(serialized, "recipient@example.test") || strings.Contains(serialized, "654321") {
		t.Fatalf("status leaked transient delivery material: %#v", status)
	}
}

func TestDeliverChallengeSkipsStaleReplay(t *testing.T) {
	fake := NewFakeSender()
	service := testService(t, stubDispatchReader{err: identityport.ErrChallengeNotDeliverable}, fake)
	if err := service.DeliverChallenge(context.Background(), testEvent(t, map[string]string{"challenge_id": "expired"})); err != nil {
		t.Fatal(err)
	}
	if fake.DeliveryCount() != 0 || service.Status().Skipped != 1 {
		t.Fatalf("stale replay should be acknowledged without delivery: %#v", service.Status())
	}
}

func TestDeliverChallengeProviderFailureIsRedactedAndRetryable(t *testing.T) {
	service := testService(t, stubDispatchReader{dispatch: identityport.ChallengeDispatch{
		ChallengeID: "challenge-1", Purpose: "registration", Email: "recipient@example.test", Code: "654321", ExpiresAt: time.Now().Add(time.Minute),
	}}, failingSender{})
	err := service.DeliverChallenge(context.Background(), testEvent(t, map[string]string{"challenge_id": "challenge-1"}))
	if err == nil || !strings.Contains(err.Error(), ErrDeliveryUnavailable.Error()) {
		t.Fatalf("delivery error = %v, want generic retryable provider error", err)
	}
	if strings.Contains(err.Error(), "recipient@example.test") || strings.Contains(err.Error(), "654321") {
		t.Fatalf("delivery error leaked sensitive data: %v", err)
	}
	status := service.Status()
	if status.State != "degraded" || status.LastError != "email provider delivery failed" {
		t.Fatalf("unexpected redacted status: %#v", status)
	}
}

func TestDeliverChallengeRejectsUnexpectedPayloadFields(t *testing.T) {
	fake := NewFakeSender()
	service := testService(t, stubDispatchReader{}, fake)
	err := service.DeliverChallenge(context.Background(), testEvent(t, map[string]string{"challenge_id": "challenge-1", "code": "654321"}))
	if err == nil || !strings.Contains(err.Error(), ErrInvalidDeliveryEvent.Error()) {
		t.Fatalf("unexpected payload error: %v", err)
	}
	if fake.DeliveryCount() != 0 {
		t.Fatal("invalid Outbox payload reached the email provider")
	}
}

func TestModuleRegistersReliableConsumer(t *testing.T) {
	app := platformmodule.NewAppContext()
	if err := reliability.BindMemoryAdapter(app); err != nil {
		t.Fatal(err)
	}
	if err := app.Provide("platform.event-bus", eventbus.NewMemoryEventBus()); err != nil {
		t.Fatal(err)
	}
	reliabilityModule := reliability.NewModule()
	if err := reliabilityModule.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := reliabilityModule.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer reliabilityModule.Stop(context.Background())
	if err := app.Provide("identity.challenge-dispatch-reader", identityport.ChallengeDispatchReader(stubDispatchReader{dispatch: identityport.ChallengeDispatch{
		ChallengeID: "challenge-1", Purpose: "registration", Email: "recipient@example.test", Code: "654321", ExpiresAt: time.Now().Add(time.Minute),
	}})); err != nil {
		t.Fatal(err)
	}
	module := NewModule(Config{Environment: "test", Provider: "fake"})
	if err := module.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := module.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer module.Stop(context.Background())
	event := testEvent(t, map[string]string{"challenge_id": "challenge-1"})
	if _, err := reliabilityModule.Service().Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if _, err := reliabilityModule.Service().ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if module.Service().Status().Delivered != 1 {
		t.Fatalf("consumer did not deliver: %#v", module.Service().Status())
	}
}

func TestNewSenderRejectsFakeOutsideLocalProfiles(t *testing.T) {
	if _, err := NewSender(Config{Environment: "production", Provider: "fake"}); err == nil {
		t.Fatal("production fake sender was accepted")
	}
}
