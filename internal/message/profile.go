package message

import (
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

func BindPostgreSQLAdapter(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("message PostgreSQL pool is required")
	}
	return app.Provide(portStore, Store(NewPgStore(pool)))
}
func BindMemoryAdapter(app *platformmodule.AppContext) error {
	return app.Provide(portStore, Store(NewMemoryStore()))
}
