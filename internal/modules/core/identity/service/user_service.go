package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

type UserService struct {
	repo                repository.UserRepository
	jwtMgr              *auth.JWTManager
	pgRepo              PgUserRepo
	roleRepo            RoleQuerier
	registrationTickets RegistrationTicketConsumer
	bus                 eventbus.EventBus
	reliable            *reliability.Service

	passwordHashEnabled bool
}

type PgUserRepo interface {
	CreateVerifiedAccount(ctx context.Context, userID, email, hashedPassword string) error
	GetCredentialByEmail(ctx context.Context, email string) (string, string, error)
	GetEmailAccount(context.Context, string) (*domain.EmailAccount, error)
}

type registrationCompensator interface {
	DeleteForRegistration(context.Context, string) error
}

// RegistrationTicketConsumer is deliberately narrow. User registration can
// consume a Ticket inside its reliable command, but never receives access to
// Challenge storage or any verification secret.
type RegistrationTicketConsumer interface {
	ConsumeTicketForCommand(context.Context, domain.ChallengeTicketConsumption) (*domain.EmailChallenge, error)
}

var (
	ErrRegistrationVerificationRequired = errors.New("registration email verification is required")
	ErrRegistrationTicketInvalid        = errors.New("registration verification ticket is invalid")
)

type RoleQuerier interface {
	GetUserRoles(ctx context.Context, userID string) ([]*repository.Role, error)
}

func NewUserService(repo repository.UserRepository, jwtMgr *auth.JWTManager, pgRepo PgUserRepo, bus eventbus.EventBus) *UserService {
	return &UserService{
		repo:                repo,
		jwtMgr:              jwtMgr,
		pgRepo:              pgRepo,
		bus:                 bus,
		passwordHashEnabled: true,
	}
}

// SetRoleRepository 设置角色仓储（用于登录时注入角色信息）
func (s *UserService) SetRoleRepository(roleRepo RoleQuerier) {
	s.roleRepo = roleRepo
}

func (s *UserService) SetRegistrationTicketConsumer(consumer RegistrationTicketConsumer) {
	s.registrationTickets = consumer
}

// SetPasswordHashEnabled 设置账号凭据是否使用 bcrypt 存储。
// 该开关仅用于本地开发/调试；生产环境应保持启用。
func (s *UserService) SetPasswordHashEnabled(enabled bool) {
	s.passwordHashEnabled = enabled
}

// SetReliability connects account registration to the Core transactional
// command/outbox boundary without exposing a database handle to Identity.
func (s *UserService) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable != nil {
		if snapshotter, ok := s.repo.(transaction.Snapshotter); ok {
			reliable.RegisterMemorySnapshotters(snapshotter)
		}
	}
}

func (s *UserService) Register(_ context.Context, _ domain.CreateUserRequest) (*domain.User, error) {
	// Registration is intentionally no longer a bypass around the verification
	// command. Keep this method to make legacy in-process callers fail closed.
	return nil, ErrRegistrationVerificationRequired
}

// RegisterVerified creates a user and verified account only after consuming a
// registration Ticket in the same Reliability command and transaction.
func (s *UserService) RegisterVerified(ctx context.Context, req domain.RegistrationRequest) (*domain.User, error) {
	if s.registrationTickets == nil || s.pgRepo == nil {
		return nil, ErrRegistrationVerificationRequired
	}
	userRequest := req.UserRequest()
	userRequest.Email = domain.NormalizeEmail(userRequest.Email)
	if domain.IsReservedEmail(userRequest.Email) {
		return nil, fmt.Errorf("email is reserved for historical migration metadata")
	}
	credential := userRequest.Password
	if s.passwordHashEnabled {
		var err error
		credential, err = auth.HashPassword(userRequest.Password)
		if err != nil {
			return nil, fmt.Errorf("hash password: %w", err)
		}
	}

	now := time.Now().UTC()
	user := &domain.User{
		ID:          strconv.FormatInt(idgen.New(), 10),
		Username:    userRequest.Username,
		Nickname:    userRequest.Nickname,
		Email:       userRequest.Email,
		Status:      domain.UserStatusActive,
		AuthVersion: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	create := func(commandCtx context.Context) error {
		if _, err := s.registrationTickets.ConsumeTicketForCommand(commandCtx, domain.ChallengeTicketConsumption{
			PublicID: req.ChallengeID,
			Purpose:  domain.ChallengePurposeRegistration,
			Ticket:   req.Ticket,
			Email:    userRequest.Email,
		}); err != nil {
			if errors.Is(err, ErrChallengeTicket) {
				return ErrRegistrationTicketInvalid
			}
			return fmt.Errorf("consume registration ticket: %w", err)
		}
		if err := s.repo.Create(commandCtx, user); err != nil {
			if errors.Is(err, repository.ErrUsernameExists) {
				return fmt.Errorf("username '%s' already taken", userRequest.Username)
			}
			if errors.Is(err, repository.ErrEmailExists) {
				return fmt.Errorf("email '%s' already registered", userRequest.Email)
			}
			return fmt.Errorf("create user: %w", err)
		}
		if err := s.pgRepo.CreateVerifiedAccount(commandCtx, user.ID, userRequest.Email, credential); err != nil {
			if compensator, ok := s.repo.(registrationCompensator); ok {
				// This is redundant under PostgreSQL TxKernel, but it keeps a
				// failing local credential adapter deterministic in isolated tests.
				_ = compensator.DeleteForRegistration(commandCtx, user.ID)
			}
			return fmt.Errorf("create verified account: %w", err)
		}
		return nil
	}

	if s.reliable != nil {
		event, err := reliability.NewEvent(eventbus.EventUserCreated, "user", user.ID, user.Public())
		if err != nil {
			return nil, err
		}
		if err := s.reliable.Execute(ctx, reliability.Command{
			Code:           "identity.user.register",
			ResourceType:   "user",
			ResourceID:     user.ID,
			OperationCode:  "identity.user.register",
			IdempotencyKey: "identity.user.register:" + user.ID,
			Event:          &event,
		}, create); err != nil {
			return nil, err
		}
	} else {
		if err := create(ctx); err != nil {
			return nil, err
		}
		if s.bus != nil {
			_ = s.bus.Publish(ctx, eventbus.NewEvent(
				eventbus.EventUserCreated, "campusos.identity", "user."+user.ID, user.Public(),
			))
		}
	}

	return user, nil
}

// Authenticate validates credentials and account state, but deliberately does
// not mint any token. The SessionService owns v12 access/refresh issuance.
func (s *UserService) Authenticate(ctx context.Context, req domain.LoginRequest) (*domain.User, error) {
	req.Email = domain.NormalizeEmail(req.Email)
	// 先通过邮箱查找用户获取凭据
	var user *domain.User
	var err error

	if s.pgRepo == nil {
		return nil, fmt.Errorf("credential repository is unavailable")
	}
	userID, credential, err := s.pgRepo.GetCredentialByEmail(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("invalid email or password")
	}
	if !s.checkPassword(req.Password, credential) {
		return nil, fmt.Errorf("invalid email or password")
	}
	account, err := s.pgRepo.GetEmailAccount(ctx, userID)
	if err != nil || !loginAllowed(account.VerificationState) {
		return nil, fmt.Errorf("invalid email or password")
	}
	user, err = s.repo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	if user.Status != domain.UserStatusActive {
		return nil, fmt.Errorf("account is %s", user.Status)
	}
	if user.AuthVersion < 1 {
		return nil, fmt.Errorf("account authentication state is invalid")
	}
	return user, nil
}

// Login remains source compatible for narrow in-process callers while no
// longer creating stateless bearer material. HTTP handlers must call
// Authenticate followed by SessionService.Issue.
func (s *UserService) Login(ctx context.Context, req domain.LoginRequest) (*domain.User, string, string, error) {
	user, err := s.Authenticate(ctx, req)
	if err != nil {
		return nil, "", "", err
	}
	return user, "", "", nil
}

// GetEmailAccount is Identity's credential-free account-state query. It keeps
// the migration-era local memory profile usable without exposing repository
// internals to other modules.
func (s *UserService) GetEmailAccount(ctx context.Context, userID string) (*domain.EmailAccount, error) {
	if s.pgRepo != nil {
		return s.pgRepo.GetEmailAccount(ctx, userID)
	}
	user, err := s.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &domain.EmailAccount{
		ID:                   "memory:" + user.ID,
		UserID:               user.ID,
		IdentifierNormalized: domain.NormalizeEmail(user.Email),
		VerificationState:    domain.VerificationStateLegacyAccepted,
		CredentialVersion:    1,
	}, nil
}

func loginAllowed(state domain.VerificationState) bool {
	switch state {
	case domain.VerificationStateVerified, domain.VerificationStateLegacyAccepted, domain.VerificationStateSystemManaged:
		return true
	default:
		return false
	}
}

func (s *UserService) checkPassword(password, credential string) bool {
	if !s.passwordHashEnabled {
		return password == credential
	}
	return auth.CheckPassword(password, credential)
}

// GetUserRoles 获取用户角色列表
func (s *UserService) GetUserRoles(ctx context.Context, userID string) ([]domain.RoleInfo, error) {
	if s.roleRepo == nil {
		// 默认返回 member 角色
		return []domain.RoleInfo{{ID: 3, Name: "member", Description: "普通会员"}}, nil
	}
	roles, err := s.roleRepo.GetUserRoles(ctx, userID)
	if err != nil {
		return []domain.RoleInfo{{ID: 3, Name: "member", Description: "普通会员"}}, nil
	}
	if len(roles) == 0 {
		return []domain.RoleInfo{{ID: 3, Name: "member", Description: "普通会员"}}, nil
	}
	var result []domain.RoleInfo
	for _, r := range roles {
		result = append(result, domain.RoleInfo{ID: r.ID, Name: r.Name, Description: r.Description})
	}
	return result, nil
}

func (s *UserService) GetByID(ctx context.Context, id string) (*domain.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, fmt.Errorf("user '%s' not found", id)
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return user, nil
}

func (s *UserService) ListUsers(ctx context.Context, page, pageSize int) ([]*domain.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.repo.List(ctx, page, pageSize)
}

func (s *UserService) SuspendUser(ctx context.Context, id string) (*domain.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	user.Status = domain.UserStatusSuspended
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("suspend user: %w", err)
	}
	return user, nil
}

func (s *UserService) ActivateUser(ctx context.Context, id string) (*domain.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	user.Status = domain.UserStatusActive
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("activate user: %w", err)
	}
	return user, nil
}

func (s *UserService) UpdateUser(ctx context.Context, id string, req domain.UpdateUserRequest) (*domain.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if req.Nickname != nil {
		user.Nickname = *req.Nickname
	}
	if req.Bio != nil {
		user.Bio = *req.Bio
	}
	if req.Avatar != nil {
		user.Avatar = *req.Avatar
	}
	user.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	return user, nil
}
