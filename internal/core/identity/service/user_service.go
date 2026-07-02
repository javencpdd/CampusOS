package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/internal/core/identity/repository"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

type UserService struct {
	repo     repository.UserRepository
	jwtMgr   *auth.JWTManager
	pgRepo   PgUserRepo
	roleRepo RoleQuerier
	bus      eventbus.EventBus

	passwordHashEnabled bool
}

type PgUserRepo interface {
	CreateAccount(ctx context.Context, userID, email, hashedPassword string) error
	GetCredentialByEmail(ctx context.Context, email string) (string, string, error)
}

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

// SetPasswordHashEnabled 设置账号凭据是否使用 bcrypt 存储。
// 该开关仅用于本地开发/调试；生产环境应保持启用。
func (s *UserService) SetPasswordHashEnabled(enabled bool) {
	s.passwordHashEnabled = enabled
}

func (s *UserService) Register(ctx context.Context, req domain.CreateUserRequest) (*domain.User, error) {
	credential := req.Password
	if s.passwordHashEnabled {
		var err error
		credential, err = auth.HashPassword(req.Password)
		if err != nil {
			return nil, fmt.Errorf("hash password: %w", err)
		}
	}

	now := time.Now().UTC()
	user := &domain.User{
		ID:        strconv.FormatInt(idgen.New(), 10),
		Username:  req.Username,
		Nickname:  req.Nickname,
		Email:     req.Email,
		Status:    domain.UserStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.Create(ctx, user); err != nil {
		if errors.Is(err, repository.ErrUsernameExists) {
			return nil, fmt.Errorf("username '%s' already taken", req.Username)
		}
		if errors.Is(err, repository.ErrEmailExists) {
			return nil, fmt.Errorf("email '%s' already registered", req.Email)
		}
		return nil, fmt.Errorf("create user: %w", err)
	}

	// 保存账号凭据
	if s.pgRepo != nil {
		if err := s.pgRepo.CreateAccount(ctx, user.ID, req.Email, credential); err != nil {
			return nil, fmt.Errorf("create account: %w", err)
		}
	}

	// 发布 user.created 事件
	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(
			eventbus.EventUserCreated, "campusos.identity", "user."+user.ID, user,
		))
	}

	return user, nil
}

func (s *UserService) Login(ctx context.Context, req domain.LoginRequest) (*domain.User, string, string, error) {
	// 先通过邮箱查找用户获取凭据
	var user *domain.User
	var err error

	if s.pgRepo != nil {
		// PostgreSQL 模式：验证密码
		userID, credential, err := s.pgRepo.GetCredentialByEmail(ctx, req.Email)
		if err != nil {
			return nil, "", "", fmt.Errorf("invalid email or password")
		}
		if !s.checkPassword(req.Password, credential) {
			return nil, "", "", fmt.Errorf("invalid email or password")
		}
		user, err = s.repo.GetByID(ctx, userID)
		if err != nil {
			return nil, "", "", fmt.Errorf("get user: %w", err)
		}
	} else {
		// 内存模式：简化验证
		user, err = s.repo.GetByEmail(ctx, req.Email)
		if err != nil {
			return nil, "", "", fmt.Errorf("invalid email or password")
		}
	}

	if user.Status != domain.UserStatusActive {
		return nil, "", "", fmt.Errorf("account is %s", user.Status)
	}

	// 生成 JWT Token
	accessToken, err := s.jwtMgr.GenerateAccessToken(user.ID, user.Username)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate access token: %w", err)
	}
	refreshToken, err := s.jwtMgr.GenerateRefreshToken(user.ID)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	return user, accessToken, refreshToken, nil
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
