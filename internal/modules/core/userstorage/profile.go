package storage

import (
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

const portQuotaRepository = "storage.adapter.quota-repository"

func BindPostgreSQLAdapter(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("user storage PostgreSQL pool is required")
	}
	return app.Provide(portQuotaRepository, QuotaRepository(NewPgQuotaRepository(pool)))
}

func BindMemoryAdapter(app *platformmodule.AppContext) error {
	return app.Provide(portQuotaRepository, QuotaRepository(NewMemoryQuotaRepository()))
}
