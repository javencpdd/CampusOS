package service

import (
	"context"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/internal/core/identity/repository"
	"github.com/campusos/CampusOS/pkg/auth"
)

type captureAccountRepo struct {
	userID     string
	email      string
	credential string
}

func (r *captureAccountRepo) CreateAccount(_ context.Context, userID, email, credential string) error {
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

func TestRegisterStoresHashedPasswordByDefault(t *testing.T) {
	accountRepo := &captureAccountRepo{}
	svc := NewUserService(repository.NewMemoryUserRepository(), testJWTManager(), accountRepo, nil)

	if _, err := svc.Register(context.Background(), domain.CreateUserRequest{
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

	if _, err := svc.Register(context.Background(), domain.CreateUserRequest{
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

func TestLoginUsesPlaintextCredentialWhenHashingDisabled(t *testing.T) {
	accountRepo := &captureAccountRepo{}
	userRepo := repository.NewMemoryUserRepository()
	svc := NewUserService(userRepo, testJWTManager(), accountRepo, nil)
	svc.SetPasswordHashEnabled(false)

	user, err := svc.Register(context.Background(), domain.CreateUserRequest{
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

func testJWTManager() *auth.JWTManager {
	return auth.NewJWTManager(auth.JWTConfig{
		Secret:     "test-secret",
		AccessTTL:  time.Hour,
		RefreshTTL: time.Hour,
		Issuer:     "test",
	})
}
