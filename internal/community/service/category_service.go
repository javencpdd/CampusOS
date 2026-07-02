package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/repository"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
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
	id := strconv.FormatInt(idgen.New(), 10)
	name := strings.TrimSpace(req.Name)
	slug := normalizeCategorySlug(req.Slug)
	if slug == "" {
		slug = fallbackCategorySlug(name, id)
	}
	cat := &domain.Category{
		ID:          id,
		Name:        name,
		Slug:        slug,
		Description: strings.TrimSpace(req.Description),
		Icon:        strings.TrimSpace(req.Icon),
		ParentID:    req.ParentID,
		SortOrder:   req.SortOrder,
		IsClosed:    req.IsClosed,
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
		cat.Name = strings.TrimSpace(*req.Name)
	}
	if req.Slug != nil {
		slug := normalizeCategorySlug(*req.Slug)
		if slug != "" {
			cat.Slug = slug
		}
	}
	if req.Description != nil {
		cat.Description = strings.TrimSpace(*req.Description)
	}
	if req.Icon != nil {
		cat.Icon = strings.TrimSpace(*req.Icon)
	}
	if req.ParentID != nil {
		cat.ParentID = req.ParentID
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

func fallbackCategorySlug(name, id string) string {
	slug := normalizeCategorySlug(name)
	if slug != "" {
		return slug
	}
	return "category-" + id
}

func normalizeCategorySlug(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	var b strings.Builder
	prevDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}

	slug := strings.Trim(b.String(), "-")
	if len(slug) > 64 {
		slug = strings.TrimRight(slug[:64], "-")
	}
	return slug
}
