package ai

import (
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

func BindPostgreSQLAdapter(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("AI PostgreSQL pool is required")
	}
	return app.Provide(portCallLogStore, CallLogStore(NewPgCallLogger(pool)))
}

func BindMemoryAdapter(app *platformmodule.AppContext) error {
	return app.Provide(portCallLogStore, CallLogStore(NewMemoryCallLogger()))
}
