package contenteditor

import (
	"context"
	"errors"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

const ModuleID = "core.content-editor"

// Module registers the dependency-neutral editor contract in the runtime
// graph. The package exposes pure functions only, so it has no mutable state
// or user-content port to expose to plugins.
type Module struct{ registered bool }

func NewModule() *Module                 { return &Module{} }
func (m *Module) ID() string             { return ModuleID }
func (m *Module) Dependencies() []string { return nil }
func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("content editor module app context is required")
	}
	m.registered = true
	return nil
}
func (m *Module) Start(context.Context) error { return nil }
func (m *Module) Stop(context.Context) error  { return nil }
func (m *Module) Health(context.Context) platformmodule.Health {
	if !m.registered {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "content editor module is not registered"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
