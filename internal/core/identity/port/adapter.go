package port

import (
	"context"
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
	return User{ID: value.ID, Username: value.Username, Status: string(value.Status)}, nil
}
