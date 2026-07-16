package port

import (
	"context"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
)

type RepositoryCategoryReader struct{ repository repository.CategoryRepository }

func NewRepositoryCategoryReader(value repository.CategoryRepository) *RepositoryCategoryReader {
	return &RepositoryCategoryReader{repository: value}
}
func (r *RepositoryCategoryReader) GetCategory(ctx context.Context, id string) (Category, error) {
	value, err := r.repository.GetByID(ctx, id)
	if err != nil {
		return Category{}, err
	}
	return Category{ID: value.ID, Name: value.Name}, nil
}

type RepositoryThreadPort struct{ repository repository.ThreadRepository }

func NewRepositoryThreadPort(value repository.ThreadRepository) *RepositoryThreadPort {
	return &RepositoryThreadPort{repository: value}
}
func (r *RepositoryThreadPort) GetThread(ctx context.Context, id string) (Thread, error) {
	value, err := r.repository.GetByID(ctx, id)
	if err != nil {
		return Thread{}, err
	}
	return Thread{ID: value.ID, CategoryID: value.CategoryID, AuthorID: value.AuthorID, Status: string(value.Status)}, nil
}
func (r *RepositoryThreadPort) SetThreadStatus(ctx context.Context, id, status string) error {
	value, err := r.repository.GetByID(ctx, id)
	if err != nil {
		return err
	}
	value.Status = domain.ThreadStatus(status)
	return r.repository.Update(ctx, value)
}

type RepositoryPostPort struct{ repository repository.PostRepository }

func NewRepositoryPostPort(value repository.PostRepository) *RepositoryPostPort {
	return &RepositoryPostPort{repository: value}
}
func (r *RepositoryPostPort) GetPost(ctx context.Context, id string) (Post, error) {
	value, err := r.repository.GetByID(ctx, id)
	if err != nil {
		return Post{}, err
	}
	return Post{ID: value.ID, ThreadID: value.ThreadID, AuthorID: value.AuthorID, Status: string(value.Status)}, nil
}
func (r *RepositoryPostPort) SetPostStatus(ctx context.Context, id, status string) error {
	value, err := r.repository.GetByID(ctx, id)
	if err != nil {
		return err
	}
	value.Status = status
	return r.repository.Update(ctx, value)
}
