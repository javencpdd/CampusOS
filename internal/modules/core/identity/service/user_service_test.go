package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

type captureAccountRepo struct {
	userID     string
	email      string
	credential string
}

type failingAccountRepo struct{ captureAccountRepo }

func (r *failingAccountRepo) CreateVerifiedAccount(context.Context, string, string, string) error {
	return errors.New("account write failed")
}

func (r *captureAccountRepo) CreateVerifiedAccount(_ context.Context, userID, email, credential string) error {
	r.userID = userID
	r.email = email
	r.credential = credential
	return nil
}

func (r *captureAccountRepo) GetCredentialByEmail(_ context.Context, email string) (string, string, error) {
	if email != r.email {
		return "", "", repository.ErrUserNotFound
	}
	return r.userID, r.credential, nil
}

func (r *captureAccountRepo) GetEmailAccount(_ context.Context, userID string) (*domain.EmailAccount, error) {
	if userID != r.userID {
		return nil, repository.ErrUserNotFound
	}
	now := time.Now().UTC()
	return &domain.EmailAccount{
		ID:                   "capture:" + userID,
		UserID:               userID,
		IdentifierNormalized: r.email,
		VerificationState:    domain.VerificationStateVerified,
		VerifiedAt:           &now,
		VerificationSource:   "test",
		CredentialVersion:    1,
		PasswordChangedAt:    &now,
	}, nil
}

type acceptingRegistrationTicket struct{}

func (acceptingRegistrationTicket) ConsumeTicketForCommand(_ context.Context, request domain.ChallengeTicketConsumption) (*domain.EmailChallenge, error) {
	if request.Purpose != domain.ChallengePurposeRegistration || request.PublicID == "" || request.Ticket == "" || request.Email == "" {
		return nil, ErrChallengeTicket
	}
	return &domain.EmailChallenge{PublicID: request.PublicID, Purpose: request.Purpose, EmailNormalized: request.Email}, nil
}

func registerVerifiedForTest(svc *UserService, req domain.CreateUserRequest) (*domain.User, error) {
	svc.SetRegistrationTicketConsumer(acceptingRegistrationTicket{})
	return svc.RegisterVerified(context.Background(), domain.RegistrationRequest{
		Username: req.Username, Nickname: req.Nickname, Email: req.Email, Password: req.Password,
		ChallengeID: "test-registration-challenge", Ticket: "test-registration-ticket",
	})
}

func TestRegisterStoresHashedPasswordByDefault(t *testing.T) {
	accountRepo := &captureAccountRepo{}
	svc := NewUserService(repository.NewMemoryUserRepository(), testJWTManager(), accountRepo, nil)

	if _, err := registerVerifiedForTest(svc, domain.CreateUserRequest{
		Username: "hashed_user",
		Nickname: "Hashed User",
		Email:    "hashed@example.test",
		Password: "Secret123",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if accountRepo.credential == "Secret123" {
		t.Fatalf("expected hashed credential, got plaintext")
	}
	if !auth.CheckPassword("Secret123", accountRepo.credential) {
		t.Fatalf("expected credential to verify with bcrypt")
	}
}

func TestRegisterStoresPlaintextPasswordWhenHashingDisabled(t *testing.T) {
	accountRepo := &captureAccountRepo{}
	svc := NewUserService(repository.NewMemoryUserRepository(), testJWTManager(), accountRepo, nil)
	svc.SetPasswordHashEnabled(false)

	if _, err := registerVerifiedForTest(svc, domain.CreateUserRequest{
		Username: "plain_user",
		Nickname: "Plain User",
		Email:    "plain@example.test",
		Password: "Secret123",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if accountRepo.credential != "Secret123" {
		t.Fatalf("expected plaintext credential, got %q", accountRepo.credential)
	}
}

func TestRegisterNormalizesEmailAndRejectsReservedPlaceholder(t *testing.T) {
	accountRepo := &captureAccountRepo{}
	users := repository.NewMemoryUserRepository()
	svc := NewUserService(users, testJWTManager(), accountRepo, nil)

	user, err := registerVerifiedForTest(svc, domain.CreateUserRequest{
		Username: "normalized_user",
		Nickname: "Normalized User",
		Email:    "  Normalized@Example.Test ",
		Password: "Secret123",
	})
	if err != nil {
		t.Fatalf("register normalized email: %v", err)
	}
	if user.Email != "normalized@example.test" || accountRepo.email != "normalized@example.test" {
		t.Fatalf("email normalization user=%q account=%q", user.Email, accountRepo.email)
	}
	if _, err := users.GetByEmail(context.Background(), "NORMALIZED@EXAMPLE.TEST"); err != nil {
		t.Fatalf("normalized lookup: %v", err)
	}

	if _, err := registerVerifiedForTest(svc, domain.CreateUserRequest{
		Username: "reserved_email_user",
		Nickname: "Reserved Email User",
		Email:    "1904650862@qq.com",
		Password: "Secret123",
	}); err == nil {
		t.Fatal("reserved placeholder email registration unexpectedly succeeded")
	}
}

func TestLoginUsesPlaintextCredentialWhenHashingDisabled(t *testing.T) {
	accountRepo := &captureAccountRepo{}
	userRepo := repository.NewMemoryUserRepository()
	svc := NewUserService(userRepo, testJWTManager(), accountRepo, nil)
	svc.SetPasswordHashEnabled(false)

	user, err := registerVerifiedForTest(svc, domain.CreateUserRequest{
		Username: "plain_login_user",
		Nickname: "Plain Login User",
		Email:    "plain-login@example.test",
		Password: "Secret123",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	loggedIn, _, _, err := svc.Login(context.Background(), domain.LoginRequest{
		Email:    "plain-login@example.test",
		Password: "Secret123",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if loggedIn.ID != user.ID {
		t.Fatalf("expected user %s, got %s", user.ID, loggedIn.ID)
	}
}

func TestRegisterRollsBackUserWhenAccountOrRequiredAuditFails(t *testing.T) {
	userRepo := repository.NewMemoryUserRepository()
	svc := NewUserService(userRepo, testJWTManager(), &failingAccountRepo{}, nil)
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))
	_, err := registerVerifiedForTest(svc, domain.CreateUserRequest{
		Username: "rollback_user",
		Nickname: "Rollback User",
		Email:    "rollback@example.test",
		Password: "Secret123",
	})
	if err == nil {
		t.Fatal("expected account write failure")
	}
	if _, getErr := userRepo.GetByEmail(context.Background(), "rollback@example.test"); !errors.Is(getErr, repository.ErrUserNotFound) {
		t.Fatalf("failed registration left a user record: %v", getErr)
	}
}

func TestRegisterVerifiedRollsBackTicketWhenAccountWriteFails(t *testing.T) {
	challenges, store, reliable, _ := newTestChallengeService(t)
	ctx := context.Background()
	receipt, err := challenges.Request(ctx, domain.ChallengeRequest{
		Purpose: domain.ChallengePurposeRegistration, Email: "rollback-ticket@example.test", ClientIP: "203.0.113.56",
	})
	if err != nil {
		t.Fatalf("request challenge: %v", err)
	}
	challenge, err := store.GetChallenge(ctx, receipt.PublicID)
	if err != nil {
		t.Fatalf("load challenge: %v", err)
	}
	dispatch, err := challenges.Dispatch(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("dispatch challenge: %v", err)
	}
	ticket, err := challenges.Verify(ctx, domain.ChallengeVerificationRequest{
		PublicID: receipt.PublicID, Purpose: domain.ChallengePurposeRegistration, Code: dispatch.Code,
	})
	if err != nil {
		t.Fatalf("verify challenge: %v", err)
	}

	users := repository.NewMemoryUserRepository()
	svc := NewUserService(users, testJWTManager(), &failingAccountRepo{}, nil)
	svc.SetReliability(reliable)
	svc.SetRegistrationTicketConsumer(challenges)
	if _, err := svc.RegisterVerified(ctx, domain.RegistrationRequest{
		Username: "ticket_rollback", Nickname: "Ticket Rollback", Email: "rollback-ticket@example.test", Password: "Secret123",
		ChallengeID: receipt.PublicID, Ticket: ticket.Ticket,
	}); err == nil {
		t.Fatal("expected verified account write failure")
	}

	stored, err := store.GetChallenge(ctx, receipt.PublicID)
	if err != nil {
		t.Fatalf("reload challenge: %v", err)
	}
	if stored.ConsumedAt != nil {
		t.Fatalf("failed registration consumed ticket at %s", stored.ConsumedAt)
	}
	if _, err := users.GetByEmail(ctx, "rollback-ticket@example.test"); !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("failed registration left a user record: %v", err)
	}
}

func TestRegisterWithoutVerificationTicketFailsClosed(t *testing.T) {
	svc := NewUserService(repository.NewMemoryUserRepository(), testJWTManager(), repository.NewMemoryUserRepository(), nil)
	if _, err := svc.Register(context.Background(), domain.CreateUserRequest{
		Username: "legacy_register", Nickname: "Legacy Register", Email: "legacy@example.test", Password: "Secret123",
	}); !errors.Is(err, ErrRegistrationVerificationRequired) {
		t.Fatalf("legacy one-step registration error = %v", err)
	}
}

func testJWTManager() *auth.JWTManager {
	return auth.NewJWTManager(auth.JWTConfig{
		Secret:     "test-secret",
		AccessTTL:  time.Hour,
		RefreshTTL: time.Hour,
		Issuer:     "test",
	})
}
