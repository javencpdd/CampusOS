package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/internal/core/identity/repository"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/google/uuid"
)

type UserService struct {
	repo   repository.UserRepository
	jwtMgr *auth.JWTManager
	pgRepo PgUserRepo
	bus    eventbus.EventBus
}

type PgUserRepo interface {
	CreateAccount(ctx context.Context, userID, email, hashedPassword string) error
	GetCredentialByEmail(ctx context.Context, email string) (string, string, error)
}

func NewUserService(repo repository.UserRepository, jwtMgr *auth.JWTManager, pgRepo PgUserRepo, bus eventbus.EventBus) *UserService {
	return &UserService{repo: repo, jwtMgr: jwtMgr, pgRepo: pgRepo, bus: bus}
}

func (s *UserService) Register(ctx context.Context, req domain.CreateUserRequest) (*domain.User, error) {
	// bcrypt 哈希密码
	hashedPwd, err := auth.HashPassword(req.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	userID := uuid.New().String()
	now := time.Now().UTC()
	user := &domain.User{
		ID:        userID,
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
		if err := s.pgRepo.CreateAccount(ctx, userID, req.Email, hashedPwd); err != nil {
			return nil, fmt.Errorf("create account: %w", err)
		}
	}

	// 发布 user.created 事件
	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(
			eventbus.EventUserCreated, "campusos.identity", "user."+userID, user,
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
		if !auth.CheckPassword(req.Password, credential) {
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
