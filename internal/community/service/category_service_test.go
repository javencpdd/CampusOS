package service

import (
	"context"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/repository"
)

func TestCreateCategoryGeneratesSlugFromEnglishName(t *testing.T) {
	svc := NewCategoryService(repository.NewMemoryCategoryRepository(), nil)

	category, err := svc.Create(context.Background(), domain.CreateCategoryRequest{
		Name:        "Campus News",
		Description: "News and notices",
		Icon:        "N",
		SortOrder:   3,
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	if category.Slug != "campus-news" {
		t.Fatalf("expected generated slug campus-news, got %q", category.Slug)
	}
	if category.Icon != "N" || category.SortOrder != 3 {
		t.Fatalf("expected icon and sort order to be preserved, got %#v", category)
	}
}

func TestCreateCategoryGeneratesFallbackSlugForChineseName(t *testing.T) {
	svc := NewCategoryService(repository.NewMemoryCategoryRepository(), nil)

	category, err := svc.Create(context.Background(), domain.CreateCategoryRequest{
		Name: "校园公告",
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	if !strings.HasPrefix(category.Slug, "category-") {
		t.Fatalf("expected category-id fallback slug, got %q", category.Slug)
	}
}

func TestCreateCategoryKeepsProvidedSlugNormalized(t *testing.T) {
	svc := NewCategoryService(repository.NewMemoryCategoryRepository(), nil)

	category, err := svc.Create(context.Background(), domain.CreateCategoryRequest{
		Name: "Any Name",
		Slug: "  Custom Slug_01  ",
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	if category.Slug != "custom-slug-01" {
		t.Fatalf("expected normalized slug custom-slug-01, got %q", category.Slug)
	}
}
