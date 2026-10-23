package identity

import (
	"context"
	"fmt"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	portUserRepository            = "identity.adapter.user-repository"
	portRoleRepository            = "identity.adapter.role-repository"
	portChallengeRepository       = "identity.adapter.challenge-repository"
	portChallengePolicyRepository = "identity.adapter.challenge-policy-repository"
	portSessionRepository         = "identity.adapter.session-repository"
	portRecoveryCaseRepository    = "identity.adapter.recovery-case-repository"
	portAdminAccountRepository    = "identity.adapter.admin-account-repository"
	portMFARepository             = "identity.adapter.mfa-repository"
	portDelegationRepository      = "identity.adapter.delegation-repository"
	portAdminSessionRepository    = "identity.adapter.admin-session-repository"
	portAdminSecurityRepository   = "identity.adapter.admin-security-repository"
	portAdminDomainRepository     = "identity.adapter.admin-domain-repository"
	portIntegrationRepository     = "identity.adapter.integration-repository"
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
		repository.NewPgAdminAccountRepository(pool),
		repository.NewPgChallengeRepository(pool),
		repository.NewPgChallengePolicyRepository(pool),
		repository.NewPgSessionRepository(pool),
		repository.NewPgRecoveryCaseRepository(pool),
		repository.NewPgMFARepository(pool),
		repository.NewPgDelegationRepository(pool),
		repository.NewPgAdminSessionRepository(pool),
		repository.NewPgAdminSecurityRepository(pool),
		repository.NewPgAdminDomainRepository(pool),
		repository.NewPgIntegrationRepository(pool),
	)
}

// BindMemoryAdapters keeps the Memory profile behavior compatible with the
// existing local-development fallback.
func BindMemoryAdapters(app *platformmodule.AppContext) error {
	adminAccounts := repository.NewMemoryAdminAccountRepository()
	adminDomains := repository.NewMemoryAdminDomainRepository()
	if err := adminDomains.ReplaceAdminRolePermissionFacts(context.Background(), "security_admin", []string{"identity.integration.read", "identity.integration.manage", "knowledge.collection.read", "knowledge.collection.manage"}, "system:memory-profile"); err != nil {
		return err
	}
	return bindAdapters(
		app,
		repository.NewMemoryUserRepository(),
		repository.NewMemoryRoleRepository(),
		adminAccounts,
		repository.NewMemoryChallengeRepository(),
		repository.NewMemoryChallengePolicyRepository(),
		repository.NewMemorySessionRepository(),
		repository.NewMemoryRecoveryCaseRepository(),
		repository.NewMemoryMFARepository(),
		repository.NewMemoryDelegationRepository(),
		repository.NewMemoryAdminSessionRepository(),
		repository.NewMemoryAdminSecurityRepository(),
		adminDomains,
		repository.NewMemoryIntegrationRepository(),
	)
}

func bindAdapters(
	app *platformmodule.AppContext,
	users repository.UserRepository,
	roles repository.RoleRepository,
	adminAccounts repository.AdminAccountRepository,
	challenges repository.ChallengeRepository,
	challengePolicies repository.ChallengePolicyRepository,
	sessions repository.SessionRepository,
	recoveryCases repository.RecoveryCaseRepository,
	mfa repository.MFARepository,
	delegations repository.DelegationRepository,
	adminSessions repository.AdminSessionRepository,
	adminSecurity repository.AdminSecurityRepository,
	adminDomains repository.AdminDomainRepository,
	integrations repository.IntegrationRepository,
) error {
	if err := app.Provide(portIntegrationRepository, integrations); err != nil {
		return err
	}
	if err := app.Provide(portUserRepository, users); err != nil {
		return err
	}
	if err := app.Provide(portRoleRepository, roles); err != nil {
		return err
	}
	if err := app.Provide(portAdminAccountRepository, adminAccounts); err != nil {
		return err
	}
	if err := app.Provide(portChallengeRepository, challenges); err != nil {
		return err
	}
	if err := app.Provide(portChallengePolicyRepository, challengePolicies); err != nil {
		return err
	}
	if err := app.Provide(portSessionRepository, sessions); err != nil {
		return err
	}
	if err := app.Provide(portRecoveryCaseRepository, recoveryCases); err != nil {
		return err
	}
	if err := app.Provide(portMFARepository, mfa); err != nil {
		return err
	}
	if err := app.Provide(portDelegationRepository, delegations); err != nil {
		return err
	}
	if err := app.Provide(portAdminSessionRepository, adminSessions); err != nil {
		return err
	}
	if err := app.Provide(portAdminSecurityRepository, adminSecurity); err != nil {
		return err
	}
	return app.Provide(portAdminDomainRepository, adminDomains)
}
