package moderation

import (
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

const portAuditStore = "moderation.adapter.audit-store"

func BindPostgreSQLAdapters(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("moderation PostgreSQL pool is required")
	}
	return app.Provide(portAuditStore, AuditStore(NewPgAuditStore(pool)))
}

func BindMemoryAdapters(app *platformmodule.AppContext) error {
	return app.Provide(portAuditStore, AuditStore(NewMemoryAuditStore()))
}
