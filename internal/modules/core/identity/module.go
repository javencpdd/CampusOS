package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/campusos/CampusOS/internal/modules/core/identity/handler"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	platformroute "github.com/campusos/CampusOS/internal/platform/route"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

const (
	ModuleID                    = "core.identity"
	portEventBus                = "platform.event-bus"
	portUserReader              = "identity.user-reader"
	portAccountReader           = "identity.account-reader"
	portChallengeDispatchReader = "identity.challenge-dispatch-reader"
	portSessionVerifier         = "identity.session-verifier"
	portAuthorization           = "identity.authorization"
	portAdminAccess             = "identity.admin-access"
	portModeration              = "identity.moderation-policy"
)

type Config struct {
	JWT                   *auth.JWTManager
	PasswordHashEnabled   bool
	ChallengeActiveKeyID  string
	ChallengeHMACKeys     map[string]string
	ChallengeIPHashSecret string
	SessionIPHashSecret   string
	RefreshBodyCompat     bool
	CookieSecure          bool
}

type HTTPHandlers struct {
	User            *handler.UserHandler
	Role            *handler.RoleHandler
	ChallengePolicy *handler.ChallengePolicyHandler
}

// Module owns Identity application composition. Its Profile adapters are
// supplied before lifecycle registration; other modules consume public ports.
type Module struct {
	config            Config
	app               *platformmodule.AppContext
	users             repository.UserRepository
	roles             repository.RoleRepository
	adminAccounts     repository.AdminAccountRepository
	challenges        repository.ChallengeRepository
	challengePolicies repository.ChallengePolicyRepository
	sessions          repository.SessionRepository
	recoveryCases     repository.RecoveryCaseRepository

	userService            *service.UserService
	challengeService       *service.ChallengeService
	challengePolicyService *service.ChallengePolicyService
	sessionService         *service.SessionService
	recoveryService        *service.RecoveryService
	permissionService      *service.PermissionService
	adminAccessService     *service.AdminAccessService
	handlers               HTTPHandlers
}

func NewModule(config Config) *Module {
	return &Module{config: config}
}

func (m *Module) ID() string { return ModuleID }

func (m *Module) Dependencies() []string { return []string{"core.event-bus", reliability.ModuleID} }

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("identity module app context is required")
	}
	if m.config.JWT == nil {
		return errors.New("identity module JWT manager is required")
	}
	users, ok := app.Lookup(portUserRepository)
	if !ok {
		return errors.New("identity user repository adapter is not bound by profile")
	}
	roles, ok := app.Lookup(portRoleRepository)
	if !ok {
		return errors.New("identity role repository adapter is not bound by profile")
	}
	userRepository, ok := users.(repository.UserRepository)
	if !ok {
		return fmt.Errorf("identity user repository adapter has incompatible type %T", users)
	}
	roleRepository, ok := roles.(repository.RoleRepository)
	if !ok {
		return fmt.Errorf("identity role repository adapter has incompatible type %T", roles)
	}
	adminAccountValue, ok := app.Lookup(portAdminAccountRepository)
	if !ok {
		return errors.New("identity administrator account repository adapter is not bound by profile")
	}
	adminAccountRepository, ok := adminAccountValue.(repository.AdminAccountRepository)
	if !ok {
		return fmt.Errorf("identity administrator account repository adapter has incompatible type %T", adminAccountValue)
	}
	challengeValue, ok := app.Lookup(portChallengeRepository)
	if !ok {
		return errors.New("identity challenge repository adapter is not bound by profile")
	}
	challengeRepository, ok := challengeValue.(repository.ChallengeRepository)
	if !ok {
		return fmt.Errorf("identity challenge repository adapter has incompatible type %T", challengeValue)
	}
	challengePolicyValue, ok := app.Lookup(portChallengePolicyRepository)
	if !ok {
		return errors.New("identity challenge policy repository adapter is not bound by profile")
	}
	challengePolicyRepository, ok := challengePolicyValue.(repository.ChallengePolicyRepository)
	if !ok {
		return fmt.Errorf("identity challenge policy repository adapter has incompatible type %T", challengePolicyValue)
	}
	sessionValue, ok := app.Lookup(portSessionRepository)
	if !ok {
		return errors.New("identity session repository adapter is not bound by profile")
	}
	sessionRepository, ok := sessionValue.(repository.SessionRepository)
	if !ok {
		return fmt.Errorf("identity session repository adapter has incompatible type %T", sessionValue)
	}
	recoveryValue, ok := app.Lookup(portRecoveryCaseRepository)
	if !ok {
		return errors.New("identity recovery case repository adapter is not bound by profile")
	}
	recoveryCases, ok := recoveryValue.(repository.RecoveryCaseRepository)
	if !ok {
		return fmt.Errorf("identity recovery case repository adapter has incompatible type %T", recoveryValue)
	}
	m.app = app
	m.users = userRepository
	m.roles = roleRepository
	m.adminAccounts = adminAccountRepository
	m.challenges = challengeRepository
	m.challengePolicies = challengePolicyRepository
	m.sessions = sessionRepository
	m.recoveryCases = recoveryCases
	m.permissionService = service.NewPermissionService(m.roles, m.users)
	m.permissionService.SetAdminAccountRepository(m.adminAccounts)
	m.adminAccessService = service.NewAdminAccessService(m.adminAccounts)
	if err := app.Provide(portUserReader, identityport.NewRepositoryUserReader(userRepository)); err != nil {
		return err
	}
	return app.Provide(portModeration, identityport.ModerationPolicy(identityport.NewPermissionModerationPolicy(m.permissionService)))
}

func (m *Module) Start(context.Context) error {
	if m.app == nil || m.users == nil || m.roles == nil || m.adminAccounts == nil || m.challenges == nil || m.challengePolicies == nil || m.sessions == nil || m.recoveryCases == nil {
		return errors.New("identity module is not registered")
	}
	value, ok := m.app.Lookup(portEventBus)
	if !ok {
		return errors.New("identity module event bus port is unavailable")
	}
	bus, ok := value.(eventbus.EventBus)
	if !ok || bus == nil {
		return fmt.Errorf("identity event bus port has incompatible type %T", value)
	}
	// The registry dependency guarantees this port in a full server. Keep
	// standalone module tests and legacy embedders compatible until they adopt
	// the Core reliability module.
	var reliable *reliability.Service
	if reliabilityValue, exists := m.app.Lookup("platform.reliability.service"); exists {
		var compatible bool
		reliable, compatible = reliabilityValue.(*reliability.Service)
		if !compatible || reliable == nil {
			return fmt.Errorf("identity reliability port has incompatible type %T", reliabilityValue)
		}
	}
	permissions := m.permissionService
	var credentials service.PgUserRepo
	if adapter, ok := m.users.(service.PgUserRepo); ok {
		credentials = adapter
	}
	users := service.NewUserService(m.users, m.config.JWT, credentials, bus)
	users.SetPasswordHashEnabled(m.config.PasswordHashEnabled)
	users.SetRoleRepository(m.roles)
	challengePolicies, err := service.NewChallengePolicyService(m.challengePolicies)
	if err != nil {
		return fmt.Errorf("initialize identity challenge policy service: %w", err)
	}
	challenges, err := service.NewChallengeService(m.challenges, service.ChallengeConfig{
		ActiveKeyID:  m.config.ChallengeActiveKeyID,
		HMACKeys:     m.config.ChallengeHMACKeys,
		IPHashSecret: m.config.ChallengeIPHashSecret,
	})
	if err != nil {
		return fmt.Errorf("initialize identity challenge service: %w", err)
	}
	challenges.SetPolicyReader(challengePolicies)
	sessionHashSecret := m.config.SessionIPHashSecret
	if sessionHashSecret == "" {
		// Explicit config is always supplied by production bootstrap. This fallback
		// keeps older standalone module tests usable without broadening production.
		sessionHashSecret = m.config.ChallengeIPHashSecret
	}
	sessions, err := service.NewSessionService(m.sessions, m.users, m.config.JWT, service.SessionConfig{IPHashSecret: sessionHashSecret})
	if err != nil {
		return fmt.Errorf("initialize identity session service: %w", err)
	}
	recovery, err := service.NewRecoveryService(m.users, m.sessions, challenges, m.recoveryCases, service.RecoveryConfig{
		PasswordHashEnabled: m.config.PasswordHashEnabled,
	})
	if err != nil {
		return fmt.Errorf("initialize identity recovery service: %w", err)
	}
	if reliable != nil {
		users.SetReliability(reliable)
		permissions.SetReliability(reliable)
		challengePolicies.SetReliability(reliable)
		challenges.SetReliability(reliable)
		sessions.SetReliability(reliable)
		recovery.SetReliability(reliable)
	}
	m.userService = users
	m.challengeService = challenges
	m.challengePolicyService = challengePolicies
	m.sessionService = sessions
	m.recoveryService = recovery
	users.SetRegistrationTicketConsumer(challenges)
	userHandler := handler.NewUserHandler(users)
	userHandler.SetChallengeService(challenges)
	userHandler.SetSessionService(sessions, handler.SessionHTTPConfig{RefreshBodyCompat: m.config.RefreshBodyCompat, CookieSecure: m.config.CookieSecure})
	userHandler.SetRecoveryService(recovery)
	userHandler.SetAdminAccessService(m.adminAccessService)
	m.handlers = HTTPHandlers{
		User:            userHandler,
		Role:            handler.NewRoleHandler(permissions),
		ChallengePolicy: handler.NewChallengePolicyHandler(challengePolicies),
	}
	if err := m.app.Provide(portAccountReader, identityport.AccountReader(identityport.NewServiceAccountReader(users))); err != nil {
		return err
	}
	if err := m.app.Provide(portChallengeDispatchReader, identityport.ChallengeDispatchReader(identityport.NewServiceChallengeDispatchReader(challenges))); err != nil {
		return err
	}
	if err := m.app.Provide(portSessionVerifier, identityport.SessionVerifier(identityport.NewServiceSessionVerifier(sessions))); err != nil {
		return err
	}
	if err := m.app.Provide(portAuthorization, identityport.Authorization(permissions)); err != nil {
		return err
	}
	if err := m.app.Provide(portAdminAccess, m.adminAccessService); err != nil {
		return err
	}
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.userService == nil || m.challengeService == nil || m.challengePolicyService == nil || m.sessionService == nil || m.recoveryService == nil || m.permissionService == nil || m.adminAccessService == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "identity services are not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) UserRepository() repository.UserRepository { return m.users }

func (m *Module) Permissions() *service.PermissionService { return m.permissionService }

func (m *Module) AdminAccess() *service.AdminAccessService { return m.adminAccessService }

func (m *Module) Challenges() *service.ChallengeService { return m.challengeService }

func (m *Module) ChallengePolicies() *service.ChallengePolicyService { return m.challengePolicyService }

func (m *Module) Sessions() *service.SessionService { return m.sessionService }

func (m *Module) Recovery() *service.RecoveryService { return m.recoveryService }

func (m *Module) Handlers() HTTPHandlers { return m.handlers }

func (m *Module) JWTManager() *auth.JWTManager { return m.config.JWT }

// SyncRouteDescriptors keeps transport descriptors at the Identity boundary.
// Server composition supplies public route metadata only; conversion to the
// authorization repository model remains owned by Identity.
func (m *Module) SyncRouteDescriptors(ctx context.Context, descriptors []platformroute.Descriptor) error {
	if m.permissionService == nil {
		return errors.New("identity permission service is unavailable")
	}
	operations := make([]repository.RouteOperation, 0, len(descriptors))
	for _, descriptor := range descriptors {
		operations = append(operations, repository.RouteOperation{
			OperationCode:  descriptor.OperationCode,
			ModuleOwner:    descriptor.Owner,
			Method:         descriptor.Method,
			PathTemplate:   descriptor.Path,
			Audience:       string(descriptor.Audience),
			PermissionCode: descriptor.PermissionCode,
			LegacyAliases:  append([]string(nil), descriptor.LegacyAliases...),
		})
	}
	return m.permissionService.SyncRouteOperations(ctx, operations)
}
