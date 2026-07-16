package server

import (
	communitycore "github.com/campusos/CampusOS/internal/modules/core/community"
	identitycore "github.com/campusos/CampusOS/internal/modules/core/identity"
	"github.com/campusos/CampusOS/internal/modules/core/moderation"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/modules/features/ai"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/runtime"
	"github.com/campusos/CampusOS/internal/modules/features/integration"
	"github.com/campusos/CampusOS/internal/modules/features/mcp"
	"github.com/campusos/CampusOS/internal/modules/features/message"
	"github.com/campusos/CampusOS/internal/modules/features/personalspace"
	"github.com/campusos/CampusOS/internal/modules/features/platformlog"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	"github.com/campusos/CampusOS/internal/modules/features/schedule"
	"github.com/campusos/CampusOS/internal/modules/features/webhook"
	"github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/plugin"
	modulecatalog "github.com/campusos/CampusOS/modules"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

type Server struct {
	cfg            *config.Config
	bus            eventbus.EventBus
	manager        *plugin.Manager
	modules        *platformmodule.Registry
	features       *feature.Registry
	featureHandler *feature.Handler
	moduleCatalog  *modulecatalog.Catalog
	appearance     *appearance.Module
	community      *communitycore.Module
	identity       *identitycore.Module
	moderation     *moderation.Module
	storage        *corestorage.Module
	space          *space.Module
	richtext       *richtext.Module
	schedule       *schedule.Module
	ai             *ai.Module
	webhook        *webhook.Module
	mcp            *mcp.Module
	message        *message.Module
	platformLog    *platformlog.Module
	integration    *integration.Module
	appContext     *platformmodule.AppContext
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
