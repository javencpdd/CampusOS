package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestChallengeLifecycleUsesOpaqueOutboxAndOneTimeTicket(t *testing.T) {
	service, store, reliable, _ := newTestChallengeService(t)
	ctx := context.Background()
	receipt, err := service.Request(ctx, domain.ChallengeRequest{
		Purpose:  domain.ChallengePurposeRegistration,
		Email:    " Student@Example.Test ",
		ClientIP: "203.0.113.10",
	})
	if err != nil {
		t.Fatalf("request challenge: %v", err)
	}
	if receipt.Purpose != domain.ChallengePurposeRegistration || receipt.PublicID == "" {
		t.Fatalf("unexpected challenge receipt: %#v", receipt)
	}
	items, err := reliable.List(ctx, reliability.EventFilter{Type: "identity.email.challenge.requested.v1", Limit: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("queued events=%#v err=%v", items, err)
	}
	payload := string(items[0].Payload)
	if strings.Contains(payload, "student@example.test") || strings.Contains(payload, "ticket") || strings.Contains(payload, "code") {
		t.Fatalf("challenge event leaked sensitive data: %s", payload)
	}

	challenge, err := store.GetChallenge(ctx, receipt.PublicID)
	if err != nil {
		t.Fatalf("load challenge: %v", err)
	}
	dispatch, err := service.Dispatch(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("dispatch projection: %v", err)
	}
	if dispatch.Email != "student@example.test" || len(dispatch.Code) != 6 {
		t.Fatalf("unexpected dispatch: %#v", dispatch)
	}
	ticket, err := service.Verify(ctx, domain.ChallengeVerificationRequest{
		PublicID: receipt.PublicID,
		Purpose:  domain.ChallengePurposeRegistration,
		Code:     dispatch.Code,
	})
	if err != nil {
		t.Fatalf("verify challenge: %v", err)
	}
	if ticket.Ticket == "" || ticket.ExpiresAt.IsZero() {
		t.Fatalf("unexpected ticket: %#v", ticket)
	}
	consumed, err := service.ConsumeTicket(ctx, domain.ChallengeTicketConsumption{
		PublicID: receipt.PublicID,
		Purpose:  domain.ChallengePurposeRegistration,
		Ticket:   ticket.Ticket,
		Email:    "student@example.test",
	})
	if err != nil || consumed.ConsumedAt == nil {
		t.Fatalf("consume ticket: challenge=%#v err=%v", consumed, err)
	}
	if _, err := service.ConsumeTicket(ctx, domain.ChallengeTicketConsumption{
		PublicID: receipt.PublicID,
		Purpose:  domain.ChallengePurposeRegistration,
		Ticket:   ticket.Ticket,
		Email:    "student@example.test",
	}); !errors.Is(err, ErrChallengeTicket) {
		t.Fatalf("second consume error=%v, want one-time ticket rejection", err)
	}
}

func TestChallengePurposeAttemptsAndRateLimitsAreEnforced(t *testing.T) {
	service, store, _, advance := newTestChallengeService(t)
	ctx := context.Background()
	receipt, err := service.Request(ctx, domain.ChallengeRequest{
		Purpose:  domain.ChallengePurposeRegistration,
		Email:    "learner@example.test",
		ClientIP: "203.0.113.11",
	})
	if err != nil {
		t.Fatalf("request challenge: %v", err)
	}
	if _, err := service.Request(ctx, domain.ChallengeRequest{
		Purpose:  domain.ChallengePurposeRegistration,
		Email:    "learner@example.test",
		ClientIP: "203.0.113.11",
	}); !errors.Is(err, ErrChallengeRateLimited) {
		t.Fatalf("immediate resend error=%v, want rate limit", err)
	}

	challenge, err := store.GetChallenge(ctx, receipt.PublicID)
	if err != nil {
		t.Fatalf("load challenge: %v", err)
	}
	dispatch, err := service.Dispatch(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, err := service.Verify(ctx, domain.ChallengeVerificationRequest{
		PublicID: receipt.PublicID,
		Purpose:  domain.ChallengePurposePasswordReset,
		Code:     dispatch.Code,
	}); !errors.Is(err, ErrChallengeInvalid) {
		t.Fatalf("cross-purpose verification error=%v, want invalid", err)
	}
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := service.Verify(ctx, domain.ChallengeVerificationRequest{
			PublicID: receipt.PublicID,
			Purpose:  domain.ChallengePurposeRegistration,
			Code:     "000000",
		}); !errors.Is(err, ErrChallengeInvalid) {
			t.Fatalf("wrong code attempt %d error=%v", attempt, err)
		}
	}
	if _, err := service.Verify(ctx, domain.ChallengeVerificationRequest{
		PublicID: receipt.PublicID,
		Purpose:  domain.ChallengePurposeRegistration,
		Code:     dispatch.Code,
	}); !errors.Is(err, ErrChallengeInvalid) {
		t.Fatalf("exhausted challenge error=%v, want invalid", err)
	}

	advance(time.Minute)
	for index := 0; index < 4; index++ {
		if _, err := service.Request(ctx, domain.ChallengeRequest{
			Purpose:  domain.ChallengePurposeRegistration,
			Email:    "learner@example.test",
			ClientIP: "203.0.113.11",
		}); err != nil {
			t.Fatalf("daily request %d: %v", index, err)
		}
		advance(time.Minute)
	}
	if _, err := service.Request(ctx, domain.ChallengeRequest{
		Purpose:  domain.ChallengePurposeRegistration,
		Email:    "learner@example.test",
		ClientIP: "203.0.113.11",
	}); !errors.Is(err, ErrChallengeRateLimited) {
		t.Fatalf("daily limit error=%v, want rate limited", err)
	}
}

func TestChallengeTicketIsBoundToNormalizedEmail(t *testing.T) {
	service, store, _, _ := newTestChallengeService(t)
	ctx := context.Background()
	receipt, err := service.Request(ctx, domain.ChallengeRequest{
		Purpose:  domain.ChallengePurposeEmailBinding,
		Email:    "binding@example.test",
		ClientIP: "203.0.113.12",
	})
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := store.GetChallenge(ctx, receipt.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := service.Dispatch(ctx, challenge.ID)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := service.Verify(ctx, domain.ChallengeVerificationRequest{PublicID: receipt.PublicID, Purpose: receipt.Purpose, Code: dispatch.Code})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeTicket(ctx, domain.ChallengeTicketConsumption{
		PublicID: receipt.PublicID, Purpose: receipt.Purpose, Ticket: ticket.Ticket, Email: "other@example.test",
	}); !errors.Is(err, ErrChallengeTicket) {
		t.Fatalf("wrong-email ticket use error=%v", err)
	}
	if _, err := service.ConsumeTicket(ctx, domain.ChallengeTicketConsumption{
		PublicID: receipt.PublicID, Purpose: receipt.Purpose, Ticket: ticket.Ticket, Email: " Binding@Example.Test ",
	}); err != nil {
		t.Fatalf("normalized email should consume ticket: %v", err)
	}
}

func newTestChallengeService(t *testing.T) (*ChallengeService, *repository.MemoryChallengeRepository, *reliability.Service, func(time.Duration)) {
	t.Helper()
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store := repository.NewMemoryChallengeRepository()
	service, err := NewChallengeService(store, ChallengeConfig{
		ActiveKeyID:  "test-v1",
		HMACKeys:     map[string]string{"test-v1": "test-challenge-hmac-secret"},
		IPHashSecret: "test-challenge-ip-hash-secret",
		Clock:        clock,
	})
	if err != nil {
		t.Fatalf("new challenge service: %v", err)
	}
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	service.SetReliability(reliable)
	advance := func(duration time.Duration) { now = now.Add(duration) }
	return service, store, reliable, advance
}
