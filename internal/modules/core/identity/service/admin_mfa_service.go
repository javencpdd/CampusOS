package service

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var (
	ErrAdminMFAAvailable   = errors.New("admin MFA service is unavailable")
	ErrAdminMFANotEnabled  = errors.New("admin TOTP is not enrolled")
	ErrAdminMFACodeInvalid = errors.New("admin TOTP code is invalid")
	ErrAdminMFAReplay      = errors.New("admin TOTP step already accepted")
	ErrAdminMFAActor       = errors.New("admin MFA commands require an MFA admin actor")
)

const (
	adminTOTPDigits = 6
	adminTOTPPeriod = int64(30)
)

// AdminMFAService runs the admin domain's TOTP lifecycle on the independent
// admin security stores. Code generation and step windows mirror the user
// MFAService exactly; secrets never leave this service decrypted.
type AdminMFAService struct {
	admins    repository.AdminDomainRepository
	security  repository.AdminSecurityRepository
	sessions  repository.AdminSessionRepository
	protector *MFASecretProtector
	reliable  *reliability.Service
	now       func() time.Time
	// bootstrapAccountID is set by tests that need the fixture account id.
	bootstrapAccountID string
}

func NewAdminMFAService(admins repository.AdminDomainRepository, security repository.AdminSecurityRepository, sessions repository.AdminSessionRepository, protector *MFASecretProtector) *AdminMFAService {
	return &AdminMFAService{admins: admins, security: security, sessions: sessions, protector: protector, now: time.Now}
}

func (s *AdminMFAService) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil {
		return
	}
	if snapshotter, ok := s.security.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
}

// SetClock replaces the wall clock in tests; production keeps time.Now.
func (s *AdminMFAService) SetClock(now func() time.Time) { s.now = now }

func validAdminTOTPCode(code string) bool {
	if len(code) != adminTOTPDigits {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// requireSelf allows account-local first enrollment without prior MFA while
// still requiring an authenticated admin session bound to the active account.
func (s *AdminMFAService) requireSelf(ctx context.Context, actor AdminActor, accountID string) error {
	if actor.ID == "" || actor.ID != accountID {
		return ErrAdminMFAActor
	}
	account, err := s.admins.GetAccountByID(ctx, actor.ID)
	if err != nil || account.Status != "active" {
		return ErrAdminMFAActor
	}
	return nil
}

// requireActor enforces the contract: admin-domain MFA administration runs
// under an MFA-authenticated admin actor bound to an active account.
func (s *AdminMFAService) requireActor(ctx context.Context, actor AdminActor) error {
	if !actor.MFAAuthenticated || actor.ID == "" {
		return ErrAdminMFAActor
	}
	account, err := s.admins.GetAccountByID(ctx, actor.ID)
	if err != nil || account.Status != "active" {
		return ErrAdminMFAActor
	}
	return nil
}

// StartEnrollment seals a fresh TOTP secret for the actor and stores it as
// pending. The returned otpauth URL is shown exactly once.
func (s *AdminMFAService) StartEnrollment(ctx context.Context, actor AdminActor, issuer string) (string, error) {
	if s.security == nil || s.protector == nil {
		return "", ErrAdminMFAAvailable
	}
	if err := s.requireSelf(ctx, actor, actor.ID); err != nil {
		return "", err
	}
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	keyID, nonce, ciphertext, err := s.protector.Seal(secret)
	if err != nil {
		return "", err
	}
	account, err := s.admins.GetAccountByID(ctx, actor.ID)
	if err != nil {
		return "", err
	}
	if _, err := s.security.UpsertTOTP(ctx, repository.AdminTOTPMethod{
		AccountID: actor.ID, Status: "pending", KeyID: keyID, Nonce: nonce, Ciphertext: ciphertext,
		EnrollmentExpiresAt: s.now().Add(15 * time.Minute),
	}); err != nil {
		return "", err
	}
	secretText := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&digits=%d&period=%d",
		issuer, account.Username, secretText, issuer, adminTOTPDigits, adminTOTPPeriod), nil
}

// verifyTOTP decrypts the envelope and accepts a code in the ±1 step window
// that advances beyond the last accepted step (replay protection).
func (s *AdminMFAService) verifyTOTP(method repository.AdminTOTPMethod, code string) (int64, error) {
	secret, err := s.protector.Open(method.KeyID, method.Nonce, method.Ciphertext)
	if err != nil {
		return 0, err
	}
	current := s.now().UTC().Unix() / adminTOTPPeriod
	for offset := int64(-1); offset <= 1; offset++ {
		step := current + offset
		if step < 0 {
			continue
		}
		if totpCode(secret, step) == strings.TrimSpace(code) {
			return step, nil
		}
	}
	return 0, ErrAdminMFACodeInvalid
}

// ConfirmEnrollment activates a pending enrollment with its first code.
func (s *AdminMFAService) ConfirmEnrollment(ctx context.Context, actor AdminActor, code string) error {
	if s.security == nil || s.protector == nil {
		return ErrAdminMFAAvailable
	}
	if !validAdminTOTPCode(code) {
		return ErrAdminMFACodeInvalid
	}
	if err := s.requireSelf(ctx, actor, actor.ID); err != nil {
		return err
	}
	method, err := s.security.GetTOTP(ctx, actor.ID)
	if err != nil {
		return ErrAdminMFANotEnabled
	}
	if method.Status != "pending" || !s.now().Before(method.EnrollmentExpiresAt) {
		return ErrAdminMFANotEnabled
	}
	step, err := s.verifyTOTP(method, code)
	if err != nil {
		return err
	}
	// Activate and record the step in one CAS so the confirming code cannot
	// be replayed for an immediate step-up.
	confirmed, ok, err := s.security.ConfirmTOTPCAS(ctx, actor.ID, method.Version, s.now())
	if err != nil {
		return err
	}
	if !ok || confirmed.Status != "active" {
		return ErrAdminMFANotEnabled
	}
	_, _, err = s.security.RecordAdminTOTPStepCAS(ctx, actor.ID, confirmed.Version, step, s.now())
	return err
}

// VerifyStepUp accepts a current TOTP code for the actor and upgrades the
// given session to MFA strength inside one command transaction.
func (s *AdminMFAService) VerifyStepUp(ctx context.Context, actor AdminActor, sessionID, code string) error {
	if s.security == nil || s.protector == nil || s.sessions == nil {
		return ErrAdminMFAAvailable
	}
	if !validAdminTOTPCode(code) {
		return ErrAdminMFACodeInvalid
	}
	if err := s.requireActor(ctx, actor); err != nil {
		return err
	}
	method, err := s.security.GetTOTP(ctx, actor.ID)
	if err != nil || method.Status != "active" {
		return ErrAdminMFANotEnabled
	}
	step, err := s.verifyTOTP(method, code)
	if err != nil {
		return err
	}
	return s.execute(ctx, "identity.admin_mfa.step_up", sessionID, func(commandCtx context.Context) error {
		session, err := s.sessions.GetSessionByID(commandCtx, sessionID)
		if err != nil || session.RevokedAt != nil || session.AccountID != actor.ID {
			return ErrAdminSessionInvalid
		}
		_, ok, err := s.security.RecordAdminTOTPStepCAS(commandCtx, actor.ID, method.Version, step, s.now())
		if err != nil {
			return err
		}
		if !ok {
			return ErrAdminMFAReplay
		}
		_, _, err = s.sessions.UpgradeSessionStrengthCAS(commandCtx, sessionID, session.Version, "mfa", s.now(), s.now())
		return err
	})
}

// DisableTOTP turns the factor off under an MFA admin actor.
func (s *AdminMFAService) DisableTOTP(ctx context.Context, actor AdminActor) error {
	if s.security == nil {
		return ErrAdminMFAAvailable
	}
	if err := s.requireActor(ctx, actor); err != nil {
		return err
	}
	method, err := s.security.GetTOTP(ctx, actor.ID)
	if err != nil {
		return ErrAdminMFANotEnabled
	}
	_, _, err = s.security.DisableTOTPCAS(ctx, actor.ID, method.Version, s.now())
	return err
}

func (s *AdminMFAService) execute(ctx context.Context, code, resourceID string, action func(context.Context) error) error {
	if s.reliable == nil || transaction.Active(ctx) {
		return action(ctx)
	}
	event, err := reliability.NewEvent("authorization.changed", "admin_mfa", resourceID, map[string]string{
		"command": code, "resource_type": "admin_mfa", "resource_id": resourceID,
	})
	if err != nil {
		return err
	}
	return s.reliable.Execute(ctx, reliability.Command{
		Code: code, ActorType: "admin", ResourceType: "admin_mfa", ResourceID: resourceID,
		OperationCode: code, Event: &event,
	}, action)
}
