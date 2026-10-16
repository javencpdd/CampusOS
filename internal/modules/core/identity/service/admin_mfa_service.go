package service

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

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
	if snapshotter, ok := s.sessions.(transaction.Snapshotter); ok {
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
	if s == nil || s.protector == nil {
		return "", ErrAdminMFAAvailable
	}
	if issuer == "" || utf8.RuneCountInString(issuer) > 64 {
		return "", ErrAdminMFACodeInvalid
	}
	initial, err := s.loadSelfFacts(ctx, actor)
	if err != nil {
		return "", err
	}
	if initial.Method != nil && initial.Method.Status == "active" && initial.Authority.AuthenticationStrength != "mfa" {
		return "", ErrAdminMFAActor
	}
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	keyID, nonce, ciphertext, err := s.protector.Seal(secret)
	if err != nil {
		return "", err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	expected := cloneAdminMFASelfFacts(initial)
	version := int64(1)
	if initial.Method != nil {
		version = initial.Method.Version + 1
	}
	expected.Method = &repository.AdminTOTPMethod{AccountID: actor.ID, Status: "pending", KeyID: keyID, Nonce: nonce, Ciphertext: ciphertext, EnrollmentExpiresAt: now.Add(15 * time.Minute), Version: version}
	err = s.selfMutation(ctx, actor, "identity.admin_mfa.enroll", actor.ID, initial, expected, func(tx context.Context) error {
		receipt, err := s.security.UpsertTOTP(tx, *expected.Method)
		if err != nil {
			return err
		}
		if receipt.Version != version {
			return identityport.ErrPolicyFactsChanged
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	secretText := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	params := url.Values{"secret": {secretText}, "issuer": {issuer}, "digits": {"6"}, "period": {"30"}}
	uri := url.URL{Scheme: "otpauth", Host: "totp", Path: "/" + issuer + ":" + initial.Account.Username, RawQuery: params.Encode()}
	return uri.String(), nil
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
	if s == nil || s.protector == nil {
		return ErrAdminMFAAvailable
	}
	if !validAdminTOTPCode(code) {
		return ErrAdminMFACodeInvalid
	}
	initial, err := s.loadSelfFacts(ctx, actor)
	if err != nil {
		return err
	}
	if initial.Method == nil || initial.Method.Status != "pending" || !s.now().Before(initial.Method.EnrollmentExpiresAt) {
		return ErrAdminMFANotEnabled
	}
	step, err := s.verifyTOTP(*initial.Method, code)
	if err != nil {
		return err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	expected := cloneAdminMFASelfFacts(initial)
	expected.Method.Status = "active"
	expected.Method.ConfirmedAt = &now
	expected.Method.LastAcceptedStep = step
	expected.Method.Version += 2
	return s.selfMutation(ctx, actor, "identity.admin_mfa.confirm", actor.ID, initial, expected, func(tx context.Context) error {
		receipt, ok, err := s.security.ConfirmTOTPCAS(tx, actor.ID, initial.Method.Version, now)
		if err != nil {
			return err
		}
		if !ok || receipt.Version != initial.Method.Version+1 {
			return identityport.ErrPolicyFactsChanged
		}
		receipt, ok, err = s.security.RecordAdminTOTPStepCAS(tx, actor.ID, receipt.Version, step, now)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAdminMFAReplay
		}
		if receipt.Version != expected.Method.Version {
			return identityport.ErrPolicyFactsChanged
		}
		return nil
	})
}

// VerifyStepUp accepts a current TOTP code for the actor and upgrades the
// given session to MFA strength inside one command transaction.
func (s *AdminMFAService) VerifyStepUp(ctx context.Context, actor AdminActor, sessionID, code string) error {
	if s == nil || s.protector == nil {
		return ErrAdminMFAAvailable
	}
	if !validAdminTOTPCode(code) {
		return ErrAdminMFACodeInvalid
	}
	initial, err := s.loadSelfFacts(ctx, actor)
	if err != nil {
		return err
	}
	if actor.CredentialID != sessionID {
		return ErrAdminMFAActor
	}
	if initial.Method == nil || initial.Method.Status != "active" {
		return ErrAdminMFANotEnabled
	}
	step, err := s.verifyTOTP(*initial.Method, code)
	if err != nil {
		return err
	}
	if step <= initial.Method.LastAcceptedStep {
		return ErrAdminMFAReplay
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	expected := cloneAdminMFASelfFacts(initial)
	expected.Method.LastAcceptedStep = step
	expected.Method.Version++
	expected.Session.AuthenticationStrength = "mfa"
	expected.Session.MFAAuthenticatedAt = &now
	expected.Session.LastActiveAt = now
	expected.Session.UpdatedAt = now
	expected.Session.Version++
	expected.Authority.AuthenticationStrength = "mfa"
	expected.Authority.MFAAuthenticatedAt = &now
	expected.Authority.SessionVersion++
	return s.selfMutation(ctx, actor, "identity.admin_mfa.step_up", sessionID, initial, expected, func(tx context.Context) error {
		receipt, ok, err := s.security.RecordAdminTOTPStepCAS(tx, actor.ID, initial.Method.Version, step, now)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAdminMFAReplay
		}
		if receipt.Version != expected.Method.Version {
			return identityport.ErrPolicyFactsChanged
		}
		session, ok, err := s.sessions.UpgradeSessionStrengthCAS(tx, sessionID, initial.Session.Version, "mfa", now, now)
		if err != nil {
			return err
		}
		if !ok || session.Version != expected.Session.Version {
			return identityport.ErrPolicyFactsChanged
		}
		return nil
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

func (s *AdminMFAService) execute(ctx context.Context, code, resourceID, actorID string, action func(context.Context) error) error {
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
		Code: code, ActorType: "admin", ActorID: actorID, ResourceType: "admin_mfa", ResourceID: resourceID,
		OperationCode: code, Event: &event,
	}, action)
}
