package schedule

import (
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

const portTermReferences = "schedule.adapter.term-references"

func BindPostgreSQLAdapter(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("schedule PostgreSQL pool is required")
	}
	return app.Provide(portTermReferences, TermReferenceRepository(NewPgTermReferenceRepository(pool)))
}

func BindMemoryAdapter(app *platformmodule.AppContext) error {
	return app.Provide(portTermReferences, TermReferenceRepository(NewMemoryTermReferenceRepository()))
}
