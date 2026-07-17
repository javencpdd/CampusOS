package community

import (
	"context"
	"errors"
	"fmt"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/handler"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

const (
	ModuleID            = "core.community"
	portEventBus        = "platform.event-bus"
	portMemoryEventBus  = "platform.memory-event-bus"
	portCache           = "platform.cache"
	portCategoryReader  = "community.category-reader"
	portCategoryCatalog = "community.category-catalog"
	portThreadPort      = "community.thread-port"
	portPostPort        = "community.post-port"
	portModeration      = "community.moderation-gateway"
	portContent         = "community.content-gateway"
	portContentQuery    = "community.content-query"
)

type HTTPHandlers struct {
	Thread   *handler.ThreadHandler
	Category *handler.CategoryHandler
	Post     *handler.PostHandler
	Event    *handler.EventHandler
}

// Module owns Community adapter lookup, application composition, public Ports,
// and HTTP handlers. Other domains consume only the Ports or events.
type Module struct {
	app          *platformmodule.AppContext
	threads      repository.ThreadRepository
	categories   repository.CategoryRepository
	posts        repository.PostRepository
	governance   repository.ContentGovernanceRepository
	typePolicies repository.ThreadTypePolicyRepository

	threadService   *service.ThreadService
	categoryService *service.CategoryService
	postService     *service.PostService
	handlers        HTTPHandlers
}

func NewModule() *Module { return &Module{} }

func (m *Module) ID() string { return ModuleID }

func (m *Module) Dependencies() []string {
	return []string{"core.event-bus", "core.identity", "core.feature-registry", reliability.ModuleID}
}

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("community module app context is required")
	}
	threads, ok := app.Lookup(portThreadRepository)
	if !ok {
		return errors.New("community thread repository adapter is not bound by profile")
	}
	categories, ok := app.Lookup(portCategoryRepository)
	if !ok {
		return errors.New("community category repository adapter is not bound by profile")
	}
	posts, ok := app.Lookup(portPostRepository)
	if !ok {
		return errors.New("community post repository adapter is not bound by profile")
	}
	var valid bool
	if m.threads, valid = threads.(repository.ThreadRepository); !valid {
		return fmt.Errorf("community thread repository adapter has incompatible type %T", threads)
	}
	if m.categories, valid = categories.(repository.CategoryRepository); !valid {
		return fmt.Errorf("community category repository adapter has incompatible type %T", categories)
	}
	if m.posts, valid = posts.(repository.PostRepository); !valid {
		return fmt.Errorf("community post repository adapter has incompatible type %T", posts)
	}
	governance, ok := app.Lookup(portGovernanceRepository)
	if !ok {
		return errors.New("community governance repository adapter is not bound by profile")
	}
	if m.governance, valid = governance.(repository.ContentGovernanceRepository); !valid {
		return fmt.Errorf("community governance repository adapter has incompatible type %T", governance)
	}
	typePolicies, ok := app.Lookup(portThreadTypePolicyRepo)
	if !ok {
		return errors.New("community thread type policy repository adapter is not bound by profile")
	}
	if m.typePolicies, valid = typePolicies.(repository.ThreadTypePolicyRepository); !valid {
		return fmt.Errorf("community thread type policy repository adapter has incompatible type %T", typePolicies)
	}
	m.app = app
	for _, binding := range []struct {
		name  string
		value interface{}
	}{
		{portCategoryReader, communityport.NewRepositoryCategoryReader(m.categories)},
		{portCategoryCatalog, &moduleCategoryCatalog{module: m}},
		{portThreadPort, communityport.NewRepositoryThreadPort(m.threads)},
		{portPostPort, communityport.NewRepositoryPostPort(m.posts)},
	} {
		if err := app.Provide(binding.name, binding.value); err != nil {
			return err
		}
	}
	if err := app.Provide(portModeration, &moduleModerationGateway{module: m}); err != nil {
		return err
	}
	if err := app.Provide(portContent, &moduleContentGateway{module: m}); err != nil {
		return err
	}
	return app.Provide(portContentQuery, &moduleContentQuery{module: m})
}

func (m *Module) Start(context.Context) error {
	if m.app == nil || m.threads == nil || m.categories == nil || m.posts == nil {
		return errors.New("community module is not registered")
	}
	busValue, ok := m.app.Lookup(portEventBus)
	if !ok {
		return errors.New("community event bus port is unavailable")
	}
	bus, ok := busValue.(eventbus.EventBus)
	if !ok || bus == nil {
		return fmt.Errorf("community event bus port has incompatible type %T", busValue)
	}
	memoryBusValue, ok := m.app.Lookup(portMemoryEventBus)
	if !ok {
		return errors.New("community memory event bus port is unavailable")
	}
	memoryBus, ok := memoryBusValue.(*eventbus.MemoryEventBus)
	if !ok || memoryBus == nil {
		return fmt.Errorf("community memory event bus port has incompatible type %T", memoryBusValue)
	}
	cacheValue, ok := m.app.Lookup(portCache)
	if !ok {
		return errors.New("community cache port is unavailable")
	}
	appCache, ok := cacheValue.(cache.Cache)
	if !ok || appCache == nil {
		return fmt.Errorf("community cache port has incompatible type %T", cacheValue)
	}
	threads := service.NewThreadService(m.threads, bus)
	if reliabilityValue, found := m.app.Lookup("platform.reliability.service"); found {
		reliable, compatible := reliabilityValue.(*reliability.Service)
		if !compatible || reliable == nil {
			return fmt.Errorf("community reliability port has incompatible type %T", reliabilityValue)
		}
		threads.SetReliability(reliable)
	}
	// Standalone Community tests and legacy composition can omit Identity's
	// policy port. The production module graph always provides it before Start.
	if authorizationValue, found := m.app.Lookup("identity.authorization"); found {
		authorization, compatible := authorizationValue.(identityport.Authorization)
		if !compatible || authorization == nil {
			return fmt.Errorf("community authorization port has incompatible type %T", authorizationValue)
		}
		threads.SetContentAuthorization(authorization)
	}
	threads.SetCategoryRepository(m.categories)
	threads.SetThreadTypePolicyRepository(m.typePolicies)
	featureValue, found := m.app.Lookup("platform.feature-registry")
	if !found {
		return errors.New("community feature registry port is unavailable")
	}
	features, compatible := featureValue.(*platformfeature.Registry)
	if !compatible || features == nil {
		return fmt.Errorf("community feature registry port has incompatible type %T", featureValue)
	}
	threads.SetThreadTypeEnabledChecker(func(threadType domain.ThreadType) bool {
		switch domain.NormalizeThreadType(threadType) {
		case domain.ThreadTypeDiscussion:
			return true
		case domain.ThreadTypeArticle:
			return features.Enabled("controlled-richtext-article")
		case domain.ThreadTypeMutualAid:
			return features.Enabled("mutual-aid")
		case domain.ThreadTypeSecondhand:
			return features.Enabled("secondhand")
		default:
			return false
		}
	})
	threads.SetCache(appCache)
	threads.SetGovernanceRepository(m.governance)
	categories := service.NewCategoryService(m.categories, bus)
	categories.SetThreadRepository(m.threads)
	categories.SetThreadTypePolicyRepository(m.typePolicies)
	if reliabilityValue, found := m.app.Lookup("platform.reliability.service"); found {
		reliable, compatible := reliabilityValue.(*reliability.Service)
		if !compatible || reliable == nil {
			return fmt.Errorf("community reliability port has incompatible type %T", reliabilityValue)
		}
		categories.SetReliability(reliable)
	}
	posts := service.NewPostService(m.posts, bus)
	posts.SetThreadRepository(m.threads)
	posts.SetCategoryRepository(m.categories)
	posts.SetCache(appCache)
	m.threadService = threads
	m.categoryService = categories
	m.postService = posts
	m.handlers = HTTPHandlers{
		Thread:   handler.NewThreadHandler(threads),
		Category: handler.NewCategoryHandler(categories),
		Post:     handler.NewPostHandler(posts),
		Event:    handler.NewEventHandler(memoryBus),
	}
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.threadService == nil || m.categoryService == nil || m.postService == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "community services are not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) ThreadRepository() repository.ThreadRepository     { return m.threads }
func (m *Module) CategoryRepository() repository.CategoryRepository { return m.categories }
func (m *Module) PostRepository() repository.PostRepository         { return m.posts }
func (m *Module) ThreadService() *service.ThreadService             { return m.threadService }
func (m *Module) PostService() *service.PostService                 { return m.postService }
func (m *Module) Handlers() HTTPHandlers                            { return m.handlers }
