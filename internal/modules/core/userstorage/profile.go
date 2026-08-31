package storage

import (
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	portQuotaRepository  = "storage.adapter.quota-repository"
	portObjectRepository = "storage.adapter.object-repository"
)

func BindPostgreSQLAdapter(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("user storage PostgreSQL pool is required")
	}
	if err := app.Provide(portQuotaRepository, QuotaRepository(NewPgQuotaRepository(pool))); err != nil {
		return err
	}
	return app.Provide(portObjectRepository, ObjectRepository(NewPgObjectRepository(pool)))
}

func BindMemoryAdapter(app *platformmodule.AppContext) error {
	if err := app.Provide(portQuotaRepository, QuotaRepository(NewMemoryQuotaRepository())); err != nil {
		return err
	}
	return app.Provide(portObjectRepository, ObjectRepository(NewMemoryObjectRepository()))
}
