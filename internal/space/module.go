package space

import (
	"context"
	"errors"
	"fmt"

	communitydomain "github.com/campusos/CampusOS/internal/community/domain"
	communityport "github.com/campusos/CampusOS/internal/community/port"
	identitydomain "github.com/campusos/CampusOS/internal/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/core/storage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

const ModuleID = "feature.personal-space"

const portRepository = "feature.personal-space.adapter.repository"

// ModuleConfig keeps legacy configuration at the boundary. The feature itself
// receives no Plugin Manager and operates exclusively through Core ports.
type ModuleConfig struct {
	FileStorageConfig func() FileStorageConfig
	Enabled           func() bool
}

type Module struct {
	config       ModuleConfig
	app          *platformmodule.AppContext
	repo         Repository
	users        identityport.UserReader
	contentQuery communityport.ContentQuery
	storage      corestorage.Port
	styles       StyleApplication
	attacher     interface{ AttachSpaceStyleTarget(StyleApplication) }
	service      *Service
	handler      *Handler
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }

func (m *Module) ID() string { return ModuleID }

func (m *Module) Dependencies() []string {
	return []string{"core.identity", "core.community", "core.user-storage", "core.feature-registry", "feature.appearance"}
}

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("personal space module app context is required")
	}
	value, ok := app.Lookup(portRepository)
	if !ok {
		return errors.New("personal space repository adapter is not bound by profile")
	}
	repo, ok := value.(Repository)
	if !ok {
		return fmt.Errorf("personal space repository adapter has incompatible type %T", value)
	}
	userValue, ok := app.Lookup("identity.user-reader")
	if !ok {
		return errors.New("identity user reader port is unavailable")
	}
	users, ok := userValue.(identityport.UserReader)
	if !ok {
		return fmt.Errorf("identity user reader port has incompatible type %T", userValue)
	}
	communityValue, ok := app.Lookup("community.content-query")
	if !ok {
		return errors.New("community content query port is unavailable")
	}
	contentQuery, ok := communityValue.(communityport.ContentQuery)
	if !ok {
		return fmt.Errorf("community content query port has incompatible type %T", communityValue)
	}
	storageValue, ok := app.Lookup("storage.user")
	if !ok {
		return errors.New("user storage port is unavailable")
	}
	storage, ok := storageValue.(corestorage.Port)
	if !ok {
		return fmt.Errorf("user storage port has incompatible type %T", storageValue)
	}
	appearanceValue, ok := app.Lookup("appearance.application")
	if !ok {
		return errors.New("appearance application is unavailable")
	}
	styles, ok := appearanceValue.(StyleApplication)
	if !ok {
		return fmt.Errorf("appearance application has incompatible style contract %T", appearanceValue)
	}
	attacher, ok := appearanceValue.(interface{ AttachSpaceStyleTarget(StyleApplication) })
	if !ok {
		return fmt.Errorf("appearance application cannot attach personal-space styles: %T", appearanceValue)
	}
	m.app, m.repo, m.users, m.contentQuery, m.storage = app, repo, users, contentQuery, storage
	m.styles, m.attacher = styles, attacher
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	if m.app == nil || m.repo == nil || m.users == nil || m.contentQuery == nil || m.storage == nil {
		return errors.New("personal space module is not registered")
	}
	svc := NewService(m.repo, identityUserReader{reader: m.users})
	svc.SetContentQuery(m.contentQuery)
	svc.SetPluginEnabledChecker(m.enabled)
	config := FileStorageConfig{}
	if m.config.FileStorageConfig != nil {
		config = m.config.FileStorageConfig()
	}
	var store *LocalFileStore
	var err error
	if NormalizePersonalSpaceFileRoot(config.RootDir) == corestorage.DefaultRoot {
		store, err = NewLocalFileStoreWithStorage(config, m.storage)
	} else {
		// Existing custom roots remain a compatibility adapter until their data
		// has been explicitly migrated into User Storage.
		store, err = NewLocalFileStore(config)
	}
	if err != nil {
		return fmt.Errorf("initialize personal space file store: %w", err)
	}
	svc.SetFileStore(store)
	busValue, ok := m.app.Lookup("platform.event-bus")
	if !ok {
		return errors.New("personal space event bus port is unavailable")
	}
	bus, ok := busValue.(eventbus.EventBus)
	if !ok || bus == nil {
		return fmt.Errorf("personal space event bus port has incompatible type %T", busValue)
	}
	if err := svc.RegisterEventHandlers(bus); err != nil {
		return err
	}
	m.attacher.AttachSpaceStyleTarget(svc)
	m.service = svc
	m.handler = NewHandlerWithStyleApplication(svc, m.styles)
	return m.app.Provide("feature.personal-space.service", svc)
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "personal space services are not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) Handler() *Handler { return m.handler }
func (m *Module) Service() *Service { return m.service }

func (m *Module) enabled() bool {
	return m.config.Enabled == nil || m.config.Enabled()
}

type identityUserReader struct{ reader identityport.UserReader }

func (r identityUserReader) GetByID(ctx context.Context, id string) (*identitydomain.User, error) {
	value, err := r.reader.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	return identityUser(value), nil
}

func (r identityUserReader) GetByUsername(ctx context.Context, username string) (*identitydomain.User, error) {
	value, err := r.reader.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	return identityUser(value), nil
}

func identityUser(value identityport.User) *identitydomain.User {
	return &identitydomain.User{ID: value.ID, Username: value.Username, Nickname: value.Nickname, Avatar: value.Avatar, Bio: value.Bio}
}

type communityThreadGateway struct{ gateway communityport.ContentGateway }

func (g communityThreadGateway) List(ctx context.Context, filter communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error) {
	return g.gateway.ListThreads(ctx, filter)
}
