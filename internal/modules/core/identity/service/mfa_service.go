package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/campusos/CampusOS/pkg/observability"
)

var (
	ErrMFAInvalid             = errors.New("identity MFA request is invalid")
	ErrMFAUnavailable         = errors.New("identity MFA service is unavailable")
	ErrMFATicketInvalid       = errors.New("identity MFA ticket is invalid or expired")
	ErrMFACodeInvalid         = errors.New("identity MFA verification code is invalid")
	ErrMFAReplay              = errors.New("identity MFA verification code was already used")
	ErrMFAEnrollmentRequired  = errors.New("identity MFA enrollment is required")
	ErrMFANotEnabled          = errors.New("identity MFA is not enabled")
	ErrMFARecoveryCodeInvalid = errors.New("identity MFA recovery code is invalid")
	ErrMFAPolicyInvalid       = errors.New("identity MFA policy is invalid")
	ErrMFAPolicySafety        = errors.New("identity MFA policy safety requirements are not met")
	ErrMFAStepUpRequired      = errors.New("identity MFA step-up is required")
	ErrMFAPermission          = errors.New("identity MFA policy permission is denied")
)

const (
	mfaTicketMaxAttempts = 5
	mfaRecoveryCodeCount = 10
	mfaTOTPStepSeconds   = int64(30)
	mfaTOTPCodeDigits    = 6
)

type MFAConfig struct {
	ActiveKeyID            string
	EncryptionKeys         map[string]string
	Issuer                 string
	TicketTTL              time.Duration
	EnrollmentTTL          time.Duration
	StepUpTTL              time.Duration
	LocalRecoveryAvailable bool
	Clock                  func() time.Time
}

// MFARequirement is returned only after a successful password step. Its raw
// ticket is short lived and must remain in page memory; clients must never put
// it in localStorage, URL parameters, logs, support tickets, or exports.
type MFARequirement struct {
	Required      bool      `json:"mfa_required"`
	Ticket        string    `json:"mfa_ticket,omitempty"`
	ExpiresAt     time.Time `json:"mfa_expires_at,omitempty"`
	EnrollmentDue bool      `json:"mfa_enrollment_due,omitempty"`
}

type MFACompletedLogin struct {
	UserID   string
	Audience domain.MFAAudience
}

type MFAAdminPolicyUpdate struct {
	Mode            domain.MFAPolicyMode
	GraceEndsAt     *time.Time
	ExpectedVersion int64
}

type mfaPermissionChecker interface {
	CheckCode(context.Context, string, string) (bool, error)
}

// mfaSessionRevoker is intentionally narrower than SessionService. MFA may
// invalidate every session as part of a single reliable command, but it never
// receives a general-purpose session repository or token issuer.
type mfaSessionRevoker interface {
	RevokeAllForCommand(context.Context, string, string) (*domain.User, error)
}

// mfaSessionStrengthReader is deliberately limited to authoritative session
// state. The MFA policy never trusts a claim or a browser-side timestamp when
// deciding whether a management-plane request still has recent MFA proof.
type mfaSessionStrengthReader interface {
	HasRecentMFA(context.Context, string, string, time.Duration) (bool, error)
}

// MFAService is the only Identity service that may decrypt a TOTP envelope.
// It exposes state projections and one-shot enrollment material, not the
// persisted secret itself.
type MFAService struct {
	repo        repository.MFARepository
	users       UserLookup
	permissions mfaPermissionChecker
	audits      repository.AuthorizationRepository
	protector   *MFASecretProtector
	config      MFAConfig
	reliable    *reliability.Service
	meter       observability.Meter
	sessions    mfaSessionRevoker
	strengths   mfaSessionStrengthReader
}

func NewMFAService(
	repo repository.MFARepository,
	users UserLookup,
	permissions mfaPermissionChecker,
	audits repository.AuthorizationRepository,
	config MFAConfig,
) (*MFAService, error) {
	if repo == nil || users == nil || permissions == nil {
		return nil, ErrMFAUnavailable
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.TicketTTL <= 0 {
		config.TicketTTL = 5 * time.Minute
	}
	if config.EnrollmentTTL <= 0 {
		config.EnrollmentTTL = 10 * time.Minute
	}
	if config.StepUpTTL <= 0 {
		config.StepUpTTL = 15 * time.Minute
	}
	if strings.TrimSpace(config.Issuer) == "" {
		config.Issuer = "CampusOS"
	}
	protector, _ := NewMFASecretProtector(config.ActiveKeyID, config.EncryptionKeys)
	return &MFAService{repo: repo, users: users, permissions: permissions, audits: audits, protector: protector, config: config}, nil
}

func (s *MFAService) SetReliability(reliable *reliability.Service) {
	if s == nil {
		return
	}
	s.reliable = reliable
	if reliable != nil {
		if snapshotter, ok := s.repo.(transaction.Snapshotter); ok {
			reliable.RegisterMemorySnapshotters(snapshotter)
		}
	}
}

func (s *MFAService) SetMeter(meter observability.Meter) {
	if s != nil {
		s.meter = meter
	}
}

func (s *MFAService) SetSessionRevoker(sessions mfaSessionRevoker) {
	if s != nil {
		s.sessions = sessions
	}
}

func (s *MFAService) SetSessionStrengthReader(reader mfaSessionStrengthReader) {
	if s != nil {
		s.strengths = reader
	}
}

func (s *MFAService) Available() bool { return s != nil && s.protector != nil }

func (s *MFAService) StepUpTTL() time.Duration {
	if s == nil {
		return 0
	}
	return s.config.StepUpTTL
}

// CheckAdminMFA is the narrow management-plane gate consumed by transport
// middleware. It closes the otherwise possible bypass where an administrator
// signs in through the ordinary user endpoint and then presents that Session
// directly to an Admin API. A false result without an error means a normal
// step-up is needed; an error means the policy cannot be evaluated safely.
func (s *MFAService) CheckAdminMFA(ctx context.Context, userID, sessionID string) (bool, error) {
	if s == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return false, ErrMFAUnavailable
	}
	policy, err := s.repo.GetPolicy(ctx)
	if errors.Is(err, repository.ErrMFAPolicyNotFound) {
		// Rolling upgrades may briefly have old schema state. Do not turn an
		// absent policy row into an implicit enforcement policy.
		return true, nil
	}
	if err != nil || policy == nil {
		return false, err
	}
	now := s.now()
	required := policy.Mode == domain.MFAPolicyRequired ||
		(policy.Mode == domain.MFAPolicyEnrollmentGrace && policy.GraceEndsAt != nil && !now.Before(*policy.GraceEndsAt))
	if !required {
		return true, nil
	}
	if !s.Available() || s.strengths == nil {
		return false, ErrMFAUnavailable
	}
	return s.strengths.HasRecentMFA(ctx, userID, sessionID, s.config.StepUpTTL)
}

func (s *MFAService) BeginLogin(ctx context.Context, userID string, audience domain.MFAAudience) (*MFARequirement, error) {
	if s == nil || strings.TrimSpace(userID) == "" || !validMFAAudience(audience) {
		return nil, ErrMFAInvalid
	}
	method, err := s.repo.GetActiveTOTP(ctx, userID)
	if errors.Is(err, repository.ErrMFAMethodNotFound) {
		policy, policyErr := s.repo.GetPolicy(ctx)
		if policyErr != nil && !errors.Is(policyErr, repository.ErrMFAPolicyNotFound) {
			return nil, policyErr
		}
		now := s.now()
		required := audience == domain.MFAAudienceAdmin && policy != nil && (policy.Mode == domain.MFAPolicyRequired ||
			(policy.Mode == domain.MFAPolicyEnrollmentGrace && policy.GraceEndsAt != nil && !now.Before(*policy.GraceEndsAt)))
		if required {
			s.observe("login", "enrollment_required")
			return nil, ErrMFAEnrollmentRequired
		}
		due := audience == domain.MFAAudienceAdmin && policy != nil && policy.Mode == domain.MFAPolicyEnrollmentGrace &&
			(policy.GraceEndsAt == nil || now.Before(*policy.GraceEndsAt))
		return &MFARequirement{Required: false, EnrollmentDue: due}, nil
	}
	if err != nil {
		return nil, err
	}
	if method == nil || !s.Available() {
		s.observe("login", "unavailable")
		return nil, ErrMFAUnavailable
	}
	rawTicket, err := auth.NewOpaqueRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate MFA ticket: %w", err)
	}
	now := s.now()
	ticket := &repository.MFATicket{
		ID: strconv.FormatInt(idgen.New(), 10), UserID: userID, Audience: audience, Purpose: domain.MFATicketPurposeLogin,
		TicketDigest: mfaDigest(rawTicket), ExpiresAt: now.Add(s.config.TicketTTL), MaxAttempts: mfaTicketMaxAttempts,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreateTicket(ctx, ticket); err != nil {
		return nil, err
	}
	s.observe("login", "ticket_issued")
	return &MFARequirement{Required: true, Ticket: rawTicket, ExpiresAt: ticket.ExpiresAt}, nil
}

func (s *MFAService) CompleteLogin(ctx context.Context, rawTicket, code string) (*MFACompletedLogin, error) {
	if s == nil || strings.TrimSpace(rawTicket) == "" || !validTOTPCode(code) {
		return nil, ErrMFATicketInvalid
	}
	var completed *MFACompletedLogin
	var resultErr error
	err := s.execute(ctx, reliability.Command{
		Code: "identity.mfa.login.complete", ActorType: "anonymous", ResourceType: "identity_mfa_ticket",
		ResourceID: mfaDigest(rawTicket), OperationCode: "identity.mfa.login.complete",
	}, func(commandCtx context.Context) error {
		ticket, ticketErr := s.repo.GetTicketForUpdate(commandCtx, mfaDigest(rawTicket))
		if ticketErr != nil || ticket.ConsumedAt != nil || !s.now().Before(ticket.ExpiresAt) || ticket.Purpose != domain.MFATicketPurposeLogin || !validMFAAudience(ticket.Audience) {
			resultErr = ErrMFATicketInvalid
			return nil
		}
		method, methodErr := s.repo.GetActiveTOTPForUpdate(commandCtx, ticket.UserID)
		if methodErr != nil {
			return methodErr
		}
		step, codeErr := s.verifyTOTP(method, code)
		if codeErr != nil {
			_, _ = s.repo.RecordTicketFailure(commandCtx, ticket.TicketDigest, s.now())
			resultErr = codeErr
			return nil
		}
		accepted, acceptErr := s.repo.AcceptTOTPStep(commandCtx, method.ID, step, s.now())
		if acceptErr != nil {
			return acceptErr
		}
		if !accepted {
			resultErr = ErrMFAReplay
			return nil
		}
		consumed, consumeErr := s.repo.MarkTicketConsumed(commandCtx, ticket.TicketDigest, s.now())
		if consumeErr != nil {
			return consumeErr
		}
		if !consumed {
			resultErr = ErrMFATicketInvalid
			return nil
		}
		completed = &MFACompletedLogin{UserID: ticket.UserID, Audience: ticket.Audience}
		return nil
	})
	if err != nil {
		s.observe("login", "error")
		return nil, err
	}
	if resultErr != nil {
		s.observe("login", mfaResult(resultErr))
		return nil, resultErr
	}
	if completed == nil {
		s.observe("login", "invalid")
		return nil, ErrMFATicketInvalid
	}
	s.observe("login", "success")
	return completed, nil
}

func (s *MFAService) Status(ctx context.Context, userID string) (*domain.MFAStatus, error) {
	if s == nil || strings.TrimSpace(userID) == "" {
		return nil, ErrMFAInvalid
	}
	status := &domain.MFAStatus{MFAAvailable: s.Available(), StepUpRequiredAfter: int(s.config.StepUpTTL.Seconds())}
	if method, err := s.repo.GetActiveTOTP(ctx, userID); err == nil && method != nil {
		status.Enabled = true
	} else if err != nil && !errors.Is(err, repository.ErrMFAMethodNotFound) {
		return nil, err
	}
	if pending, err := s.repo.GetPendingTOTPForUpdate(ctx, userID); err == nil && pending != nil && s.now().Before(pending.EnrollmentExpiresAt) {
		status.PendingEnrollment = true
	} else if err != nil && !errors.Is(err, repository.ErrMFAMethodNotFound) {
		return nil, err
	}
	count, err := s.repo.CountRecoveryCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	status.RecoveryCodesRemaining = count
	policy, err := s.repo.GetPolicy(ctx)
	if err == nil && policy != nil {
		status.PolicyMode = policy.Mode
		status.GraceEndsAt = cloneMFAServiceTime(policy.GraceEndsAt)
	} else if err != nil && !errors.Is(err, repository.ErrMFAPolicyNotFound) {
		return nil, err
	}
	return status, nil
}

func (s *MFAService) StartEnrollment(ctx context.Context, userID string) (*domain.MFAEnrollment, error) {
	if s == nil || strings.TrimSpace(userID) == "" {
		return nil, ErrMFAInvalid
	}
	if !s.Available() {
		return nil, ErrMFAUnavailable
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil || user == nil || user.Status != domain.UserStatusActive {
		return nil, ErrMFAInvalid
	}
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate MFA seed: %w", err)
	}
	manualKey := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	keyID, nonce, ciphertext, err := s.protector.Seal(secret)
	if err != nil {
		return nil, ErrMFAUnavailable
	}
	now := s.now()
	method := &repository.MFATOTPMethod{
		ID: strconv.FormatInt(idgen.New(), 10), UserID: userID, State: repository.MFAMethodPending,
		KeyID: keyID, Nonce: nonce, Ciphertext: ciphertext, EnrollmentExpiresAt: now.Add(s.config.EnrollmentTTL), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.execute(ctx, reliability.Command{
		Code: "identity.mfa.enrollment.start", ActorID: userID, ResourceType: "identity_mfa_method", ResourceID: method.ID,
		OperationCode: "identity.mfa.enrollment.start",
	}, func(commandCtx context.Context) error { return s.repo.CreatePendingTOTP(commandCtx, method) }); err != nil {
		return nil, err
	}
	label := url.QueryEscape(strings.TrimSpace(s.config.Issuer) + ":" + user.Username)
	issuer := url.QueryEscape(strings.TrimSpace(s.config.Issuer))
	uri := fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30", label, manualKey, issuer)
	s.observe("enrollment", "started")
	return &domain.MFAEnrollment{ManualKey: manualKey, OTAuthURI: uri, ExpiresAt: method.EnrollmentExpiresAt}, nil
}

func (s *MFAService) ConfirmEnrollment(ctx context.Context, userID, code string) (*domain.MFAEnrollmentConfirmation, error) {
	if s == nil || strings.TrimSpace(userID) == "" || !validTOTPCode(code) {
		return nil, ErrMFAInvalid
	}
	if !s.Available() {
		return nil, ErrMFAUnavailable
	}
	var confirmation *domain.MFAEnrollmentConfirmation
	event, err := reliability.NewEvent("identity.mfa.enrolled.v1", "user", userID, map[string]string{"factor": "totp"})
	if err != nil {
		return nil, err
	}
	err = s.execute(ctx, reliability.Command{
		Code: "identity.mfa.enrollment.confirm", ActorID: userID, ResourceType: "identity_mfa_method", ResourceID: userID,
		OperationCode: "identity.mfa.enrollment.confirm", Event: &event,
	}, func(commandCtx context.Context) error {
		method, methodErr := s.repo.GetPendingTOTPForUpdate(commandCtx, userID)
		if methodErr != nil || !s.now().Before(method.EnrollmentExpiresAt) {
			return ErrMFACodeInvalid
		}
		step, codeErr := s.verifyTOTP(method, code)
		if codeErr != nil {
			return codeErr
		}
		if activateErr := s.repo.ActivatePendingTOTP(commandCtx, userID, method.ID, step, s.now()); activateErr != nil {
			return activateErr
		}
		codes, records, generateErr := newRecoveryCodes(userID, method.ID, s.now())
		if generateErr != nil {
			return generateErr
		}
		if replaceErr := s.repo.ReplaceRecoveryCodes(commandCtx, userID, method.ID, records); replaceErr != nil {
			return replaceErr
		}
		confirmation = &domain.MFAEnrollmentConfirmation{RecoveryCodes: codes}
		return nil
	})
	if err != nil {
		s.observe("enrollment", "error")
		return nil, err
	}
	if confirmation == nil {
		return nil, ErrMFAUnavailable
	}
	s.observe("enrollment", "confirmed")
	return confirmation, nil
}

// VerifyCurrentFactor accepts a current TOTP code and consumes its time step
// atomically. It is used by Step-up and high-risk self-service actions.
func (s *MFAService) VerifyCurrentFactor(ctx context.Context, userID, code string) error {
	if s == nil || strings.TrimSpace(userID) == "" || !validTOTPCode(code) {
		return ErrMFACodeInvalid
	}
	method, err := s.repo.GetActiveTOTPForUpdate(ctx, userID)
	if errors.Is(err, repository.ErrMFAMethodNotFound) {
		return ErrMFANotEnabled
	}
	if err != nil {
		return err
	}
	step, err := s.verifyTOTP(method, code)
	if err != nil {
		return err
	}
	accepted, err := s.repo.AcceptTOTPStep(ctx, method.ID, step, s.now())
	if err != nil {
		return err
	}
	if !accepted {
		return ErrMFAReplay
	}
	s.observe("verify", "success")
	return nil
}

func (s *MFAService) RotateRecoveryCodes(ctx context.Context, userID, code, recoveryCode string) (*domain.MFAEnrollmentConfirmation, error) {
	if s == nil || strings.TrimSpace(userID) == "" {
		return nil, ErrMFAInvalid
	}
	var result *domain.MFAEnrollmentConfirmation
	err := s.execute(ctx, reliability.Command{
		Code: "identity.mfa.recovery_codes.rotate", ActorID: userID, ResourceType: "identity_mfa_recovery_codes", ResourceID: userID,
		OperationCode: "identity.mfa.recovery_codes.rotate",
	}, func(commandCtx context.Context) error {
		method, err := s.repo.GetActiveTOTPForUpdate(commandCtx, userID)
		if errors.Is(err, repository.ErrMFAMethodNotFound) {
			return ErrMFANotEnabled
		}
		if err != nil {
			return err
		}
		if err := s.proveCurrentFactor(commandCtx, userID, method, code, recoveryCode); err != nil {
			return err
		}
		codes, records, err := newRecoveryCodes(userID, method.ID, s.now())
		if err != nil {
			return err
		}
		if err := s.repo.ReplaceRecoveryCodes(commandCtx, userID, method.ID, records); err != nil {
			return err
		}
		result = &domain.MFAEnrollmentConfirmation{RecoveryCodes: codes}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *MFAService) Disable(ctx context.Context, userID, code, recoveryCode string) error {
	if s == nil || strings.TrimSpace(userID) == "" {
		return ErrMFAInvalid
	}
	event, err := reliability.NewEvent("identity.mfa.disabled.v1", "user", userID, map[string]string{"factor": "totp"})
	if err != nil {
		return err
	}
	err = s.execute(ctx, reliability.Command{
		Code: "identity.mfa.disable", ActorID: userID, ResourceType: "identity_mfa_method", ResourceID: userID,
		OperationCode: "identity.mfa.disable", Event: &event,
	}, func(commandCtx context.Context) error {
		method, methodErr := s.repo.GetActiveTOTPForUpdate(commandCtx, userID)
		if errors.Is(methodErr, repository.ErrMFAMethodNotFound) {
			return ErrMFANotEnabled
		}
		if methodErr != nil {
			return methodErr
		}
		if proveErr := s.proveCurrentFactor(commandCtx, userID, method, code, recoveryCode); proveErr != nil {
			return proveErr
		}
		if disableErr := s.repo.DisableActiveTOTP(commandCtx, userID, s.now()); disableErr != nil {
			return disableErr
		}
		if replaceErr := s.repo.ReplaceRecoveryCodes(commandCtx, userID, method.ID, nil); replaceErr != nil {
			return replaceErr
		}
		if s.sessions == nil {
			return ErrMFAUnavailable
		}
		_, revokeErr := s.sessions.RevokeAllForCommand(commandCtx, userID, "mfa_disabled")
		return revokeErr
	})
	if err == nil {
		s.observe("disable", "success")
	}
	return err
}

// DisableFromLocalRecovery is the CLI-only recovery primitive. Its caller
// must enforce a local terminal, explicit confirmation and deployment-secret
// verification before invoking it. The method still records required audit
// evidence, emits a durable notification event, clears recovery codes and
// invalidates every session atomically.
func (s *MFAService) DisableFromLocalRecovery(ctx context.Context, userID, reason string) error {
	if s == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(reason) == "" || len(strings.TrimSpace(reason)) > 500 {
		return ErrMFAInvalid
	}
	if s.sessions == nil {
		return ErrMFAUnavailable
	}
	event, err := reliability.NewEvent("identity.mfa.local_recovered.v1", "user", userID, map[string]string{"action": "mfa_reset"})
	if err != nil {
		return err
	}
	return s.execute(ctx, reliability.Command{
		Code: "identity.mfa.local_recovery", ActorID: userID, ActorType: "local_operator", ResourceType: "identity_mfa_method", ResourceID: userID,
		OperationCode: "cli.identity.mfa.local_recovery", PermissionCode: "identity.mfa.local_recovery", Event: &event,
	}, func(commandCtx context.Context) error {
		method, methodErr := s.repo.GetActiveTOTPForUpdate(commandCtx, userID)
		if errors.Is(methodErr, repository.ErrMFAMethodNotFound) {
			return ErrMFANotEnabled
		}
		if methodErr != nil {
			return methodErr
		}
		if disableErr := s.repo.DisableActiveTOTP(commandCtx, userID, s.now()); disableErr != nil {
			return disableErr
		}
		if replaceErr := s.repo.ReplaceRecoveryCodes(commandCtx, userID, method.ID, nil); replaceErr != nil {
			return replaceErr
		}
		if _, revokeErr := s.sessions.RevokeAllForCommand(commandCtx, userID, "mfa_local_recovery"); revokeErr != nil {
			return revokeErr
		}
		return s.recordLocalRecoveryAudit(commandCtx, userID, reason)
	})
}

func (s *MFAService) GetAdminPolicy(ctx context.Context) (*domain.MFAAdminPolicyStatus, error) {
	if s == nil {
		return nil, ErrMFAUnavailable
	}
	policy, err := s.repo.GetPolicy(ctx)
	if err != nil {
		return nil, err
	}
	coverage, err := s.repo.AdminCoverage(ctx)
	if err != nil {
		return nil, err
	}
	coverage.LocalRecoveryAvailable = s.config.LocalRecoveryAvailable
	return &domain.MFAAdminPolicyStatus{Policy: *policy, Coverage: coverage, Available: s.Available()}, nil
}

func (s *MFAService) UpdateAdminPolicy(ctx context.Context, actorID string, request MFAAdminPolicyUpdate) (*domain.MFAAdminPolicyStatus, error) {
	if s == nil || strings.TrimSpace(actorID) == "" || request.ExpectedVersion < 1 || !validMFAPolicyMode(request.Mode) {
		return nil, ErrMFAPolicyInvalid
	}
	allowed, err := s.permissions.CheckCode(ctx, actorID, "identity.mfa_policy.update")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrMFAPermission
	}
	now := s.now()
	if request.Mode == domain.MFAPolicyEnrollmentGrace {
		if request.GraceEndsAt == nil || !request.GraceEndsAt.After(now) || request.GraceEndsAt.After(now.Add(90*24*time.Hour)) {
			return nil, ErrMFAPolicyInvalid
		}
	} else if request.GraceEndsAt != nil {
		return nil, ErrMFAPolicyInvalid
	}
	coverage, err := s.repo.AdminCoverage(ctx)
	if err != nil {
		return nil, err
	}
	if request.Mode == domain.MFAPolicyRequired {
		if !s.Available() || coverage.ActiveAdministrators < 1 || coverage.MFAEnrolledAdministrators < coverage.ActiveAdministrators || !s.config.LocalRecoveryAvailable {
			return nil, ErrMFAPolicySafety
		}
	}
	policy := domain.MFAPolicy{ID: "admin", Mode: request.Mode, GraceEndsAt: cloneMFAServiceTime(request.GraceEndsAt), UpdatedBy: actorID, UpdatedAt: now}
	var updated *domain.MFAPolicy
	event, err := reliability.NewEvent("identity.mfa.policy.updated.v1", "identity_mfa_policy", "admin", map[string]string{"mode": string(request.Mode)})
	if err != nil {
		return nil, err
	}
	err = s.execute(ctx, reliability.Command{
		Code: "identity.mfa_policy.update", ActorID: actorID, ActorType: "user", ResourceType: "identity_mfa_policy", ResourceID: "admin",
		OperationCode: "http.identity.mfa_policy.update", PermissionCode: "identity.mfa_policy.update", Event: &event,
	}, func(commandCtx context.Context) error {
		var updateErr error
		updated, updateErr = s.repo.UpdatePolicy(commandCtx, policy, request.ExpectedVersion)
		if updateErr != nil {
			return updateErr
		}
		return s.recordPolicyAudit(commandCtx, actorID, request.Mode)
	})
	if err != nil {
		return nil, err
	}
	coverage.LocalRecoveryAvailable = s.config.LocalRecoveryAvailable
	return &domain.MFAAdminPolicyStatus{Policy: *updated, Coverage: coverage, Available: s.Available()}, nil
}

func (s *MFAService) proveCurrentFactor(ctx context.Context, userID string, method *repository.MFATOTPMethod, code, recoveryCode string) error {
	if strings.TrimSpace(code) != "" {
		if !validTOTPCode(code) {
			return ErrMFACodeInvalid
		}
		step, err := s.verifyTOTP(method, code)
		if err != nil {
			return err
		}
		accepted, err := s.repo.AcceptTOTPStep(ctx, method.ID, step, s.now())
		if err != nil {
			return err
		}
		if !accepted {
			return ErrMFAReplay
		}
		return nil
	}
	recoveryCode = normalizeRecoveryCode(recoveryCode)
	if recoveryCode == "" {
		return ErrMFARecoveryCodeInvalid
	}
	consumed, err := s.repo.ConsumeRecoveryCode(ctx, userID, mfaDigest("recovery:"+recoveryCode), s.now())
	if err != nil {
		return err
	}
	if !consumed {
		return ErrMFARecoveryCodeInvalid
	}
	return nil
}

func (s *MFAService) verifyTOTP(method *repository.MFATOTPMethod, code string) (int64, error) {
	if !s.Available() || method == nil {
		return 0, ErrMFAUnavailable
	}
	secret, err := s.protector.Open(method.KeyID, method.Nonce, method.Ciphertext)
	if err != nil {
		return 0, ErrMFAUnavailable
	}
	step, ok := verifyTOTPCode(secret, code, s.now())
	if !ok {
		return 0, ErrMFACodeInvalid
	}
	return step, nil
}

func (s *MFAService) recordPolicyAudit(ctx context.Context, actorID string, mode domain.MFAPolicyMode) error {
	if s.audits == nil {
		if transaction.Active(ctx) {
			return ErrMFAUnavailable
		}
		return nil
	}
	return s.audits.RecordAuthorizationAudit(ctx, repository.AuthorizationAudit{
		ActorID: actorID, PermissionCode: "identity.mfa_policy.update", OperationCode: "http.identity.mfa_policy.update",
		ResourceType: "identity_mfa_policy", ResourceID: "admin", Outcome: "allow", Reason: "mode=" + string(mode),
	})
}

func (s *MFAService) recordLocalRecoveryAudit(ctx context.Context, userID, reason string) error {
	if s.audits == nil {
		if transaction.Active(ctx) {
			return ErrMFAUnavailable
		}
		return nil
	}
	return s.audits.RecordAuthorizationAudit(ctx, repository.AuthorizationAudit{
		ActorID: userID, PermissionCode: "identity.mfa.local_recovery", OperationCode: "cli.identity.mfa.local_recovery",
		ResourceType: "identity_mfa_method", ResourceID: userID, Outcome: "allow", Reason: strings.TrimSpace(reason),
	})
}

func (s *MFAService) execute(ctx context.Context, command reliability.Command, action func(context.Context) error) error {
	if s.reliable != nil {
		return s.reliable.Execute(ctx, command, action)
	}
	return action(ctx)
}

func (s *MFAService) now() time.Time {
	if s == nil || s.config.Clock == nil {
		return time.Now().UTC()
	}
	return s.config.Clock().UTC()
}

func (s *MFAService) observe(operation, result string) {
	if s == nil || s.meter == nil {
		return
	}
	_ = s.meter.AddCounter("campusos_identity_mfa_total", observability.Labels{"operation": operation, "result": result}, 1)
}

func validMFAAudience(value domain.MFAAudience) bool {
	return value == domain.MFAAudienceWeb || value == domain.MFAAudienceAdmin
}
func validMFAPolicyMode(value domain.MFAPolicyMode) bool {
	return value == domain.MFAPolicyOff || value == domain.MFAPolicyEnrollmentGrace || value == domain.MFAPolicyRequired
}
func validTOTPCode(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != mfaTOTPCodeDigits {
		return false
	}
	for _, runeValue := range value {
		if runeValue < '0' || runeValue > '9' {
			return false
		}
	}
	return true
}

func verifyTOTPCode(secret []byte, code string, now time.Time) (int64, bool) {
	if len(secret) == 0 || !validTOTPCode(code) {
		return 0, false
	}
	current := now.UTC().Unix() / mfaTOTPStepSeconds
	for offset := int64(-1); offset <= 1; offset++ {
		step := current + offset
		if step < 0 {
			continue
		}
		if hmac.Equal([]byte(totpCode(secret, step)), []byte(strings.TrimSpace(code))) {
			return step, true
		}
	}
	return 0, false
}

func totpCode(secret []byte, step int64) string {
	message := make([]byte, 8)
	for index := 7; index >= 0; index-- {
		message[index] = byte(step)
		step >>= 8
	}
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(message)
	digest := mac.Sum(nil)
	offset := int(digest[len(digest)-1] & 0x0f)
	value := (int(digest[offset])&0x7f)<<24 | int(digest[offset+1])<<16 | int(digest[offset+2])<<8 | int(digest[offset+3])
	return fmt.Sprintf("%06d", value%1000000)
}

func newRecoveryCodes(userID, methodID string, now time.Time) ([]string, []repository.MFARecoveryCode, error) {
	values := make([]string, 0, mfaRecoveryCodeCount)
	records := make([]repository.MFARecoveryCode, 0, mfaRecoveryCodeCount)
	seen := make(map[string]struct{}, mfaRecoveryCodeCount)
	for len(values) < mfaRecoveryCodeCount {
		bytes := make([]byte, 12)
		if _, err := rand.Read(bytes); err != nil {
			return nil, nil, fmt.Errorf("generate MFA recovery code: %w", err)
		}
		raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes)
		if _, exists := seen[raw]; exists {
			continue
		}
		seen[raw] = struct{}{}
		formatted := formatRecoveryCode(raw)
		values = append(values, formatted)
		records = append(records, repository.MFARecoveryCode{
			ID: strconv.FormatInt(idgen.New(), 10), UserID: userID, MethodID: methodID,
			Digest: mfaDigest("recovery:" + raw), CreatedAt: now,
		})
	}
	return values, records, nil
}

func formatRecoveryCode(value string) string {
	value = normalizeRecoveryCode(value)
	parts := make([]string, 0, (len(value)+3)/4)
	for len(value) > 4 {
		parts, value = append(parts, value[:4]), value[4:]
	}
	if value != "" {
		parts = append(parts, value)
	}
	return strings.Join(parts, "-")
}

func normalizeRecoveryCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.NewReplacer("-", "", " ", "", "\t", "", "\n", "").Replace(value)
	if len(value) < 16 || len(value) > 32 {
		return ""
	}
	for _, runeValue := range value {
		if !(runeValue >= 'A' && runeValue <= 'Z') && !(runeValue >= '2' && runeValue <= '7') {
			return ""
		}
	}
	return value
}

func mfaDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func cloneMFAServiceTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func mfaResult(err error) string {
	switch {
	case errors.Is(err, ErrMFACodeInvalid):
		return "invalid_code"
	case errors.Is(err, ErrMFAReplay):
		return "replay"
	case errors.Is(err, ErrMFAUnavailable):
		return "unavailable"
	default:
		return "invalid"
	}
}
