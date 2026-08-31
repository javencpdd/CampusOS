package personaldocuments

import (
	"context"
	"errors"
	"fmt"

	coreeditor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	platformobservability "github.com/campusos/CampusOS/internal/platform/observability"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/observability"
)

const portRepository = "personal-documents.adapter.repository"

type ModuleConfig struct{ Enabled func() bool }
type Module struct {
	config     ModuleConfig
	repository Repository
	objects    corestorage.ObjectPort
	reliable   *reliability.Service
	meter      observability.Meter
	service    *Service
	handler    *Handler
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }
func (m *Module) ID() string                { return ModuleID }
func (m *Module) Dependencies() []string {
	return []string{coreeditor.ModuleID, "core.identity", "core.user-storage", reliability.ModuleID, "core.feature-registry", platformobservability.ModuleID}
}
func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("personal documents app context is required")
	}
	value, ok := app.Lookup(portRepository)
	if !ok {
		return errors.New("personal document repository is unavailable")
	}
	repo, ok := value.(Repository)
	if !ok || repo == nil {
		return fmt.Errorf("personal document repository has incompatible type %T", value)
	}
	value, ok = app.Lookup("storage.objects")
	if !ok {
		return errors.New("storage object port is unavailable")
	}
	objects, ok := value.(corestorage.ObjectPort)
	if !ok || objects == nil {
		return fmt.Errorf("storage object port has incompatible type %T", value)
	}
	value, ok = app.Lookup("platform.reliability.service")
	if !ok {
		return errors.New("personal document reliability service is unavailable")
	}
	reliable, ok := value.(*reliability.Service)
	if !ok || reliable == nil {
		return fmt.Errorf("personal document reliability service has incompatible type %T", value)
	}
	m.repository, m.objects, m.reliable = repo, objects, reliable
	if value, exists := app.Lookup(platformobservability.PortMeter); exists {
		meter, compatible := value.(observability.Meter)
		if !compatible || meter == nil {
			return fmt.Errorf("personal documents observability meter has incompatible type %T", value)
		}
		m.meter = meter
	}
	return nil
}
func (m *Module) Start(ctx context.Context) error {
	svc, e := NewService(m.repository, m.objects)
	if e != nil {
		return e
	}
	svc.SetEnabledChecker(m.config.Enabled)
	svc.SetReliability(m.reliable)
	svc.SetMeter(m.meter)
	m.reliable.RegisterConsumer(previewRequestedEvent, previewRequestConsumer, svc.AcknowledgePreviewRequest)
	_ = svc.RefreshPreviewMetrics(ctx)
	m.service = svc
	m.handler = NewHandler(svc)
	return nil
}
func (m *Module) Stop(context.Context) error {
	if m.reliable != nil {
		m.reliable.RegisterConsumer(previewRequestedEvent, previewRequestConsumer, nil)
	}
	return nil
}
func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "personal document service is unavailable"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
func (m *Module) Handler() *Handler { return m.handler }
func (m *Module) Service() *Service { return m.service }
