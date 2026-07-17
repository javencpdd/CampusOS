package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/idgen"
)

const SystemAdministratorEmail = "admin@campusos.local"

var (
	ErrRecoveryInvalid           = errors.New("identity recovery request is invalid, expired, or unavailable")
	ErrRecoveryUnavailable       = errors.New("identity recovery service is unavailable")
	ErrRecoveryEmailUnavailable  = errors.New("identity email cannot be used for this operation")
	ErrRecoveryCaseNotEligible   = errors.New("identity recovery case is not eligible")
	ErrSystemPasswordResetDenied = errors.New("system administrator password reset is not allowed")
)

// RecoveryConfig carries only runtime behavior. Challenge secrets remain
// encapsulated in ChallengeService and are never copied into recovery flows.
type RecoveryConfig struct {
	PasswordHashEnabled bool
	Clock               func() time.Time
}

// RecoveryService coordinates identity transitions that must atomically join
// Ticket consumption, account mutation, auth-version invalidation, session
// revocation, command audit, and a durable non-sensitive event.
type RecoveryService struct {
	users        repository.UserRepository
	credentials  repository.AccountCredentialMutator
	authVersions repository.AuthVersionWriter
	sessions     repository.SessionRepository
	challenges   *ChallengeService
	cases        repository.RecoveryCaseRepository
	reliable     *reliability.Service

	passwordHashEnabled bool
	clock               func() time.Time
}

func NewRecoveryService(
	users repository.UserRepository,
	sessions repository.SessionRepository,
	challenges *ChallengeService,
	cases repository.RecoveryCaseRepository,
	config RecoveryConfig,
) (*RecoveryService, error) {
	if users == nil || sessions == nil || challenges == nil || cases == nil {
		return nil, ErrRecoveryUnavailable
	}
	credentials, ok := users.(repository.AccountCredentialMutator)
	if !ok {
		return nil, errors.New("identity user repository does not implement account recovery mutations")
	}
	authVersions, ok := users.(repository.AuthVersionWriter)
	if !ok {
		return nil, errors.New("identity user repository does not implement auth-version invalidation")
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &RecoveryService{
		users: users, credentials: credentials, authVersions: authVersions, sessions: sessions,
		challenges: challenges, cases: cases, passwordHashEnabled: config.PasswordHashEnabled, clock: config.Clock,
	}, nil
}

func (s *RecoveryService) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil {
		return
	}
	if snapshotter, ok := s.users.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
	if snapshotter, ok := s.sessions.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
	if snapshotter, ok := s.cases.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
	// Compound recovery commands consume a Challenge in the same transaction.
	// Registering it here also keeps isolated memory-profile tests atomic.
	s.challenges.SetReliability(reliable)
}

// RequestPasswordReset deliberately returns an indistinguishable accepted
// response for absent, legacy, unverified, suspended, and rate-limited
// accounts. A random placeholder ID prevents response-shape enumeration.
func (s *RecoveryService) RequestPasswordReset(ctx context.Context, email, clientIP string) (*domain.PasswordResetChallengeResponse, error) {
	placeholder, err := auth.NewOpaqueRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate recovery placeholder: %w", err)
	}
	result := &domain.PasswordResetChallengeResponse{Accepted: true, ChallengeID: placeholder}
	email = domain.NormalizeEmail(email)
	if email == "" || domain.IsReservedEmail(email) {
		return result, nil
	}
	user, err := s.users.GetByEmail(ctx, email)
	if err != nil || user.Status != domain.UserStatusActive {
		return result, nil
	}
	account, err := s.credentials.GetEmailAccount(ctx, user.ID)
	if err != nil || account.IdentifierNormalized != email || account.VerificationState != domain.VerificationStateVerified {
		return result, nil
	}
	receipt, err := s.challenges.Request(ctx, domain.ChallengeRequest{
		Purpose: domain.ChallengePurposePasswordReset, Email: email, AccountID: account.ID, ClientIP: clientIP,
	})
	if errors.Is(err, ErrChallengeRateLimited) {
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("request password reset challenge: %w", err)
	}
	result.ChallengeID = receipt.PublicID
	return result, nil
}

func (s *RecoveryService) VerifyPasswordReset(ctx context.Context, request domain.PasswordResetVerificationRequest) (*domain.ChallengeTicket, error) {
	ticket, err := s.challenges.Verify(ctx, domain.ChallengeVerificationRequest{
		PublicID: request.ChallengeID, Purpose: domain.ChallengePurposePasswordReset, Code: request.Code,
	})
	if err != nil {
		return nil, ErrRecoveryInvalid
	}
	return ticket, nil
}

func (s *RecoveryService) CompletePasswordReset(ctx context.Context, request domain.PasswordResetCompletionRequest) error {
	email := domain.NormalizeEmail(request.Email)
	credential, err := s.passwordCredential(request.Password)
	if err != nil {
		return err
	}
	event, err := reliability.NewEvent("identity.password.reset.v1", "identity_email_challenge", strings.TrimSpace(request.ChallengeID), struct {
		ChallengeID string `json:"challenge_id"`
	}{ChallengeID: strings.TrimSpace(request.ChallengeID)})
	if err != nil {
		return err
	}
	err = s.execute(ctx, reliability.Command{
		Code: "identity.password.reset", ActorType: "anonymous", ResourceType: "identity_email_challenge",
		ResourceID: strings.TrimSpace(request.ChallengeID), OperationCode: "identity.password.reset",
		IdempotencyKey: "identity.password.reset:" + strings.TrimSpace(request.ChallengeID), Event: &event,
	}, func(commandCtx context.Context) error {
		challenge, consumeErr := s.challenges.ConsumeTicketForCommand(commandCtx, domain.ChallengeTicketConsumption{
			PublicID: request.ChallengeID, Purpose: domain.ChallengePurposePasswordReset, Ticket: request.Ticket, Email: email,
		})
		if consumeErr != nil {
			return ErrRecoveryInvalid
		}
		user, lookupErr := s.users.GetByEmail(commandCtx, email)
		if lookupErr != nil || user.Status != domain.UserStatusActive {
			return ErrRecoveryInvalid
		}
		account, lookupErr := s.credentials.GetEmailAccount(commandCtx, user.ID)
		if lookupErr != nil || account.ID != challenge.AccountID || account.IdentifierNormalized != email || account.VerificationState != domain.VerificationStateVerified {
			return ErrRecoveryInvalid
		}
		if mutationErr := s.credentials.UpdatePasswordForVerifiedEmail(commandCtx, user.ID, email, credential); mutationErr != nil {
			return mapRecoveryMutationError(mutationErr)
		}
		if _, bumpErr := s.authVersions.BumpAuthVersion(commandCtx, user.ID); bumpErr != nil {
			return bumpErr
		}
		if _, revokeErr := s.sessions.RevokeByUser(commandCtx, user.ID, "password_reset", s.now()); revokeErr != nil {
			return revokeErr
		}
		return nil
	})
	if err != nil {
		return mapRecoveryCommandError(err)
	}
	return nil
}

func (s *RecoveryService) RequestEmailBinding(ctx context.Context, userID string, request domain.EmailBindingChallengeRequest, clientIP string) (*domain.ChallengeReceipt, error) {
	email := domain.NormalizeEmail(request.Email)
	if email == "" || domain.IsReservedEmail(email) {
		return nil, ErrRecoveryEmailUnavailable
	}
	user, err := s.users.GetByID(ctx, strings.TrimSpace(userID))
	if err != nil || user.Status != domain.UserStatusActive {
		return nil, ErrRecoveryInvalid
	}
	account, err := s.credentials.GetEmailAccount(ctx, user.ID)
	if err != nil || account.VerificationState == domain.VerificationStateSystemManaged {
		return nil, ErrRecoveryEmailUnavailable
	}
	if existing, lookupErr := s.users.GetByEmail(ctx, email); lookupErr == nil && existing.ID != user.ID {
		return nil, ErrRecoveryEmailUnavailable
	}
	receipt, err := s.challenges.Request(ctx, domain.ChallengeRequest{
		Purpose: domain.ChallengePurposeEmailBinding, Email: email, AccountID: account.ID, ClientIP: clientIP,
	})
	if errors.Is(err, ErrChallengeRateLimited) {
		return nil, ErrChallengeRateLimited
	}
	if err != nil {
		return nil, fmt.Errorf("request email binding challenge: %w", err)
	}
	return receipt, nil
}

func (s *RecoveryService) VerifyEmailBinding(ctx context.Context, request domain.EmailBindingVerificationRequest) (*domain.ChallengeTicket, error) {
	ticket, err := s.challenges.Verify(ctx, domain.ChallengeVerificationRequest{
		PublicID: request.ChallengeID, Purpose: domain.ChallengePurposeEmailBinding, Code: request.Code,
	})
	if err != nil {
		return nil, ErrRecoveryInvalid
	}
	return ticket, nil
}

func (s *RecoveryService) CompleteEmailBinding(ctx context.Context, userID string, request domain.EmailBindingCompletionRequest) error {
	email := domain.NormalizeEmail(request.Email)
	event, err := reliability.NewEvent("identity.email.bound.v1", "user", strings.TrimSpace(userID), struct {
		UserID string `json:"user_id"`
	}{UserID: strings.TrimSpace(userID)})
	if err != nil {
		return err
	}
	err = s.execute(ctx, reliability.Command{
		Code: "identity.email.bind", ActorID: strings.TrimSpace(userID), ResourceType: "user", ResourceID: strings.TrimSpace(userID),
		OperationCode: "identity.email.bind", IdempotencyKey: "identity.email.bind:" + strings.TrimSpace(request.ChallengeID), Event: &event,
	}, func(commandCtx context.Context) error {
		challenge, consumeErr := s.challenges.ConsumeTicketForCommand(commandCtx, domain.ChallengeTicketConsumption{
			PublicID: request.ChallengeID, Purpose: domain.ChallengePurposeEmailBinding, Ticket: request.Ticket, Email: email,
		})
		if consumeErr != nil {
			return ErrRecoveryInvalid
		}
		account, lookupErr := s.credentials.GetEmailAccount(commandCtx, strings.TrimSpace(userID))
		if lookupErr != nil || account.ID != challenge.AccountID || account.VerificationState == domain.VerificationStateSystemManaged {
			return ErrRecoveryInvalid
		}
		if mutationErr := s.credentials.BindVerifiedEmail(commandCtx, strings.TrimSpace(userID), email, "email_binding"); mutationErr != nil {
			return mapRecoveryMutationError(mutationErr)
		}
		if _, bumpErr := s.authVersions.BumpAuthVersion(commandCtx, strings.TrimSpace(userID)); bumpErr != nil {
			return bumpErr
		}
		if _, revokeErr := s.sessions.RevokeByUser(commandCtx, strings.TrimSpace(userID), "email_binding", s.now()); revokeErr != nil {
			return revokeErr
		}
		return nil
	})
	if err != nil {
		return mapRecoveryCommandError(err)
	}
	return nil
}

func (s *RecoveryService) CreateAdminRecoveryCase(ctx context.Context, actorID string, request domain.AdminRecoveryCaseCreateRequest, clientIP string) (*domain.RecoveryCaseView, error) {
	userID := strings.TrimSpace(request.UserID)
	email := domain.NormalizeEmail(request.Email)
	proofReference := strings.TrimSpace(request.ProofReference)
	if userID == "" || email == "" || proofReference == "" || domain.IsReservedEmail(email) {
		return nil, ErrRecoveryCaseNotEligible
	}
	publicID, err := auth.NewOpaqueRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate recovery case id: %w", err)
	}
	caseValue := &domain.RecoveryCase{
		ID: strconvID(), PublicID: publicID, UserID: userID, TargetEmailNormalized: email, CreatedBy: strings.TrimSpace(actorID),
		ProofReference: proofReference, Status: domain.RecoveryCasePending, CreatedAt: s.now(), UpdatedAt: s.now(),
	}
	var event reliability.Event
	err = s.execute(ctx, reliability.Command{
		Code: "identity.account.recovery.create", ActorID: strings.TrimSpace(actorID), ActorType: "user", ResourceType: "identity_account_recovery_case",
		ResourceID: publicID, OperationCode: "identity.account.recovery.create", PermissionCode: "identity.account.recovery.override",
		IdempotencyKey: "identity.account.recovery.create:" + publicID,
		EventFactory:   func() (reliability.Event, error) { return event, nil },
	}, func(commandCtx context.Context) error {
		user, lookupErr := s.users.GetByID(commandCtx, userID)
		if lookupErr != nil || user.Status != domain.UserStatusActive {
			return ErrRecoveryCaseNotEligible
		}
		account, lookupErr := s.credentials.GetEmailAccount(commandCtx, userID)
		if lookupErr != nil || (account.VerificationState != domain.VerificationStateLegacyAccepted && account.VerificationState != domain.VerificationStateUnverified) {
			return ErrRecoveryCaseNotEligible
		}
		if existing, existingErr := s.users.GetByEmail(commandCtx, email); existingErr == nil && existing.ID != userID {
			return ErrRecoveryEmailUnavailable
		}
		challenge, requestedEvent, challengeErr := s.challenges.RequestForCommand(commandCtx, domain.ChallengeRequest{
			Purpose: domain.ChallengePurposePasswordReset, Email: email, AccountID: account.ID, ClientIP: clientIP,
		})
		if challengeErr != nil {
			if errors.Is(challengeErr, ErrChallengeRateLimited) {
				return ErrRecoveryEmailUnavailable
			}
			return challengeErr
		}
		requestedEvent.IdempotencyKey = "identity.email.challenge.request:" + challenge.ID
		caseValue.AccountID = account.ID
		caseValue.ChallengeID = challenge.ID
		caseValue.ExpiresAt = challenge.ExpiresAt
		caseValue.UpdatedAt = s.now()
		if createErr := s.cases.Create(commandCtx, caseValue); createErr != nil {
			return createErr
		}
		event = requestedEvent
		return nil
	})
	if err != nil {
		return nil, mapRecoveryCommandError(err)
	}
	view := caseValue.View()
	return &view, nil
}

func (s *RecoveryService) ListAdminRecoveryCases(ctx context.Context, limit int) ([]domain.RecoveryCaseView, error) {
	items, err := s.cases.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	views := make([]domain.RecoveryCaseView, 0, len(items))
	for _, value := range items {
		views = append(views, value.View())
	}
	return views, nil
}

func (s *RecoveryService) CancelAdminRecoveryCase(ctx context.Context, actorID, publicID string) error {
	publicID = strings.TrimSpace(publicID)
	event, err := reliability.NewEvent("identity.account.recovery.cancelled.v1", "identity_account_recovery_case", publicID, struct {
		CaseID string `json:"case_id"`
	}{CaseID: publicID})
	if err != nil {
		return err
	}
	err = s.execute(ctx, reliability.Command{
		Code: "identity.account.recovery.cancel", ActorID: strings.TrimSpace(actorID), ActorType: "user", ResourceType: "identity_account_recovery_case",
		ResourceID: publicID, OperationCode: "identity.account.recovery.cancel", PermissionCode: "identity.account.recovery.override",
		IdempotencyKey: "identity.account.recovery.cancel:" + publicID, Event: &event,
	}, func(commandCtx context.Context) error {
		caseValue, lookupErr := s.cases.GetByPublicIDForUpdate(commandCtx, publicID)
		if errors.Is(lookupErr, repository.ErrRecoveryCaseNotFound) {
			return ErrRecoveryCaseNotEligible
		}
		if lookupErr != nil {
			return lookupErr
		}
		if caseValue.Status != domain.RecoveryCasePending || !s.now().Before(caseValue.ExpiresAt) {
			return ErrRecoveryCaseNotEligible
		}
		now := s.now()
		caseValue.Status = domain.RecoveryCaseCancelled
		caseValue.CancelledAt = &now
		caseValue.UpdatedAt = now
		return s.cases.Update(commandCtx, caseValue)
	})
	return mapRecoveryCommandError(err)
}

func (s *RecoveryService) CompleteAdminRecoveryCase(ctx context.Context, request domain.RecoveryCaseCompletionRequest) error {
	credential, err := s.passwordCredential(request.Password)
	if err != nil {
		return err
	}
	event, err := reliability.NewEvent("identity.account.recovery.completed.v1", "identity_email_challenge", strings.TrimSpace(request.ChallengeID), struct {
		ChallengeID string `json:"challenge_id"`
	}{ChallengeID: strings.TrimSpace(request.ChallengeID)})
	if err != nil {
		return err
	}
	err = s.execute(ctx, reliability.Command{
		Code: "identity.account.recovery.complete", ActorType: "anonymous", ResourceType: "identity_email_challenge",
		ResourceID: strings.TrimSpace(request.ChallengeID), OperationCode: "identity.account.recovery.complete",
		IdempotencyKey: "identity.account.recovery.complete:" + strings.TrimSpace(request.ChallengeID), Event: &event,
	}, func(commandCtx context.Context) error {
		preview, previewErr := s.challenges.LookupForCommand(commandCtx, strings.TrimSpace(request.ChallengeID), domain.ChallengePurposePasswordReset)
		if previewErr != nil {
			return ErrRecoveryInvalid
		}
		caseValue, lookupErr := s.cases.GetByChallengeIDForUpdate(commandCtx, preview.ID)
		if lookupErr != nil || caseValue.Status != domain.RecoveryCasePending || !s.now().Before(caseValue.ExpiresAt) {
			return ErrRecoveryInvalid
		}
		challenge, consumeErr := s.challenges.ConsumeTicketForCommand(commandCtx, domain.ChallengeTicketConsumption{
			PublicID: request.ChallengeID, Purpose: domain.ChallengePurposePasswordReset, Ticket: request.Ticket, Email: caseValue.TargetEmailNormalized,
		})
		if consumeErr != nil || challenge.ID != caseValue.ChallengeID || challenge.AccountID != caseValue.AccountID {
			return ErrRecoveryInvalid
		}
		user, lookupErr := s.users.GetByID(commandCtx, caseValue.UserID)
		if lookupErr != nil || user.Status != domain.UserStatusActive {
			return ErrRecoveryInvalid
		}
		if mutationErr := s.credentials.RecoverAccountWithEmailAndPassword(commandCtx, caseValue.UserID, caseValue.AccountID, caseValue.TargetEmailNormalized, credential, "admin_recovery"); mutationErr != nil {
			return mapRecoveryMutationError(mutationErr)
		}
		if _, bumpErr := s.authVersions.BumpAuthVersion(commandCtx, caseValue.UserID); bumpErr != nil {
			return bumpErr
		}
		if _, revokeErr := s.sessions.RevokeByUser(commandCtx, caseValue.UserID, "admin_recovery", s.now()); revokeErr != nil {
			return revokeErr
		}
		now := s.now()
		caseValue.Status = domain.RecoveryCaseCompleted
		caseValue.CompletedAt = &now
		caseValue.UpdatedAt = now
		return s.cases.Update(commandCtx, caseValue)
	})
	if err != nil {
		return mapRecoveryCommandError(err)
	}
	return nil
}

func (s *RecoveryService) ListUserSessions(ctx context.Context, userID string) ([]domain.SessionView, error) {
	items, err := s.sessions.ListByUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	views := make([]domain.SessionView, 0, len(items))
	for _, item := range items {
		views = append(views, item.View(false))
	}
	return views, nil
}

func (s *RecoveryService) RevokeUserSessionsByAdmin(ctx context.Context, actorID, userID string) error {
	userID = strings.TrimSpace(userID)
	event, err := reliability.NewEvent("identity.session.admin_revoked.v1", "user", userID, struct {
		UserID string `json:"user_id"`
	}{UserID: userID})
	if err != nil {
		return err
	}
	err = s.execute(ctx, reliability.Command{
		Code: "identity.session.admin_revoke", ActorID: strings.TrimSpace(actorID), ActorType: "user", ResourceType: "user", ResourceID: userID,
		OperationCode: "identity.session.admin_revoke", PermissionCode: "identity.session.revoke",
		IdempotencyKey: "identity.session.admin_revoke:" + userID + ":" + fmt.Sprintf("%d", s.now().UnixNano()), Event: &event,
	}, func(commandCtx context.Context) error {
		if _, lookupErr := s.users.GetByID(commandCtx, userID); lookupErr != nil {
			return ErrRecoveryInvalid
		}
		if _, bumpErr := s.authVersions.BumpAuthVersion(commandCtx, userID); bumpErr != nil {
			return bumpErr
		}
		_, revokeErr := s.sessions.RevokeByUser(commandCtx, userID, "admin_revoked", s.now())
		return revokeErr
	})
	return mapRecoveryCommandError(err)
}

// ResetSystemAdministratorPassword is intentionally local-command only. HTTP
// handlers must never call it because system-managed accounts are excluded
// from public email and administrator-assisted recovery.
func (s *RecoveryService) ResetSystemAdministratorPassword(ctx context.Context, email, password string) error {
	email = domain.NormalizeEmail(email)
	if email != SystemAdministratorEmail {
		return ErrSystemPasswordResetDenied
	}
	credential, err := s.passwordCredential(password)
	if err != nil {
		return err
	}
	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return ErrSystemPasswordResetDenied
	}
	account, err := s.credentials.GetEmailAccount(ctx, user.ID)
	if err != nil || account.VerificationState != domain.VerificationStateSystemManaged {
		return ErrSystemPasswordResetDenied
	}
	event, err := reliability.NewEvent("identity.system_password.reset.v1", "user", user.ID, struct {
		UserID string `json:"user_id"`
	}{UserID: user.ID})
	if err != nil {
		return err
	}
	err = s.execute(ctx, reliability.Command{
		Code: "identity.system_password.reset", ActorID: "local-cli", ActorType: "system", ResourceType: "user", ResourceID: user.ID,
		OperationCode: "identity.system_password.reset", IdempotencyKey: "identity.system_password.reset:" + user.ID + ":" + fmt.Sprintf("%d", s.now().UnixNano()), Event: &event,
	}, func(commandCtx context.Context) error {
		if mutationErr := s.credentials.UpdatePasswordForSystemManagedEmail(commandCtx, user.ID, email, credential); mutationErr != nil {
			return mapRecoveryMutationError(mutationErr)
		}
		if _, bumpErr := s.authVersions.BumpAuthVersion(commandCtx, user.ID); bumpErr != nil {
			return bumpErr
		}
		_, revokeErr := s.sessions.RevokeByUser(commandCtx, user.ID, "local_password_reset", s.now())
		return revokeErr
	})
	return mapRecoveryCommandError(err)
}

func (s *RecoveryService) passwordCredential(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", ErrRecoveryInvalid
	}
	if !s.passwordHashEnabled {
		return password, nil
	}
	credential, err := auth.HashPassword(password)
	if err != nil {
		return "", fmt.Errorf("hash recovery password: %w", err)
	}
	return credential, nil
}

func (s *RecoveryService) execute(ctx context.Context, command reliability.Command, action func(context.Context) error) error {
	if s.reliable != nil {
		return s.reliable.Execute(ctx, command, action)
	}
	return action(ctx)
}

func (s *RecoveryService) now() time.Time { return s.clock().UTC() }

func strconvID() string { return fmt.Sprintf("%d", idgen.New()) }

func mapRecoveryMutationError(err error) error {
	if errors.Is(err, repository.ErrEmailExists) || errors.Is(err, repository.ErrAccountNotEligible) || errors.Is(err, repository.ErrUserNotFound) {
		return ErrRecoveryInvalid
	}
	return err
}

func mapRecoveryCommandError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRecoveryInvalid) || errors.Is(err, ErrRecoveryCaseNotEligible) || errors.Is(err, ErrRecoveryEmailUnavailable) || errors.Is(err, ErrChallengeTicket) {
		return ErrRecoveryInvalid
	}
	return err
}
