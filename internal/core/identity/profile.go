package identity

import (
	"fmt"

	"github.com/campusos/CampusOS/internal/core/identity/repository"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	portUserRepository = "identity.adapter.user-repository"
	portRoleRepository = "identity.adapter.role-repository"
)

// BindPostgreSQLAdapters binds only Identity's repository adapters. It is
// called by an Infrastructure Profile, never by a business Handler or Server.
func BindPostgreSQLAdapters(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("identity PostgreSQL pool is required")
	}
	return bindAdapters(app, repository.NewPgUserRepository(pool), repository.NewPgRoleRepository(pool))
}

// BindMemoryAdapters keeps the Memory profile behavior compatible with the
// existing local-development fallback.
func BindMemoryAdapters(app *platformmodule.AppContext) error {
	return bindAdapters(app, repository.NewMemoryUserRepository(), repository.NewMemoryRoleRepository())
}

func bindAdapters(app *platformmodule.AppContext, users repository.UserRepository, roles repository.RoleRepository) error {
	if err := app.Provide(portUserRepository, users); err != nil {
		return err
	}
	return app.Provide(portRoleRepository, roles)
}
