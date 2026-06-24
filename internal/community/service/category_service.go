package service

import (
	"context"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/repository"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/google/uuid"
)

type CategoryService struct {
	repo repository.CategoryRepository
	bus  eventbus.EventBus
}

func NewCategoryService(repo repository.CategoryRepository, bus eventbus.EventBus) *CategoryService {
	return &CategoryService{repo: repo, bus: bus}
}

func (s *CategoryService) Create(ctx context.Context, req domain.CreateCategoryRequest) (*domain.Category, error) {
	now := time.Now().UTC()
	cat := &domain.Category{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		ParentID:    req.ParentID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.Create(ctx, cat); err != nil {
		return nil, fmt.Errorf("create category: %w", err)
	}

	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(
			eventbus.EventCategoryCreated, "campusos.community", "category."+cat.ID, cat,
		))
	}

	return cat, nil
}

func (s *CategoryService) GetByID(ctx context.Context, id string) (*domain.Category, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *CategoryService) List(ctx context.Context) ([]*domain.Category, error) {
	return s.repo.List(ctx)
}

func (s *CategoryService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func (s *CategoryService) Update(ctx context.Context, id string, req domain.UpdateCategoryRequest) (*domain.Category, error) {
	cat, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		cat.Name = *req.Name
	}
	if req.Description != nil {
		cat.Description = *req.Description
	}
	if req.IsClosed != nil {
		cat.IsClosed = *req.IsClosed
	}
	if req.SortOrder != nil {
		cat.SortOrder = *req.SortOrder
	}
	cat.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, cat); err != nil {
		return nil, err
	}
	return cat, nil
}
