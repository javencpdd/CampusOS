package community

import (
	"fmt"

	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	portThreadRepository     = "community.adapter.thread-repository"
	portCategoryRepository   = "community.adapter.category-repository"
	portPostRepository       = "community.adapter.post-repository"
	portGovernanceRepository = "community.adapter.governance-repository"
	portThreadTypePolicyRepo = "community.adapter.thread-type-policy-repository"
	portNotificationRepo     = "community.adapter.notification-repository"
)

func BindPostgreSQLAdapters(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("community PostgreSQL pool is required")
	}
	return bindAdapters(app, repository.NewPgThreadRepository(pool), repository.NewPgCategoryRepository(pool), repository.NewPgPostRepository(pool), repository.NewPgContentGovernanceRepository(pool), repository.NewPgThreadTypePolicyRepository(pool), repository.NewPgNotificationRepository(pool))
}

func BindMemoryAdapters(app *platformmodule.AppContext) error {
	return bindAdapters(app, repository.NewMemoryThreadRepository(), repository.NewMemoryCategoryRepository(), repository.NewMemoryPostRepository(), repository.NewMemoryContentGovernanceRepository(), repository.NewMemoryThreadTypePolicyRepository(), repository.NewMemoryNotificationRepository())
}

func bindAdapters(app *platformmodule.AppContext, threads repository.ThreadRepository, categories repository.CategoryRepository, posts repository.PostRepository, governance repository.ContentGovernanceRepository, typePolicies repository.ThreadTypePolicyRepository, notifications repository.NotificationRepository) error {
	for _, binding := range []struct {
		name  string
		value interface{}
	}{
		{portThreadRepository, threads},
		{portCategoryRepository, categories},
		{portPostRepository, posts},
		{portGovernanceRepository, governance},
		{portThreadTypePolicyRepo, typePolicies},
		{portNotificationRepo, notifications},
	} {
		if err := app.Provide(binding.name, binding.value); err != nil {
			return err
		}
	}
	return nil
}
