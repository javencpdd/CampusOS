package ai

import (
	"context"
	"encoding/json"
	"errors"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/jackc/pgx/v5/pgxpool"
	"strconv"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/config"
)

const ModuleID = "integration.ai"
const FeatureID = "ai-gateway"
const PortStatus = "ai.status/v1"

// StatusReader grants no invocation, credential or raw-log capability.
type StatusReader interface{ Status() ServiceStatus }
type statusReader struct{ service *Service }

func (s statusReader) Status() ServiceStatus { return s.service.Status() }

const portCallLogStore = "integration.ai.adapter.call-log-store"

type Module struct {
	config            config.AIConfig
	app               *platformmodule.AppContext
	service           *Service
	handler           *Handler
	managed           *ManagedService
	enabled           func() bool
	pluginTextAdapter PluginTextAdapter
	pluginText        *pluginTextService
}

// ConfigurePluginText binds an optional trusted adapter before module startup.
func (m *Module) ConfigurePluginText(adapter PluginTextAdapter) { m.pluginTextAdapter = adapter }

func NewModule(config config.AIConfig, enabled ...func() bool) *Module {
	m := &Module{config: config, enabled: func() bool { return config.Enabled }}
	if len(enabled) > 0 && enabled[0] != nil {
		m.enabled = enabled[0]
	}
	return m
}
func (m *Module) ID() string { return ModuleID }
func (m *Module) Dependencies() []string {
	return []string{"core.identity", reliability.ModuleID, "core.feature-registry"}
}

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("AI module app context is required")
	}
	m.app = app
	if value, ok := app.Lookup("platform.reliability.service"); ok {
		if reliable, ok := value.(*reliability.Service); ok {
			reliable.RegisterConsumer("plugin.ai.authorized.v1", "ai.plugin.metadata", func(_ context.Context, event reliability.Event) error {
				if identityport.ValidateClosedHTTPObject(event.Payload, []string{"publisher", "key", "version", "generation", "profile_id", "run_id"}, 2048) != nil {
					return ErrManagedDenied
				}
				var p map[string]string
				if json.Unmarshal(event.Payload, &p) != nil || !validText(p["publisher"], 128, true) || !validText(p["key"], 128, true) || !validText(p["version"], 64, true) || !managedIDValid("ar_", p["run_id"]) || (p["profile_id"] != "local-chat" && p["profile_id"] != "remote-chat") {
					return ErrManagedDenied
				}
				n, e := strconv.ParseInt(p["generation"], 10, 64)
				if e != nil || n < 1 {
					return ErrManagedDenied
				}
				return nil
			})
			reliable.RegisterConsumer("ai.managed.changed.v1", "ai.managed.metadata", func(_ context.Context, event reliability.Event) error {
				var payload map[string]string
				if json.Unmarshal(event.Payload, &payload) != nil || len(payload) != 2 || len(payload["id"]) > 128 {
					return ErrManagedDenied
				}
				switch payload["operation"] {
				case "profiles.provision", "binding.change", "profile.state", "call.reserve", "call.dispatch", "call.settle":
					return nil
				}
				return ErrManagedDenied
			})
		}
	}
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	if m.managed != nil {
		if err := m.managed.start(ctx); err != nil {
			return err
		}
		m.pluginText.start()
		return nil
	}
	var pool *pgxpool.Pool
	if value, ok := m.app.Lookup("integration.ai.adapter.pool"); ok {
		pool, _ = value.(*pgxpool.Pool)
	}
	value, _ := m.app.Lookup("platform.reliability.service")
	reliable, _ := value.(*reliability.Service)
	value, _ = m.app.Lookup("identity.admin-execution-authority")
	admins, _ := value.(identityport.AdminExecutionAuthorityReader)
	configs, keys, err := managedConfiguration(m.config)
	if !m.config.Enabled || pool == nil || reliable == nil || admins == nil {
		err = ErrManagedUnavailable
	}
	managed := &ManagedService{pool: pool, reliable: reliable, admins: admins, enabled: m.enabled, keys: keys, configs: configs, configurationErr: err, defaultProfile: m.config.DefaultProfile, instances: map[string]*managedInstance{}, remote: func(p security.EgressPolicy) (managedEgress, error) { return security.NewEgressBroker(p) }}
	if pool == nil {
		managed.configurationErr = ErrManagedUnavailable
	} else {
		if err = managed.start(ctx); err != nil {
			return err
		}
	}
	m.managed = managed
	m.pluginText = newPluginTextService(m.pluginTextAdapter, m.enabled)
	m.service = &Service{enabled: m.config.Enabled, provider: "managed", managed: managed}
	if value, ok := m.app.Lookup(portCallLogStore); ok {
		if store, ok := value.(CallLogStore); ok {
			m.service.SetCallLogStore(store)
		}
	}
	m.handler = NewHandler(m.service)
	value, _ = m.app.Lookup("identity.admin-session-authority")
	m.handler.adminSessions, _ = value.(identityport.AdminSessionAuthorityReader)
	if err = m.app.Provide(PortManagedChat, ManagedChat(managed)); err != nil {
		return err
	}
	if err = m.app.Provide(PortLocalCheckedChat, LocalCheckedChat(managed)); err != nil {
		return err
	}
	if err = m.app.Provide(PortCheckedChat, CheckedChat(managed)); err != nil {
		return err
	}
	if err = m.app.Provide(PortPluginText, PluginText(m.pluginText)); err != nil {
		return err
	}
	return m.app.Provide(PortStatus, StatusReader(statusReader{service: m.service}))
}
func (m *Module) Stop(context.Context) error {
	if m.pluginText != nil {
		m.pluginText.stop()
	}
	if m.managed != nil {
		m.managed.stop()
	}
	return nil
}
func (m *Module) Managed() *ManagedService { return m.managed }

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "AI module is not started"}
	}
	status := m.service.Status()
	if status.Enabled && !status.Ready {
		return platformmodule.Health{Status: platformmodule.HealthDegraded, Message: status.Error}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
func (m *Module) Service() *Service { return m.service }
func (m *Module) Handler() *Handler { return m.handler }
