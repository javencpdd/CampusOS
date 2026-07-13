package moderation

import (
	"context"
	"errors"
	"fmt"

	communityport "github.com/campusos/CampusOS/internal/community/port"
	identityport "github.com/campusos/CampusOS/internal/core/identity/port"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

const ModuleID = "core.moderation"

type ModuleConfig struct {
	ConfigProvider func() Config
}

// Module owns always-on scope enforcement and audit composition. The optional
// legacy config provider is an input adapter only; it never controls whether
// the Core authorization path exists.
type Module struct {
	config    ModuleConfig
	app       *platformmodule.AppContext
	policy    identityport.ModerationPolicy
	community communityport.ModerationGateway
	audit     AuditStore
	service   *Service
	handler   *Handler
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }

func (m *Module) ID() string { return ModuleID }

func (m *Module) Dependencies() []string { return []string{"core.identity", "core.community"} }

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("moderation module app context is required")
	}
	policyValue, ok := app.Lookup("identity.moderation-policy")
	if !ok {
		return errors.New("identity moderation policy port is unavailable")
	}
	policy, ok := policyValue.(identityport.ModerationPolicy)
	if !ok {
		return fmt.Errorf("identity moderation policy port has incompatible type %T", policyValue)
	}
	communityValue, ok := app.Lookup("community.moderation-gateway")
	if !ok {
		return errors.New("community moderation gateway port is unavailable")
	}
	community, ok := communityValue.(communityport.ModerationGateway)
	if !ok {
		return fmt.Errorf("community moderation gateway port has incompatible type %T", communityValue)
	}
	auditValue, ok := app.Lookup(portAuditStore)
	if !ok {
		return errors.New("moderation audit adapter is not bound by profile")
	}
	audit, ok := auditValue.(AuditStore)
	if !ok {
		return fmt.Errorf("moderation audit adapter has incompatible type %T", auditValue)
	}
	m.app = app
	m.policy = policy
	m.community = community
	m.audit = audit
	return nil
}

func (m *Module) Start(context.Context) error {
	if m.policy == nil || m.community == nil || m.audit == nil {
		return errors.New("moderation module is not registered")
	}
	service := NewService(m.policy, m.community, m.audit, ConfigFromPluginConfig(nil))
	if m.config.ConfigProvider != nil {
		service.SetConfigProvider(m.config.ConfigProvider)
	}
	// Scope enforcement and audit remain active even when every operation
	// switch is disabled. Only individual action permissions are configurable.
	service.SetEnabledChecker(func() bool { return true })
	m.service = service
	m.handler = NewHandler(service)
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "moderation services are not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) Handler() *Handler { return m.handler }

func (m *Module) Service() *Service { return m.service }
