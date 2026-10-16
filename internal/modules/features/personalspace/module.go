package space

import (
	"context"
	"errors"
	"fmt"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
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
	quotaManager corestorage.QuotaManager
	styles       StyleApplication
	attacher     interface{ AttachSpaceStyleTarget(StyleApplication) }
	service      *Service
	handler      *Handler
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }

func (m *Module) ID() string { return ModuleID }

func (m *Module) Dependencies() []string {
	return []string{"core.identity", "core.community", "core.user-storage", "core.reliability", "core.feature-registry", "feature.appearance"}
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
	quotaValue, ok := app.Lookup("storage.quota-manager")
	if !ok {
		return errors.New("user storage quota manager is unavailable")
	}
	quotaManager, ok := quotaValue.(corestorage.QuotaManager)
	if !ok || quotaManager == nil {
		return fmt.Errorf("user storage quota manager has incompatible type %T", quotaValue)
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
	m.app, m.repo, m.users, m.contentQuery, m.storage, m.quotaManager = app, repo, users, contentQuery, storage, quotaManager
	m.styles, m.attacher = styles, attacher
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	if m.app == nil || m.repo == nil || m.users == nil || m.contentQuery == nil || m.storage == nil {
		return errors.New("personal space module is not registered")
	}
	var ownerVerifier identityport.SessionVerifier
	if value, found := m.app.Lookup("identity.session-verifier"); found {
		var compatible bool
		ownerVerifier, compatible = value.(identityport.SessionVerifier)
		if !compatible || nilOwnContentsAdapter(ownerVerifier) {
			return fmt.Errorf("personal space owner session verifier port has incompatible type %T", value)
		}
	}
	var ownerStrength identityport.SessionStrengthReader
	if value, found := m.app.Lookup("identity.session-strength"); found {
		var compatible bool
		ownerStrength, compatible = value.(identityport.SessionStrengthReader)
		if !compatible || nilOwnContentsAdapter(ownerStrength) {
			return fmt.Errorf("personal space owner session strength port has incompatible type %T", value)
		}
	}
	svc := NewService(m.repo, identityUserReader{reader: m.users})
	var adminAuthority identityport.AdminSessionAuthorityReader
	if value, found := m.app.Lookup("identity.admin-session-authority"); found {
		var compatible bool
		adminAuthority, compatible = value.(identityport.AdminSessionAuthorityReader)
		if !compatible || corestorage.NilImageAdapter(adminAuthority) {
			return fmt.Errorf("personal space admin session authority has incompatible type %T", value)
		}
	}
	if value, found := m.app.Lookup("identity.admin-execution-authority"); found {
		reader, compatible := value.(identityport.AdminExecutionAuthorityReader)
		if !compatible || corestorage.NilImageAdapter(reader) {
			return fmt.Errorf("personal space admin execution authority has incompatible type %T", value)
		}
		svc.SetAdminExecutionAuthority(reader)
	}
	svc.SetContentQuery(m.contentQuery)
	svc.SetQuotaManager(m.quotaManager)
	svc.SetPluginEnabledChecker(m.enabled)
	if value, found := m.app.Lookup("storage.objects"); found {
		account, compatible := value.(corestorage.InlineImageAccounting)
		if !compatible || corestorage.NilImageAdapter(account) {
			return fmt.Errorf("personal space avatar accounting has incompatible type %T", value)
		}
		svc.SetAvatarStorage(m.storage, account)
	}
	if value, ok := m.app.Lookup("platform.reliability.service"); ok {
		command, compatible := value.(*reliability.Service)
		if !compatible || command == nil {
			return fmt.Errorf("personal space reliability command has incompatible type %T", value)
		}
		svc.SetReliability(command)
		if reporter, ok := value.(CompatibilityReporter); ok {
			svc.SetCompatibilityReporter(reporter)
		}
	}
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
	// Quota overrides govern the shared user directory even when an installation
	// retained a custom compatible root during migration.
	store.quota = m.quotaManager
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
	m.handler.SetOwnerSessionProof(ownerVerifier, ownerStrength)
	m.handler.SetAdminSessionAuthority(adminAuthority)
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

func (r identityUserReader) GetByIDForUpdate(ctx context.Context, id string) (*identitydomain.User, error) {
	reader, ok := r.reader.(identityport.UserCommandReader)
	if !ok || corestorage.NilImageAdapter(reader) {
		return nil, ErrSpaceFactsUnavailable
	}
	value, err := reader.GetUserForUpdate(ctx, id)
	if err != nil {
		return nil, err
	}
	return identityUser(value), nil
}

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
	return &identitydomain.User{ID: value.ID, Username: value.Username, Nickname: value.Nickname, Email: value.Email, Avatar: value.Avatar, Bio: value.Bio, Status: identitydomain.UserStatus(value.Status)}
}

type communityThreadGateway struct{ gateway communityport.ContentGateway }

func (g communityThreadGateway) List(ctx context.Context, filter communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error) {
	return g.gateway.ListThreads(ctx, filter)
}
