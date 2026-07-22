package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var (
	ErrAdminAdmissionInvalid     = errors.New("administrator admission request is invalid")
	ErrAdminAdmissionPermission  = errors.New("administrator admission permission is denied")
	ErrAdminAdmissionUnavailable = errors.New("administrator admission service is unavailable")
)

// AdminAdmissionCommand intentionally contains the minimum mutation data.
// Reasons are retained only in required protected audit evidence, never in an
// outbox event or metrics label.
type AdminAdmissionCommand struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

// AdminAdmissionView joins a credential-free admission record with the small
// identity projection that an administrator needs to identify the subject.
type AdminAdmissionView struct {
	Account  repository.AdminAccount `json:"account"`
	Username string                  `json:"username,omitempty"`
	Nickname string                  `json:"nickname,omitempty"`
	Email    string                  `json:"email,omitempty"`
	Status   domain.UserStatus       `json:"user_status,omitempty"`
}

type adminAdmissionPermissionChecker interface {
	CheckCode(context.Context, string, string) (bool, error)
}

type adminAdmissionSessionRevoker interface {
	RevokeAllForCommand(context.Context, string, string) (*domain.User, error)
}

// AdminAdmissionService owns the management-plane admission lifecycle. It is
// deliberately separate from role management: role grants answer what an
// identity may do, while this service decides whether it may enter Admin at
// all.
type AdminAdmissionService struct {
	accounts    repository.AdminAccountRepository
	users       UserLookup
	permissions adminAdmissionPermissionChecker
	sessions    adminAdmissionSessionRevoker
	audits      repository.AuthorizationRepository
	reliable    *reliability.Service
	clock       func() time.Time
}

func NewAdminAdmissionService(
	accounts repository.AdminAccountRepository,
	users UserLookup,
	permissions adminAdmissionPermissionChecker,
	sessions adminAdmissionSessionRevoker,
	audits repository.AuthorizationRepository,
) (*AdminAdmissionService, error) {
	if accounts == nil || users == nil || permissions == nil || sessions == nil {
		return nil, ErrAdminAdmissionUnavailable
	}
	return &AdminAdmissionService{
		accounts: accounts, users: users, permissions: permissions, sessions: sessions, audits: audits, clock: time.Now,
	}, nil
}

func (s *AdminAdmissionService) SetReliability(reliable *reliability.Service) {
	if s == nil {
		return
	}
	s.reliable = reliable
	if reliable != nil {
		if snapshotter, ok := s.accounts.(transaction.Snapshotter); ok {
			reliable.RegisterMemorySnapshotters(snapshotter)
		}
		if snapshotter, ok := s.users.(transaction.Snapshotter); ok {
			reliable.RegisterMemorySnapshotters(snapshotter)
		}
	}
}

func (s *AdminAdmissionService) List(ctx context.Context, filter repository.AdminAccountListFilter) ([]AdminAdmissionView, int64, error) {
	if s == nil || s.accounts == nil {
		return nil, 0, ErrAdminAdmissionUnavailable
	}
	filter.Status = strings.TrimSpace(filter.Status)
	if filter.Status != "" && !validAdminAdmissionStatus(filter.Status) {
		return nil, 0, ErrAdminAdmissionInvalid
	}
	accounts, total, err := s.accounts.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	views := make([]AdminAdmissionView, 0, len(accounts))
	for _, account := range accounts {
		view, err := s.view(ctx, account)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, view)
	}
	return views, total, nil
}

func (s *AdminAdmissionService) Get(ctx context.Context, userID string) (*AdminAdmissionView, error) {
	if s == nil || s.accounts == nil {
		return nil, ErrAdminAdmissionUnavailable
	}
	if !validAdminAdmissionUserID(userID) {
		return nil, ErrAdminAdmissionInvalid
	}
	account, err := s.accounts.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	view, err := s.view(ctx, *account)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *AdminAdmissionService) Suspend(ctx context.Context, actorID, userID string, command AdminAdmissionCommand) (*AdminAdmissionView, error) {
	if err := s.validateMutation(ctx, actorID, userID, command, "identity.admin_account.suspend"); err != nil {
		return nil, err
	}
	var updated *repository.AdminAccount
	event, err := reliability.NewEvent("identity.admin_account.suspended.v1", "identity_admin_account", userID, map[string]string{
		"status": repository.AdminAccountStatusSuspended,
	})
	if err != nil {
		return nil, err
	}
	action := func(commandCtx context.Context) error {
		var transitionErr error
		updated, transitionErr = s.accounts.Suspend(commandCtx, userID, command.ExpectedVersion, actorID, strings.TrimSpace(command.Reason), s.now())
		if transitionErr != nil {
			return transitionErr
		}
		if _, revokeErr := s.sessions.RevokeAllForCommand(commandCtx, userID, "admin_admission_suspended"); revokeErr != nil {
			return revokeErr
		}
		return s.recordRequiredAudit(commandCtx, actorID, "identity.admin_account.suspend", "http.identity.admin_account.suspend", userID, command.Reason)
	}
	if err := s.execute(ctx, reliability.Command{
		Code: "identity.admin_account.suspend", ActorID: actorID, ActorType: "user",
		ResourceType: "identity_admin_account", ResourceID: userID,
		OperationCode: "http.identity.admin_account.suspend", PermissionCode: "identity.admin_account.suspend",
		Event: &event,
	}, action); err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrAdminAdmissionUnavailable
	}
	view, err := s.view(ctx, *updated)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *AdminAdmissionService) Restore(ctx context.Context, actorID, userID string, command AdminAdmissionCommand) (*AdminAdmissionView, error) {
	if err := s.validateMutation(ctx, actorID, userID, command, "identity.admin_account.restore"); err != nil {
		return nil, err
	}
	return s.restore(ctx, actorID, userID, command, "identity.admin_account.restore", "http.identity.admin_account.restore", "identity.admin_account.restored.v1")
}

// RestoreFromLocalRecovery is restricted to the local CLI after it has proved
// possession of deployment bootstrap material. It has no HTTP route and still
// records a reliable command plus required authorization evidence.
func (s *AdminAdmissionService) RestoreFromLocalRecovery(ctx context.Context, userID string, command AdminAdmissionCommand) (*AdminAdmissionView, error) {
	if s == nil || !validAdminAdmissionUserID(userID) || !validAdminAdmissionCommand(command) {
		return nil, ErrAdminAdmissionInvalid
	}
	return s.restore(ctx, "", userID, command, "identity.admin_account.local_restore", "cli.identity.admin_account.local_restore", "identity.admin_account.locally_restored.v1")
}

func (s *AdminAdmissionService) restore(ctx context.Context, actorID, userID string, command AdminAdmissionCommand, commandCode, operationCode, eventType string) (*AdminAdmissionView, error) {
	var updated *repository.AdminAccount
	event, err := reliability.NewEvent(eventType, "identity_admin_account", userID, map[string]string{
		"status": repository.AdminAccountStatusActive,
	})
	if err != nil {
		return nil, err
	}
	action := func(commandCtx context.Context) error {
		var transitionErr error
		updated, transitionErr = s.accounts.RestoreAdmission(commandCtx, userID, command.ExpectedVersion, actorID, strings.TrimSpace(command.Reason), s.now())
		if transitionErr != nil {
			return transitionErr
		}
		return s.recordRequiredAudit(commandCtx, actorID, "identity.admin_account.restore", operationCode, userID, command.Reason)
	}
	if err := s.execute(ctx, reliability.Command{
		Code: commandCode, ActorID: actorID, ActorType: localActorType(actorID), ResourceType: "identity_admin_account", ResourceID: userID,
		OperationCode: operationCode, PermissionCode: "identity.admin_account.restore", Event: &event,
	}, action); err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrAdminAdmissionUnavailable
	}
	view, err := s.view(ctx, *updated)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *AdminAdmissionService) ListAudits(ctx context.Context, limit int) ([]repository.AuthorizationAudit, error) {
	if s == nil || s.audits == nil {
		return nil, ErrAdminAdmissionUnavailable
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	items, err := s.audits.ListAuthorizationAudits(ctx, 500)
	if err != nil {
		return nil, err
	}
	filtered := make([]repository.AuthorizationAudit, 0, min(limit, len(items)))
	for _, item := range items {
		if !strings.HasPrefix(item.PermissionCode, "identity.admin_account.") {
			continue
		}
		filtered = append(filtered, item)
		if len(filtered) == limit {
			break
		}
	}
	return filtered, nil
}

func (s *AdminAdmissionService) validateMutation(ctx context.Context, actorID, userID string, command AdminAdmissionCommand, permissionCode string) error {
	if s == nil || s.accounts == nil || s.permissions == nil || s.sessions == nil {
		return ErrAdminAdmissionUnavailable
	}
	if !validAdminAdmissionUserID(actorID) || !validAdminAdmissionUserID(userID) || !validAdminAdmissionCommand(command) {
		return ErrAdminAdmissionInvalid
	}
	allowed, err := s.permissions.CheckCode(ctx, actorID, permissionCode)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrAdminAdmissionPermission
	}
	return nil
}

func (s *AdminAdmissionService) view(ctx context.Context, account repository.AdminAccount) (AdminAdmissionView, error) {
	view := AdminAdmissionView{Account: account}
	user, err := s.users.GetByID(ctx, account.UserID)
	if errors.Is(err, repository.ErrUserNotFound) {
		return view, nil
	}
	if err != nil {
		return AdminAdmissionView{}, err
	}
	view.Username = user.Username
	view.Nickname = user.Nickname
	view.Email = user.Email
	view.Status = user.Status
	return view, nil
}

func (s *AdminAdmissionService) recordRequiredAudit(ctx context.Context, actorID, permissionCode, operationCode, userID, reason string) error {
	if s.audits == nil {
		if transaction.Active(ctx) {
			return ErrAdminAdmissionUnavailable
		}
		return nil
	}
	return s.audits.RecordAuthorizationAudit(ctx, repository.AuthorizationAudit{
		ActorID: actorID, PermissionCode: permissionCode, OperationCode: operationCode,
		ResourceType: "identity_admin_account", ResourceID: userID, Outcome: "allow", Reason: strings.TrimSpace(reason),
	})
}

func (s *AdminAdmissionService) execute(ctx context.Context, command reliability.Command, action func(context.Context) error) error {
	if s.reliable == nil {
		return action(ctx)
	}
	return s.reliable.Execute(ctx, command, action)
}

func (s *AdminAdmissionService) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock().UTC()
}

func validAdminAdmissionStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "", repository.AdminAccountStatusActive, repository.AdminAccountStatusSuspended, repository.AdminAccountStatusRevoked:
		return true
	default:
		return false
	}
}

func validAdminAdmissionUserID(value string) bool {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return err == nil && parsed > 0
}

func validAdminAdmissionCommand(command AdminAdmissionCommand) bool {
	reason := strings.TrimSpace(command.Reason)
	return command.ExpectedVersion >= 1 && reason != "" && len(reason) <= 500
}

func localActorType(actorID string) string {
	if strings.TrimSpace(actorID) == "" {
		return "local_operator"
	}
	return "user"
}
