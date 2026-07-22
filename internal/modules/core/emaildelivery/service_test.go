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
	"github.com/campusos/CampusOS/pkg/observability"
)

type stubDispatchReader struct {
	dispatch identityport.ChallengeDispatch
	err      error
}

type stubAccountReader struct {
	account identityport.EmailAccount
	err     error
}

func (s stubAccountReader) GetEmailAccount(context.Context, string) (identityport.EmailAccount, error) {
	return s.account, s.err
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

type transientSender struct{ calls int }

func (s *transientSender) Provider() string { return "smtp" }
func (s *transientSender) Send(context.Context, Message) error {
	s.calls++
	if s.calls == 1 {
		return errors.New("temporary SMTP failure for recipient@example.test")
	}
	return nil
}
func (s *transientSender) Health(context.Context) ProviderHealth {
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
	service, err := NewService(dispatch, stubAccountReader{account: identityport.EmailAccount{IdentifierNormalized: "recipient@example.test", VerificationState: "verified"}}, reliable, sender)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestDeliverMFALocalRecoverySendsRedactedSecurityNotice(t *testing.T) {
	fake := NewFakeSender()
	service := testService(t, stubDispatchReader{}, fake)
	event, err := reliability.NewEvent(MFALocalRecoveryEvent, "user", "73001", map[string]string{"action": "mfa_reset"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeliverMFALocalRecovery(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if fake.DeliveryCount() != 1 || service.Status().Delivered != 1 {
		t.Fatalf("security notice was not delivered: %#v", service.Status())
	}
	if rendered := service.Status().LastError; strings.Contains(rendered, "recipient@example.test") || strings.Contains(rendered, "mfa_reset") {
		t.Fatalf("security delivery status leaked sensitive material: %#v", service.Status())
	}
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

func TestDeliveryMetricsUseBoundedLabelsAndDoNotLeakMessageData(t *testing.T) {
	collector := observability.NewCollector()
	service := testService(t, stubDispatchReader{dispatch: identityport.ChallengeDispatch{
		ChallengeID: "challenge-1", Purpose: "registration", Email: "recipient@example.test", Code: "654321", ExpiresAt: time.Now().Add(time.Minute),
	}}, NewFakeSender())
	service.SetMeter(collector)
	if err := service.DeliverChallenge(t.Context(), testEvent(t, map[string]string{"challenge_id": "challenge-1"})); err != nil {
		t.Fatal(err)
	}
	output := collector.PrometheusText()
	for _, expected := range []string{
		`campusos_email_delivery_total{provider="fake",result="delivered"} 1`,
		`campusos_email_delivery_duration_seconds_count{provider="fake",result="delivered"} 1`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("delivery metrics missing %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "recipient@example.test") || strings.Contains(output, "654321") {
		t.Fatalf("delivery metrics leaked transient message data:\n%s", output)
	}
}

func TestDeliveryMetricsShowTransientFailureAndRecovery(t *testing.T) {
	collector := observability.NewCollector()
	sender := &transientSender{}
	service := testService(t, stubDispatchReader{dispatch: identityport.ChallengeDispatch{
		ChallengeID: "challenge-1", Purpose: "registration", Email: "recipient@example.test", Code: "654321", ExpiresAt: time.Now().Add(time.Minute),
	}}, sender)
	service.SetMeter(collector)
	event := testEvent(t, map[string]string{"challenge_id": "challenge-1"})
	if err := service.DeliverChallenge(t.Context(), event); err == nil {
		t.Fatal("first SMTP delivery should fail transiently")
	}
	if err := service.DeliverChallenge(t.Context(), event); err != nil {
		t.Fatalf("SMTP recovery delivery: %v", err)
	}
	output := collector.PrometheusText()
	for _, expected := range []string{
		`campusos_email_delivery_total{provider="smtp",result="unavailable"} 1`,
		`campusos_email_delivery_total{provider="smtp",result="delivered"} 1`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("transient delivery metric missing %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "recipient@example.test") || strings.Contains(output, "654321") {
		t.Fatalf("transient delivery metrics leaked message data:\n%s", output)
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
	if err := app.Provide("identity.account-reader", identityport.AccountReader(stubAccountReader{account: identityport.EmailAccount{IdentifierNormalized: "recipient@example.test", VerificationState: "verified"}})); err != nil {
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
