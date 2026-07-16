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
	platformroute "github.com/campusos/CampusOS/internal/platform/route"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

const (
	ModuleID          = "core.identity"
	portEventBus      = "platform.event-bus"
	portUserReader    = "identity.user-reader"
	portAuthorization = "identity.authorization"
	portModeration    = "identity.moderation-policy"
)

type Config struct {
	JWT                 *auth.JWTManager
	PasswordHashEnabled bool
}

type HTTPHandlers struct {
	User *handler.UserHandler
	Role *handler.RoleHandler
}

// Module owns Identity application composition. Its Profile adapters are
// supplied before lifecycle registration; other modules consume public ports.
type Module struct {
	config Config
	app    *platformmodule.AppContext
	users  repository.UserRepository
	roles  repository.RoleRepository

	userService       *service.UserService
	permissionService *service.PermissionService
	handlers          HTTPHandlers
}

func NewModule(config Config) *Module {
	return &Module{config: config}
}

func (m *Module) ID() string { return ModuleID }

func (m *Module) Dependencies() []string { return []string{"core.event-bus"} }

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
	m.app = app
	m.users = userRepository
	m.roles = roleRepository
	m.permissionService = service.NewPermissionService(m.roles, m.users)
	if err := app.Provide(portUserReader, identityport.NewRepositoryUserReader(userRepository)); err != nil {
		return err
	}
	return app.Provide(portModeration, identityport.ModerationPolicy(identityport.NewPermissionModerationPolicy(m.permissionService)))
}

func (m *Module) Start(context.Context) error {
	if m.app == nil || m.users == nil || m.roles == nil {
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
	permissions := m.permissionService
	var credentials service.PgUserRepo
	if adapter, ok := m.users.(service.PgUserRepo); ok {
		credentials = adapter
	}
	users := service.NewUserService(m.users, m.config.JWT, credentials, bus)
	users.SetPasswordHashEnabled(m.config.PasswordHashEnabled)
	users.SetRoleRepository(m.roles)
	m.userService = users
	m.handlers = HTTPHandlers{User: handler.NewUserHandler(users), Role: handler.NewRoleHandler(permissions)}
	if err := m.app.Provide(portAuthorization, identityport.Authorization(permissions)); err != nil {
		return err
	}
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.userService == nil || m.permissionService == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "identity services are not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) UserRepository() repository.UserRepository { return m.users }

func (m *Module) Permissions() *service.PermissionService { return m.permissionService }

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
