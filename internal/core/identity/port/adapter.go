package port

import (
	"context"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/internal/core/identity/repository"
)

type RepositoryUserReader struct{ repository repository.UserRepository }

func NewRepositoryUserReader(repository repository.UserRepository) *RepositoryUserReader {
	return &RepositoryUserReader{repository: repository}
}
func (r *RepositoryUserReader) GetUser(ctx context.Context, id string) (User, error) {
	value, err := r.repository.GetByID(ctx, id)
	if err != nil {
		return User{}, err
	}
	return userProjection(value), nil
}

func (r *RepositoryUserReader) GetUserByUsername(ctx context.Context, username string) (User, error) {
	value, err := r.repository.GetByUsername(ctx, username)
	if err != nil {
		return User{}, err
	}
	return userProjection(value), nil
}

func userProjection(value *domain.User) User {
	if value == nil {
		return User{}
	}
	return User{
		ID:       value.ID,
		Username: value.Username,
		Nickname: value.Nickname,
		Email:    value.Email,
		Avatar:   value.Avatar,
		Bio:      value.Bio,
		Status:   string(value.Status),
	}
}
