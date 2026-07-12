package server

import (
	"github.com/campusos/CampusOS/internal/appearance"
	"github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

type Server struct {
	cfg        *config.Config
	bus        eventbus.EventBus
	manager    *plugin.Manager
	modules    *platformmodule.Registry
	features   *feature.Registry
	appearance *appearance.Facade
	appContext *platformmodule.AppContext
}

func New(cfg *config.Config) *Server { return &Server{cfg: cfg} }

// Run owns only the process bootstrap. Business composition lives in application.go
// and is migrated module by module without changing the public server constructor.
func (s *Server) Run() error {
	infra, err := s.startInfrastructure()
	if err != nil {
		return err
	}
	defer infra.Stop()
	return s.runApplication(infra)
}
