package community

import (
	"fmt"

	"github.com/campusos/CampusOS/internal/community/repository"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	portThreadRepository   = "community.adapter.thread-repository"
	portCategoryRepository = "community.adapter.category-repository"
	portPostRepository     = "community.adapter.post-repository"
)

func BindPostgreSQLAdapters(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("community PostgreSQL pool is required")
	}
	return bindAdapters(app, repository.NewPgThreadRepository(pool), repository.NewPgCategoryRepository(pool), repository.NewPgPostRepository(pool))
}

func BindMemoryAdapters(app *platformmodule.AppContext) error {
	return bindAdapters(app, repository.NewMemoryThreadRepository(), repository.NewMemoryCategoryRepository(), repository.NewMemoryPostRepository())
}

func bindAdapters(app *platformmodule.AppContext, threads repository.ThreadRepository, categories repository.CategoryRepository, posts repository.PostRepository) error {
	for _, binding := range []struct {
		name  string
		value interface{}
	}{
		{portThreadRepository, threads},
		{portCategoryRepository, categories},
		{portPostRepository, posts},
	} {
		if err := app.Provide(binding.name, binding.value); err != nil {
			return err
		}
	}
	return nil
}
