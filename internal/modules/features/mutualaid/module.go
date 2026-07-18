package mutualaid

import (
	"context"
	"errors"
	"fmt"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
)

const portStore = "feature.mutual-aid.adapter.store"

type ModuleConfig struct {
	Enabled func() bool
}

// Module composes Campus Mutual Aid through Community's public content ports.
// It owns only mutual_aid_details; it cannot bypass Community moderation,
// category type policy, revision, or thread data ownership.
type Module struct {
	config    ModuleConfig
	app       *platformmodule.AppContext
	store     Store
	community communityport.ContentGateway
	query     communityport.ContentQuery
	service   *Service
	handler   *Handler
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }
func (m *Module) ID() string                { return ModuleID }
func (m *Module) Dependencies() []string {
	return []string{"core.community", "core.user-storage", "core.feature-registry", reliability.ModuleID}
}

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("mutual aid module app context is required")
	}
	storeValue, ok := app.Lookup(portStore)
	if !ok {
		return errors.New("mutual aid store adapter is not bound by profile")
	}
	store, ok := storeValue.(Store)
	if !ok {
		return fmt.Errorf("mutual aid store adapter has incompatible type %T", storeValue)
	}
	communityValue, ok := app.Lookup("community.content-gateway")
	if !ok {
		return errors.New("community content gateway port is unavailable")
	}
	community, ok := communityValue.(communityport.ContentGateway)
	if !ok {
		return fmt.Errorf("community content gateway port has incompatible type %T", communityValue)
	}
	queryValue, ok := app.Lookup("community.content-query")
	if !ok {
		return errors.New("community content query port is unavailable")
	}
	query, ok := queryValue.(communityport.ContentQuery)
	if !ok {
		return fmt.Errorf("community content query port has incompatible type %T", queryValue)
	}
	m.app, m.store, m.community, m.query = app, store, community, query
	return nil
}

func (m *Module) Start(context.Context) error {
	if m.app == nil || m.store == nil || m.community == nil || m.query == nil {
		return errors.New("mutual aid module is not registered")
	}
	reliableValue, ok := m.app.Lookup("platform.reliability.service")
	if !ok {
		return errors.New("reliability service port is unavailable")
	}
	reliable, ok := reliableValue.(*reliability.Service)
	if !ok || reliable == nil {
		return fmt.Errorf("reliability service port has incompatible type %T", reliableValue)
	}
	service := NewService(m.store, m.community, m.query)
	service.SetReliability(reliable)
	service.SetEnabledChecker(m.enabled)
	m.service = service
	m.handler = NewHandler(service)
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "mutual aid service is not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) Handler() *Handler { return m.handler }
func (m *Module) Service() *Service { return m.service }

func (m *Module) enabled() bool {
	return m.config.Enabled == nil || m.config.Enabled()
}
