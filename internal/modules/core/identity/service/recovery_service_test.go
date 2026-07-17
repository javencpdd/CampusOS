package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

func TestPasswordResetRevokesSessionsAndConsumesTicketAtomically(t *testing.T) {
	service, users, challenges, challengeStore, sessions, jwtManager, reliable := newRecoveryServiceForTest(t)
	ctx := context.Background()
	user, err := users.GetByID(ctx, "33001")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.Issue(ctx, user, SessionMetadata{DeviceName: "Before reset", ClientIP: "203.0.113.20"})
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	claims, err := jwtManager.VerifyAccessToken(issued.AccessToken)
	if err != nil {
		t.Fatal(err)
	}

	requested, err := service.RequestPasswordReset(ctx, "member@example.test", "203.0.113.20")
	if err != nil || !requested.Accepted || requested.ChallengeID == "" {
		t.Fatalf("password reset request=%#v err=%v", requested, err)
	}
	challenge, err := challengeStore.GetChallenge(ctx, requested.ChallengeID)
	if err != nil {
		t.Fatalf("load reset challenge: %v", err)
	}
	dispatch, err := challenges.Dispatch(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("dispatch reset code: %v", err)
	}
	ticket, err := service.VerifyPasswordReset(ctx, domain.PasswordResetVerificationRequest{
		ChallengeID: requested.ChallengeID, Code: dispatch.Code,
	})
	if err != nil {
		t.Fatalf("verify reset code: %v", err)
	}
	completion := domain.PasswordResetCompletionRequest{
		Email: "Member@Example.Test", ChallengeID: requested.ChallengeID, Ticket: ticket.Ticket, Password: "new-recovery-password",
	}
	if err := service.CompletePasswordReset(ctx, completion); err != nil {
		t.Fatalf("complete reset: %v", err)
	}
	if _, credential, err := users.GetCredentialByEmail(ctx, "member@example.test"); err != nil || !auth.CheckPassword("new-recovery-password", credential) || auth.CheckPassword("original-password", credential) {
		t.Fatalf("credential was not safely rotated: credential match=%v err=%v", auth.CheckPassword("new-recovery-password", credential), err)
	}
	if err := sessions.VerifyAccess(ctx, claims); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("pre-reset access token remained valid: %v", err)
	}
	if err := service.CompletePasswordReset(ctx, completion); !errors.Is(err, ErrRecoveryInvalid) {
		t.Fatalf("reused reset ticket error=%v, want invalid", err)
	}

	unknown, err := service.RequestPasswordReset(ctx, "unknown@example.test", "203.0.113.20")
	if err != nil || !unknown.Accepted || len(unknown.ChallengeID) < 16 {
		t.Fatalf("enumeration-safe response=%#v err=%v", unknown, err)
	}
	events, err := reliable.List(ctx, reliability.EventFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		payload := string(event.Payload)
		if strings.Contains(payload, "member@example.test") || strings.Contains(payload, "new-recovery-password") || strings.Contains(payload, ticket.Ticket) {
			t.Fatalf("recovery outbox payload leaked sensitive value: %s", payload)
		}
	}
}

func TestEmailBindingUpdatesAccountAndForcesRelogin(t *testing.T) {
	service, users, challenges, challengeStore, sessions, jwtManager, _ := newRecoveryServiceForTest(t)
	ctx := context.Background()
	user, err := users.GetByID(ctx, "33001")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.Issue(ctx, user, SessionMetadata{DeviceName: "Binding browser"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwtManager.VerifyAccessToken(issued.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.RequestEmailBinding(ctx, user.ID, domain.EmailBindingChallengeRequest{Email: "new.member@example.test"}, "203.0.113.21")
	if err != nil {
		t.Fatalf("request email binding: %v", err)
	}
	challenge, err := challengeStore.GetChallenge(ctx, receipt.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := challenges.Dispatch(ctx, challenge.ID)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := service.VerifyEmailBinding(ctx, domain.EmailBindingVerificationRequest{ChallengeID: receipt.PublicID, Code: dispatch.Code})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CompleteEmailBinding(ctx, user.ID, domain.EmailBindingCompletionRequest{
		Email: "New.Member@Example.Test", ChallengeID: receipt.PublicID, Ticket: ticket.Ticket,
	}); err != nil {
		t.Fatalf("complete email binding: %v", err)
	}
	updated, err := users.GetByID(ctx, user.ID)
	if err != nil || updated.Email != "new.member@example.test" {
		t.Fatalf("user email after binding=%#v err=%v", updated, err)
	}
	account, err := users.GetEmailAccount(ctx, user.ID)
	if err != nil || account.IdentifierNormalized != "new.member@example.test" || account.VerificationState != domain.VerificationStateVerified {
		t.Fatalf("account after binding=%#v err=%v", account, err)
	}
	if err := sessions.VerifyAccess(ctx, claims); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("pre-binding access token remained valid: %v", err)
	}
	if _, err := users.GetByEmail(ctx, "member@example.test"); !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("old email remained an account lookup: %v", err)
	}
}

func TestAdminAssistedRecoveryRequiresLegacyAccountAndNeverLeaksProof(t *testing.T) {
	ctx := context.Background()
	legacy := newLegacyRecoveryUserRepository(t)
	challengeStore := repository.NewMemoryChallengeRepository()
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	challenges, err := NewChallengeService(challengeStore, ChallengeConfig{
		ActiveKeyID: "recovery-test-v1", HMACKeys: map[string]string{"recovery-test-v1": "recovery-test-secret"},
		IPHashSecret: "recovery-test-ip-secret", Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionStore := repository.NewMemorySessionRepository()
	jwtManager := auth.NewJWTManager(auth.JWTConfig{Secret: "recovery-legacy-jwt", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, Issuer: "test"})
	sessions, err := NewSessionService(sessionStore, legacy, jwtManager, SessionConfig{IPHashSecret: "recovery-test-ip-secret", Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	recovery, err := NewRecoveryService(legacy, sessionStore, challenges, repository.NewMemoryRecoveryCaseRepository(), RecoveryConfig{PasswordHashEnabled: true, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetReliability(reliable)
	recovery.SetReliability(reliable)
	user, err := legacy.GetByID(ctx, "33002")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.Issue(ctx, user, SessionMetadata{DeviceName: "Legacy browser"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwtManager.VerifyAccessToken(issued.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	caseView, err := recovery.CreateAdminRecoveryCase(ctx, "33099", domain.AdminRecoveryCaseCreateRequest{
		UserID: user.ID, Email: "recovered@example.test", ProofReference: "offline-id-check-20260717",
	}, "203.0.113.22")
	if err != nil {
		t.Fatalf("create recovery case: %v", err)
	}
	if caseView.TargetEmailMasked == "recovered@example.test" || caseView.ID == "" {
		t.Fatalf("case response exposed target email or no id: %#v", caseView)
	}
	events, err := reliable.List(ctx, reliability.EventFilter{Type: "identity.email.challenge.requested.v1", Limit: 5})
	if err != nil || len(events) != 1 {
		t.Fatalf("recovery delivery event=%#v err=%v", events, err)
	}
	if strings.Contains(string(events[0].Payload), "recovered@example.test") || strings.Contains(string(events[0].Payload), "offline-id-check") {
		t.Fatalf("recovery event leaked email/proof: %s", events[0].Payload)
	}
	// The outbox only contains an internal ID. Obtain the code through the
	// same Core dispatch boundary used by email delivery.
	// Memory inspection is intentionally test-only; production delivery uses
	// the opaque event ID through ChallengeService.Dispatch.
	cases, err := recovery.ListAdminRecoveryCases(ctx, 10)
	if err != nil || len(cases) != 1 {
		t.Fatalf("list recovery cases=%#v err=%v", cases, err)
	}
	// Find the Challenge by its internal delivery event payload without turning
	// the application response into a credential-bearing API.
	var payload struct {
		ChallengeID string `json:"challenge_id"`
	}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil || payload.ChallengeID == "" {
		t.Fatalf("decode test-only outbox payload: %v %#v", err, payload)
	}
	dispatch, err := challenges.Dispatch(ctx, payload.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := challengeStore.GetChallengeByID(ctx, payload.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := recovery.VerifyPasswordReset(ctx, domain.PasswordResetVerificationRequest{ChallengeID: challenge.PublicID, Code: dispatch.Code})
	if err != nil {
		t.Fatal(err)
	}
	if err := recovery.CompleteAdminRecoveryCase(ctx, domain.RecoveryCaseCompletionRequest{
		ChallengeID: challenge.PublicID, Ticket: ticket.Ticket, Password: "legacy-new-password",
	}); err != nil {
		t.Fatalf("complete admin recovery: %v", err)
	}
	if _, credential, err := legacy.GetCredentialByEmail(ctx, "recovered@example.test"); err != nil || !auth.CheckPassword("legacy-new-password", credential) {
		t.Fatalf("legacy credential transition failed: %v", err)
	}
	if err := sessions.VerifyAccess(ctx, claims); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("legacy session survived recovery: %v", err)
	}
}

func newRecoveryServiceForTest(t *testing.T) (*RecoveryService, *repository.MemoryUserRepository, *ChallengeService, *repository.MemoryChallengeRepository, *SessionService, *auth.JWTManager, *reliability.Service) {
	t.Helper()
	ctx := context.Background()
	users := repository.NewMemoryUserRepository()
	user := &domain.User{
		ID: "33001", Username: "recovery_member", Nickname: "Recovery Member", Email: "member@example.test",
		Status: domain.UserStatusActive, AuthVersion: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	credential, err := auth.HashPassword("original-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.CreateVerifiedAccount(ctx, user.ID, user.Email, credential); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 17, 11, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	challengeStore := repository.NewMemoryChallengeRepository()
	challenges, err := NewChallengeService(challengeStore, ChallengeConfig{
		ActiveKeyID: "recovery-test-v1", HMACKeys: map[string]string{"recovery-test-v1": "recovery-test-secret"},
		IPHashSecret: "recovery-test-ip-secret", Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionStore := repository.NewMemorySessionRepository()
	jwtManager := auth.NewJWTManager(auth.JWTConfig{Secret: "recovery-jwt-secret", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, Issuer: "test"})
	sessions, err := NewSessionService(sessionStore, users, jwtManager, SessionConfig{IPHashSecret: "recovery-test-ip-secret", Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	recovery, err := NewRecoveryService(users, sessionStore, challenges, repository.NewMemoryRecoveryCaseRepository(), RecoveryConfig{PasswordHashEnabled: true, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetReliability(reliable)
	recovery.SetReliability(reliable)
	return recovery, users, challenges, challengeStore, sessions, jwtManager, reliable
}

// legacyRecoveryUserRepository models the v12 migration state without adding
// a public write bypass to the production Memory repository. It has exactly
// the narrow credential transitions RecoveryService is allowed to call.
type legacyRecoveryUserRepository struct {
	*repository.MemoryUserRepository
	account domain.EmailAccount
}

func newLegacyRecoveryUserRepository(t *testing.T) *legacyRecoveryUserRepository {
	t.Helper()
	base := repository.NewMemoryUserRepository()
	user := &domain.User{
		ID: "33002", Username: "legacy_member", Nickname: "Legacy Member", Email: "legacy@example.test",
		Status: domain.UserStatusActive, AuthVersion: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := base.Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return &legacyRecoveryUserRepository{
		MemoryUserRepository: base,
		account: domain.EmailAccount{
			ID: "legacy:33002", UserID: user.ID, IdentifierNormalized: user.Email,
			VerificationState: domain.VerificationStateLegacyAccepted, CredentialVersion: 1,
		},
	}
}

func (r *legacyRecoveryUserRepository) GetEmailAccount(_ context.Context, userID string) (*domain.EmailAccount, error) {
	if userID != r.account.UserID {
		return nil, repository.ErrUserNotFound
	}
	copy := r.account
	return &copy, nil
}

func (r *legacyRecoveryUserRepository) UpdatePasswordForVerifiedEmail(ctx context.Context, userID, email, credential string) error {
	if r.account.VerificationState != domain.VerificationStateVerified {
		return repository.ErrAccountNotEligible
	}
	return r.MemoryUserRepository.UpdatePasswordForVerifiedEmail(ctx, userID, email, credential)
}

func (r *legacyRecoveryUserRepository) BindVerifiedEmail(ctx context.Context, userID, email, source string) error {
	if r.account.VerificationState == domain.VerificationStateSystemManaged {
		return repository.ErrAccountNotEligible
	}
	return r.MemoryUserRepository.BindVerifiedEmail(ctx, userID, email, source)
}

func (r *legacyRecoveryUserRepository) RecoverAccountWithEmailAndPassword(ctx context.Context, userID, accountID, email, credential, source string) error {
	if userID != r.account.UserID || accountID != r.account.ID || r.account.VerificationState != domain.VerificationStateLegacyAccepted {
		return repository.ErrAccountNotEligible
	}
	user, err := r.MemoryUserRepository.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	user.Email = domain.NormalizeEmail(email)
	if err := r.MemoryUserRepository.Update(ctx, user); err != nil {
		return err
	}
	if err := r.MemoryUserRepository.CreateVerifiedAccount(ctx, userID, email, credential); err != nil {
		return err
	}
	now := time.Now().UTC()
	r.account.IdentifierNormalized = domain.NormalizeEmail(email)
	r.account.VerificationState = domain.VerificationStateVerified
	r.account.VerifiedAt = &now
	r.account.VerificationSource = source
	r.account.CredentialVersion++
	r.account.PasswordChangedAt = &now
	return nil
}

func (r *legacyRecoveryUserRepository) UpdatePasswordForSystemManagedEmail(context.Context, string, string, string) error {
	return repository.ErrAccountNotEligible
}

var _ repository.AccountCredentialMutator = (*legacyRecoveryUserRepository)(nil)
