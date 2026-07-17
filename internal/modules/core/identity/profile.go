package identity

import (
	"fmt"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	portUserRepository         = "identity.adapter.user-repository"
	portRoleRepository         = "identity.adapter.role-repository"
	portChallengeRepository    = "identity.adapter.challenge-repository"
	portSessionRepository      = "identity.adapter.session-repository"
	portRecoveryCaseRepository = "identity.adapter.recovery-case-repository"
)

// BindPostgreSQLAdapters binds only Identity's repository adapters. It is
// called by an Infrastructure Profile, never by a business Handler or Server.
func BindPostgreSQLAdapters(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("identity PostgreSQL pool is required")
	}
	return bindAdapters(
		app,
		repository.NewPgUserRepository(pool),
		repository.NewPgRoleRepository(pool),
		repository.NewPgChallengeRepository(pool),
		repository.NewPgSessionRepository(pool),
		repository.NewPgRecoveryCaseRepository(pool),
	)
}

// BindMemoryAdapters keeps the Memory profile behavior compatible with the
// existing local-development fallback.
func BindMemoryAdapters(app *platformmodule.AppContext) error {
	return bindAdapters(
		app,
		repository.NewMemoryUserRepository(),
		repository.NewMemoryRoleRepository(),
		repository.NewMemoryChallengeRepository(),
		repository.NewMemorySessionRepository(),
		repository.NewMemoryRecoveryCaseRepository(),
	)
}

func bindAdapters(
	app *platformmodule.AppContext,
	users repository.UserRepository,
	roles repository.RoleRepository,
	challenges repository.ChallengeRepository,
	sessions repository.SessionRepository,
	recoveryCases repository.RecoveryCaseRepository,
) error {
	if err := app.Provide(portUserRepository, users); err != nil {
		return err
	}
	if err := app.Provide(portRoleRepository, roles); err != nil {
		return err
	}
	if err := app.Provide(portChallengeRepository, challenges); err != nil {
		return err
	}
	if err := app.Provide(portSessionRepository, sessions); err != nil {
		return err
	}
	return app.Provide(portRecoveryCaseRepository, recoveryCases)
}
