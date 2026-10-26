package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	platformobservability "github.com/campusos/CampusOS/internal/platform/observability"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/observability"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

const (
	ModuleID                   = "feature.knowledge"
	FeatureID                  = "knowledge"
	PortCollections            = "knowledge.collections"
	PortPublicSourceCandidates = "knowledge.public-source-candidates"
)

type Module struct {
	pluginSourceAdapter PublicPluginSourceAdapter
	pluginSources       *pluginSourceService
	managedRAGModel     ManagedRAGModel
	publicManagedRAG    *PublicLocalRAG
	privateManagedRAG   *PrivateLocalRAG
	ragModel            LocalAnswerModel
	rag                 *PublicLocalRAG
	privateRAGModel     PrivateLocalAnswerModel
	privateRAG          *PrivateLocalRAG
	privateReader       PrivateDocumentMetadataReader
	privateFullReader   PrivateSourceReader
	privateVault        *PrivateVault
	privateIndex        *PrivateIndexService
	meter               observability.Meter
	app                 *platformmodule.AppContext
	collections         *CollectionService
	handler             *Handler
	enabled             func() bool
	service             *PublicSourceService
	articles            *PublicArticleSourceService
	indexRequests       *indexRequestConsumer
	backfill            *indexBackfiller
	indexer             *IndexService
	sourceEvents        *indexSourceConsumer
	retrieval           *RetrievalService
}

func NewModule(enabled func() bool) *Module                         { return &Module{enabled: enabled} }
func (m *Module) ConfigurePluginSource(a PublicPluginSourceAdapter) { m.pluginSourceAdapter = a }
func NewModuleWithPrivateDocuments(enabled func() bool, reader PrivateDocumentMetadataReader) *Module {
	return &Module{enabled: enabled, privateReader: reader}
}
func (m *Module) ConfigurePrivateIndex(reader PrivateSourceReader, vault *PrivateVault) {
	m.privateFullReader, m.privateVault = reader, vault
}
func (m *Module) ConfigurePublicLocalRAG(model LocalAnswerModel)         { m.ragModel = model }
func (m *Module) PublicLocalRAG() *PublicLocalRAG                        { return m.rag }
func (m *Module) ConfigurePrivateLocalRAG(model PrivateLocalAnswerModel) { m.privateRAGModel = model }
func (m *Module) PrivateLocalRAG() *PrivateLocalRAG                      { return m.privateRAG }
func (m *Module) PrivateIndex() *PrivateIndexService                     { return m.privateIndex }
func (m *Module) ConfigureManagedRAG(model ManagedRAGModel)              { m.managedRAGModel = model }
func (m *Module) PublicRAG() *PublicLocalRAG                             { return m.publicManagedRAG }
func (m *Module) PrivateRAG() *PrivateLocalRAG                           { return m.privateManagedRAG }
func (m *Module) ID() string                                             { return ModuleID }
func (m *Module) Dependencies() []string {
	return []string{"core.community", "core.identity", reliability.ModuleID, "core.feature-registry", richtext.ModuleID, "feature.personal-documents"}
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
	value, _ = app.Lookup(portRepository)
	requests, ok := value.(indexRequestStore)
	if !ok || nilSourceAdapter(requests) {
		return ErrSourceUnavailable
	}
	value, _ = app.Lookup("platform.reliability.service")
	reliable, ok := value.(*reliability.Service)
	if !ok || reliable == nil {
		return ErrSourceUnavailable
	}
	consumer, err := newIndexRequestConsumer(requests, reliable)
	if err != nil {
		return err
	}
	scanner, ok := valueBackfillStore(app)
	if !ok {
		return ErrSourceUnavailable
	}
	backfill, err := newIndexBackfiller(scanner, reliable)
	if err != nil {
		return err
	}
	if err = app.Provide(PortPublicSourceCandidates, PublicSourceCandidates(service)); err != nil {
		return err
	}
	if err = app.Provide(PortPublicArticleCandidates, PublicArticleCandidates(articles)); err != nil {
		return err
	}
	// Register before any module starts: the Core worker starts before this
	// Feature. A fallback must never acknowledge old events without the queue.
	reliable.RegisterConsumer(selectionChangedEvent, IndexRequestConsumerName, consumer.consume)
	indexRepo, ok := requests.(indexStore)
	if !ok {
		return ErrSourceUnavailable
	}
	sourceConsumer := &indexSourceConsumer{store: indexRepo, reliable: reliable}
	for event := range indexSourceEvents {
		reliable.RegisterConsumer(event, IndexSourceConsumerName, sourceConsumer.consume)
	}
	reliable.RegisterConsumer("knowledge.private.changed.v1", "knowledge.private.metadata", func(_ context.Context, event reliability.Event) error {
		var payload map[string]string
		if json.Unmarshal(event.Payload, &payload) != nil || len(payload) != 1 || !validPrivateID(payload["id"]) {
			return ErrInvalid
		}
		return nil
	})
	reliable.RegisterConsumer("knowledge.private.rag.approved.v1", "knowledge.private.rag.metadata", func(_ context.Context, event reliability.Event) error {
		if identityport.ValidateClosedHTTPObject(event.Payload, []string{"id", "step", "profile"}, 1024) != nil {
			return ErrInvalid
		}
		var payload map[string]string
		if json.Unmarshal(event.Payload, &payload) != nil || !opaque("ar_", payload["id"]) || payload["step"] != "chat" || (payload["profile"] != "local-chat" && payload["profile"] != "remote-chat") {
			return ErrInvalid
		}
		return nil
	})
	m.sourceEvents = sourceConsumer
	m.indexRequests = consumer
	m.backfill = backfill
	if value, exists := app.Lookup(platformobservability.PortMeter); exists {
		meter, ok := value.(observability.Meter)
		if !ok || nilSourceAdapter(meter) {
			return ErrSourceUnavailable
		}
		m.meter = meter
	}
	m.app = app
	m.service = service
	m.articles = articles
	return nil
}
func (m *Module) Start(ctx context.Context) error {
	if m.service == nil || m.articles == nil || m.app == nil || m.indexRequests == nil || m.backfill == nil {
		return ErrSourceUnavailable
	}
	// Resolve required private-read proofs before publishing ports or assigning
	// handler state. A failed first Start must not bypass this check on retry.
	var verifier identityport.SessionVerifier
	var strength identityport.SessionStrengthReader
	if !nilSourceAdapter(m.privateReader) {
		value, _ := m.app.Lookup("identity.session-verifier")
		var ok bool
		verifier, ok = value.(identityport.SessionVerifier)
		if !ok || nilSourceAdapter(verifier) {
			return ErrPrivateUnavailable
		}
		value, _ = m.app.Lookup("identity.session-strength")
		strength, ok = value.(identityport.SessionStrengthReader)
		if !ok || nilSourceAdapter(strength) {
			return ErrPrivateUnavailable
		}
	}
	if m.collections != nil && m.handler != nil {
		if m.pluginSources != nil {
			m.pluginSources.start()
		}
		if m.publicManagedRAG != nil {
			m.publicManagedRAG.start()
		}
		if m.privateManagedRAG != nil {
			m.privateManagedRAG.start()
		}
		if m.rag != nil {
			m.rag.start()
		}
		if m.privateIndex != nil {
			m.privateIndex.start(ctx)
		}
		if m.privateRAG != nil {
			m.privateRAG.start()
		}
		if m.handler.privateSources != nil {
			m.handler.privateSources.stopped.Store(false)
		}
		if err := m.backfill.start(ctx); err != nil {
			return err
		}
		m.indexRequests.resume()
		m.sourceEvents.stopped.Store(false)
		m.retrieval.stopped.Store(false)
		return m.indexer.start(ctx)
	}
	value, _ := m.app.Lookup(portRepository)
	repo, ok := value.(Repository)
	if !ok || nilSourceAdapter(repo) {
		return ErrSourceUnavailable
	}
	value, _ = m.app.Lookup("identity.admin-execution-authority")
	admins, ok := value.(identityport.AdminExecutionAuthorityReader)
	if !ok || nilSourceAdapter(admins) {
		return ErrSourceUnavailable
	}
	value, _ = m.app.Lookup("identity.admin-session-authority")
	sessions, ok := value.(identityport.AdminSessionAuthorityReader)
	if !ok || nilSourceAdapter(sessions) {
		return ErrSourceUnavailable
	}
	value, _ = m.app.Lookup("identity.integration-read-gateway")
	machine, ok := value.(identityport.IntegrationReadGateway)
	if !ok || nilSourceAdapter(machine) {
		return ErrSourceUnavailable
	}
	value, _ = m.app.Lookup("platform.reliability.service")
	reliable, ok := value.(*reliability.Service)
	if !ok || reliable == nil {
		return ErrSourceUnavailable
	}
	value, _ = m.app.Lookup("community.content-query")
	query, ok := value.(communityport.ContentQuery)
	if !ok || nilSourceAdapter(query) {
		return ErrSourceUnavailable
	}
	value, _ = m.app.Lookup("community.source-category-catalog")
	categories, ok := value.(communityport.SourceCategoryCatalog)
	if !ok || nilSourceAdapter(categories) {
		return ErrSourceUnavailable
	}
	service, e := NewCollectionService(repo, admins, reliable, query, categories, m.service, m.articles, m.enabled)
	if e != nil {
		return e
	}
	indexRepo, ok := repo.(indexStore)
	if !ok {
		return ErrSourceUnavailable
	}
	inventory, ok := query.(communityport.SourceReferenceInventory)
	if !ok {
		return ErrSourceUnavailable
	}
	m.indexer, e = newIndexService(indexRepo, service, inventory)
	if e != nil {
		return e
	}
	m.indexer.meter = m.meter
	m.retrieval, e = newRetrievalService(m.indexer, LexicalProvider{})
	if e != nil {
		return e
	}
	if e = m.app.Provide(PortPublicRetrieval, PublicRetrieval(m.retrieval)); e != nil {
		return e
	}
	if !nilSourceAdapter(m.ragModel) {
		m.rag = newPublicLocalRAG(m.retrieval, service, m.indexer, m.ragModel)
		if e = m.app.Provide(PortPublicLocalRAG, m.rag); e != nil {
			return e
		}
	}
	if e = m.app.Provide(PortCollections, service); e != nil {
		return e
	}
	m.pluginSources = newPluginSourceService(service, m.pluginSourceAdapter)
	if e = m.app.Provide(PortPluginPublicSource, PluginPublicSource(m.pluginSources)); e != nil {
		return e
	}
	m.indexRequests.resume()
	m.collections = service
	m.handler = NewHandler(service, sessions, m.retrieval)
	m.handler.privateSources = &privateSourcePreflight{reader: m.privateReader, enabled: m.enabled}
	m.handler.userVerifier, m.handler.userStrength = verifier, strength
	m.handler.machine = machine
	if !nilSourceAdapter(m.managedRAGModel) {
		m.publicManagedRAG = newPublicManagedRAG(m.retrieval, service, m.indexer, m.managedRAGModel)
		m.handler.publicRAG, m.handler.ragModel = m.publicManagedRAG, m.managedRAGModel
		if e = m.app.Provide(PortPublicRAG, m.publicManagedRAG); e != nil {
			return e
		}
	}
	if pg, ok := repo.(*PgRepository); ok && !nilSourceAdapter(m.privateFullReader) && m.privateVault != nil {
		var schemaReady bool
		_ = pg.pool.QueryRow(ctx, `SELECT to_regclass('knowledge_private_sources') IS NOT NULL AND to_regclass('knowledge_private_tasks') IS NOT NULL`).Scan(&schemaReady)
		if schemaReady {
			m.privateIndex = &PrivateIndexService{repo: pg, vault: m.privateVault, reader: m.privateFullReader, reliable: reliable, enabled: m.enabled}
			m.handler.privateIndex = m.privateIndex
			m.privateIndex.start(ctx)
			if !nilSourceAdapter(m.managedRAGModel) {
				m.privateManagedRAG = newPrivateManagedRAG(m.privateIndex, m.managedRAGModel)
				m.handler.privateRAG = m.privateManagedRAG
				if e = m.app.Provide(PortPrivateRAG, m.privateManagedRAG); e != nil {
					return e
				}
			}
			if !nilSourceAdapter(m.privateRAGModel) {
				m.privateRAG = newPrivateLocalRAG(m.privateIndex, m.privateRAGModel)
				if e = m.app.Provide(PortPrivateLocalRAG, m.privateRAG); e != nil {
					return e
				}
			}
		}
	}
	if e = m.backfill.start(ctx); e != nil {
		return e
	}
	return m.indexer.start(ctx)
}
func (m *Module) Handler() *Handler               { return m.handler }
func (m *Module) Collections() *CollectionService { return m.collections }
func (m *Module) Indexer() *IndexService          { return m.indexer }
func (m *Module) Retrieval() PublicRetrieval      { return m.retrieval }
func (m *Module) Stop(ctx context.Context) error {
	if m.pluginSources != nil {
		m.pluginSources.stop()
	}
	if m.privateManagedRAG != nil {
		m.privateManagedRAG.stop()
	}
	if m.publicManagedRAG != nil {
		m.publicManagedRAG.stop()
	}
	if m.privateRAG != nil {
		m.privateRAG.stop()
	}
	if m.rag != nil {
		m.rag.stop()
	}
	if m.privateIndex != nil {
		m.privateIndex.stop()
	}
	if m.handler != nil && m.handler.privateSources != nil {
		m.handler.privateSources.stopped.Store(true)
	}
	// Keep the registered callback rejecting work until Core stops its worker;
	// unregistering here would let the fallback acknowledge shutdown-window events.
	m.indexRequests.stop()
	if m.retrieval != nil {
		m.retrieval.stopped.Store(true)
	}
	if m.sourceEvents != nil {
		m.sourceEvents.stopped.Store(true)
	}
	var stopErr error
	if m.indexer != nil {
		stopErr = m.indexer.stop(ctx)
	}
	if m.backfill != nil {
		stopErr = errors.Join(stopErr, m.backfill.stop(ctx))
	}
	return stopErr
}
func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.articles == nil || m.collections == nil || m.handler == nil || m.indexRequests == nil || m.backfill == nil || m.indexRequests.stopped.Load() {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "knowledge source reader unavailable"}
	}
	if m.backfill.failed.Load() {
		return platformmodule.Health{Status: platformmodule.HealthDegraded, Message: "knowledge metadata backfill retry pending"}
	}
	if m.indexer == nil || m.sourceEvents == nil || m.retrieval == nil || m.retrieval.stopped.Load() {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "knowledge index executor unavailable"}
	}
	if m.indexer.failed.Load() {
		return platformmodule.Health{Status: platformmodule.HealthDegraded, Message: "knowledge index execution retry pending"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func valueBackfillStore(app *platformmodule.AppContext) (indexBackfillStore, bool) {
	value, _ := app.Lookup(portRepository)
	store, ok := value.(indexBackfillStore)
	return store, ok && !nilSourceAdapter(store)
}
