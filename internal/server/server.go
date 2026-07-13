package server

import (
	"github.com/campusos/CampusOS/internal/ai"
	"github.com/campusos/CampusOS/internal/appearance"
	communitycore "github.com/campusos/CampusOS/internal/community"
	identitycore "github.com/campusos/CampusOS/internal/core/identity"
	corestorage "github.com/campusos/CampusOS/internal/core/storage"
	"github.com/campusos/CampusOS/internal/integration"
	"github.com/campusos/CampusOS/internal/mcp"
	"github.com/campusos/CampusOS/internal/message"
	"github.com/campusos/CampusOS/internal/moderation"
	"github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platformlog"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/internal/richtext"
	"github.com/campusos/CampusOS/internal/schedule"
	"github.com/campusos/CampusOS/internal/space"
	"github.com/campusos/CampusOS/internal/webhook"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

type Server struct {
	cfg         *config.Config
	bus         eventbus.EventBus
	manager     *plugin.Manager
	modules     *platformmodule.Registry
	features    *feature.Registry
	appearance  *appearance.Module
	community   *communitycore.Module
	identity    *identitycore.Module
	moderation  *moderation.Module
	storage     *corestorage.Module
	space       *space.Module
	richtext    *richtext.Module
	schedule    *schedule.Module
	ai          *ai.Module
	webhook     *webhook.Module
	mcp         *mcp.Module
	message     *message.Module
	platformLog *platformlog.Module
	integration *integration.Module
	appContext  *platformmodule.AppContext
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
