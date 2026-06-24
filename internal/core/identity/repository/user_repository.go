package repository

import (
	"context"
	"errors"
	"sync"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
)

var (
	ErrUserNotFound   = errors.New("user not found")
	ErrEmailExists    = errors.New("email already exists")
	ErrUsernameExists = errors.New("username already exists")
)

// UserRepository 用户仓储接口
type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	GetByID(ctx context.Context, id string) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	Update(ctx context.Context, user *domain.User) error
	List(ctx context.Context, page, pageSize int) ([]*domain.User, int64, error)
}

// MemoryUserRepository 内存用户仓储（Demo 用）
type MemoryUserRepository struct {
	mu    sync.RWMutex
	users map[string]*domain.User
}

// NewMemoryUserRepository 创建内存用户仓储
func NewMemoryUserRepository() *MemoryUserRepository {
	return &MemoryUserRepository{
		users: make(map[string]*domain.User),
	}
}

func (r *MemoryUserRepository) Create(_ context.Context, user *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查用户名唯一性
	for _, u := range r.users {
		if u.Username == user.Username {
			return ErrUsernameExists
		}
		if u.Email == user.Email {
			return ErrEmailExists
		}
	}

	r.users[user.ID] = user
	return nil
}

func (r *MemoryUserRepository) GetByID(_ context.Context, id string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, ok := r.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (r *MemoryUserRepository) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, u := range r.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, ErrUserNotFound
}

func (r *MemoryUserRepository) GetByUsername(_ context.Context, username string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, u := range r.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, ErrUserNotFound
}

func (r *MemoryUserRepository) Update(_ context.Context, user *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.users[user.ID]; !ok {
		return ErrUserNotFound
	}
	r.users[user.ID] = user
	return nil
}

func (r *MemoryUserRepository) List(_ context.Context, page, pageSize int) ([]*domain.User, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := int64(len(r.users))

	// 简单分页
	all := make([]*domain.User, 0, len(r.users))
	for _, u := range r.users {
		all = append(all, u)
	}

	start := (page - 1) * pageSize
	if start >= len(all) {
		return []*domain.User{}, total, nil
	}

	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}

	return all[start:end], total, nil
}
