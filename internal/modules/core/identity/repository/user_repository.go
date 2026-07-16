package repository

import (
	"context"
	"errors"
	"sync"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
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

	r.users[user.ID] = cloneUser(user)
	return nil
}

func (r *MemoryUserRepository) GetByID(_ context.Context, id string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, ok := r.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return cloneUser(user), nil
}

func (r *MemoryUserRepository) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, u := range r.users {
		if u.Email == email {
			return cloneUser(u), nil
		}
	}
	return nil, ErrUserNotFound
}

func (r *MemoryUserRepository) GetByUsername(_ context.Context, username string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, u := range r.users {
		if u.Username == username {
			return cloneUser(u), nil
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
	r.users[user.ID] = cloneUser(user)
	return nil
}

func (r *MemoryUserRepository) List(_ context.Context, page, pageSize int) ([]*domain.User, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := int64(len(r.users))

	// 简单分页
	all := make([]*domain.User, 0, len(r.users))
	for _, u := range r.users {
		all = append(all, cloneUser(u))
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

// DeleteForRegistration is deliberately narrow: it only supports compensating
// a just-created user when a non-transactional local test adapter fails while
// creating the associated account. Production PostgreSQL uses TxKernel.
func (r *MemoryUserRepository) DeleteForRegistration(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.users[id]; !exists {
		return ErrUserNotFound
	}
	delete(r.users, id)
	return nil
}

func (r *MemoryUserRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make(map[string]*domain.User, len(r.users))
	for id, user := range r.users {
		items[id] = cloneUser(user)
	}
	return items
}

func (r *MemoryUserRepository) Restore(value any) {
	items, ok := value.(map[string]*domain.User)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users = make(map[string]*domain.User, len(items))
	for id, user := range items {
		r.users[id] = cloneUser(user)
	}
}

func cloneUser(user *domain.User) *domain.User {
	if user == nil {
		return nil
	}
	copyUser := *user
	return &copyUser
}
