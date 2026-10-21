package knowledge

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

const (
	ModuleID                   = "feature.knowledge"
	FeatureID                  = "knowledge"
	PortPublicSourceCandidates = "knowledge.public-source-candidates"
)

type Module struct {
	enabled  func() bool
	service  *PublicSourceService
	articles *PublicArticleSourceService
}

func NewModule(enabled func() bool) *Module { return &Module{enabled: enabled} }
func (m *Module) ID() string                { return ModuleID }
func (m *Module) Dependencies() []string {
	return []string{"core.community", "core.feature-registry", richtext.ModuleID}
}
func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("knowledge app context is required")
	}
	value, found := app.Lookup("community.content-query")
	query, typed := value.(PublicThreadQuery)
	if !found || !typed {
		return ErrSourceUnavailable
	}
	service, err := NewPublicSourceService(query, m.enabled)
	if err != nil {
		return err
	}
	value, found = app.Lookup(richtext.PortPublicTextSource)
	reader, typed := value.(richtext.PublicTextSourceReader)
	if !found || !typed {
		return ErrSourceUnavailable
	}
	articles, err := NewPublicArticleSourceService(reader, m.enabled)
	if err != nil {
		return err
	}
	if err = app.Provide(PortPublicSourceCandidates, PublicSourceCandidates(service)); err != nil {
		return err
	}
	if err = app.Provide(PortPublicArticleCandidates, PublicArticleCandidates(articles)); err != nil {
		return err
	}
	m.service = service
	m.articles = articles
	return nil
}
func (m *Module) Start(context.Context) error {
	if m.service == nil || m.articles == nil {
		return ErrSourceUnavailable
	}
	return nil
}
func (m *Module) Stop(context.Context) error { return nil }
func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.articles == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "knowledge source reader unavailable"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
