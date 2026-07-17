package secondhand

import (
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

func BindPostgreSQLAdapter(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if app == nil || pool == nil {
		return fmt.Errorf("secondhand PostgreSQL adapter requires app context and pool")
	}
	return app.Provide(portStore, Store(NewPgStore(pool)))
}

func BindMemoryAdapter(app *platformmodule.AppContext) error {
	if app == nil {
		return fmt.Errorf("secondhand memory adapter requires app context")
	}
	return app.Provide(portStore, Store(NewMemoryStore()))
}
