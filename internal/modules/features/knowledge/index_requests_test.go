package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func requestEvent(t *testing.T, kind, id string) reliability.Event {
	t.Helper()
	op := "collection.save"
	if kind == "official" {
		op = "official.save"
	}
	e, err := reliability.NewEvent(selectionChangedEvent, "knowledge", id, map[string]string{"resource_id": id, "operation": op})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func indexRows(t *testing.T, repo *MemoryRepository) []IndexRequest {
	t.Helper()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	rows := []IndexRequest{}
	for _, r := range repo.rows[indexRequestRecords] {
		var request IndexRequest
		if e := json.Unmarshal(r.Data, &request); e != nil {
			t.Fatal(e)
		}
		rows = append(rows, request)
	}
	return rows
}
func TestIndexRequestsCurrentFactsDuplicateAndReorderedEvents(t *testing.T) {
	f := fixtureCollections(t)
	c := f.collection(t)
	consumer, e := newIndexRequestConsumer(f.repo, f.s.reliable)
	if e != nil {
		t.Fatal(e)
	}
	old := requestEvent(t, "collection", c.ID)
	old.ID = "999999"
	if e = consumer.consume(t.Context(), old); e != nil {
		t.Fatal(e)
	}
	c.Title = "current"
	c, e = f.s.SaveCollection(t.Context(), c, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	newer := requestEvent(t, "collection", c.ID)
	newer.ID = "1"
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := consumer.consume(t.Context(), newer); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	// Delayed event IDs neither select the old revision nor form a cursor.
	if e = consumer.consume(t.Context(), old); e != nil {
		t.Fatal(e)
	}
	rows := indexRows(t, f.repo)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	found := false
	for _, r := range rows {
		if r.TargetVersion == c.Version {
			found = true
		}
		if r.ID != indexRequestID(r.TargetKind, r.TargetID, r.TargetVersion) || r.Status != "pending" {
			t.Fatal(r)
		}
	}
	if !found {
		t.Fatal("current version request absent")
	}
}
func TestIndexRequestsOfficialFeatureOffMissingAndPayloadBoundary(t *testing.T) {
	f := fixtureCollections(t)
	source, e := f.s.SaveOfficial(t.Context(), Official{Title: "PRIVATE_TITLE", Content: "PRIVATE_BODY", Format: "text", Status: "draft"}, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	consumer, e := newIndexRequestConsumer(f.repo, f.s.reliable)
	if e != nil {
		t.Fatal(e)
	}
	f.enabled = false
	event := requestEvent(t, "official", source.ID)
	event.Headers = json.RawMessage(`{"Authorization":"DO_NOT_COPY"}`)
	if e = consumer.consume(t.Context(), event); e != nil {
		t.Fatal(e)
	}
	missing := "kc_" + strings.Repeat("a", 32)
	if e = consumer.consume(t.Context(), requestEvent(t, "collection", missing)); e != nil {
		t.Fatal(e)
	}
	rows := indexRows(t, f.repo)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	foundMissing := false
	for _, r := range rows {
		if r.TargetID == missing && r.TargetVersion == 0 {
			foundMissing = true
		}
	}
	wire, _ := json.Marshal(rows)
	for _, secret := range []string{"PRIVATE_TITLE", "PRIVATE_BODY", "Authorization", "DO_NOT_COPY", "official.save"} {
		if strings.Contains(string(wire), secret) {
			t.Fatal("task copied event/body data", secret)
		}
	}
	if !foundMissing {
		t.Fatal("missing-target reconciliation intent absent")
	}
	before := len(rows)
	for _, tc := range []struct {
		name   string
		change func(*reliability.Event)
	}{
		{"unknown_type", func(e *reliability.Event) { e.Type = "community.thread.updated" }},
		{"wrong_schema", func(e *reliability.Event) { e.SchemaVersion = "v2" }},
		{"wrong_domain", func(e *reliability.Event) { e.AggregateType = "user" }},
		{"aggregate_mismatch", func(e *reliability.Event) { e.AggregateID = missing }},
		{"unknown_operation", func(e *reliability.Event) {
			e.Payload = json.RawMessage(`{"resource_id":"` + source.ID + `","operation":"official.fetch_url"}`)
		}},
		{"forged_version", func(e *reliability.Event) {
			e.Payload = json.RawMessage(`{"resource_id":"` + source.ID + `","operation":"official.save","version":"1"}`)
		}},
		{"duplicate", func(e *reliability.Event) {
			e.Payload = json.RawMessage(`{"resource_id":"` + source.ID + `","resource_id":"` + missing + `","operation":"official.save"}`)
		}},
		{"oversized", func(e *reliability.Event) { e.Payload = json.RawMessage(strings.Repeat(" ", 1025)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := event
			tc.change(&bad)
			if e := consumer.consume(t.Context(), bad); !errors.Is(e, ErrIndexEventInvalid) {
				t.Fatal(e)
			}
			if len(indexRows(t, f.repo)) != before {
				t.Fatal("invalid event inserted a task")
			}
		})
	}
}

type stoppingIndexStore struct {
	*MemoryRepository
	stop func()
}

func (s stoppingIndexStore) requestCurrentIndex(ctx context.Context, kind, id string) (IndexRequest, error) {
	r, e := s.MemoryRepository.requestCurrentIndex(ctx, kind, id)
	s.stop()
	return r, e
}
func TestIndexRequestsStopRollsBackAndResumeDeduplicates(t *testing.T) {
	f := fixtureCollections(t)
	c := f.collection(t)
	var consumer *indexRequestConsumer
	store := stoppingIndexStore{MemoryRepository: f.repo, stop: func() { consumer.stop() }}
	var e error
	consumer, e = newIndexRequestConsumer(store, f.s.reliable)
	if e != nil {
		t.Fatal(e)
	}
	event := requestEvent(t, "collection", c.ID)
	if e = consumer.consume(t.Context(), event); !errors.Is(e, ErrIndexConsumerStopped) {
		t.Fatal(e)
	}
	if len(indexRows(t, f.repo)) != 0 {
		t.Fatal("stopped consumer committed a task")
	}
	if e = consumer.consume(t.Context(), event); !errors.Is(e, ErrIndexConsumerStopped) {
		t.Fatal(e)
	}
	consumer.store = f.repo
	consumer.resume()
	if e = consumer.consume(t.Context(), event); e != nil {
		t.Fatal(e)
	}
	if len(indexRows(t, f.repo)) != 1 {
		t.Fatal("resumed intent missing")
	}
}

type indexReceiptFailure struct {
	reliability.Store
	fail bool
}

func (s *indexReceiptFailure) RecordConsumerReceipt(ctx context.Context, r reliability.ConsumerReceipt) error {
	if s.fail && r.ConsumerName == IndexRequestConsumerName {
		return errors.New("owned index receipt failure")
	}
	return s.Store.RecordConsumerReceipt(ctx, r)
}
func TestIndexRequestsOutboxCommitBeforeReceiptFailure(t *testing.T) {
	f := fixtureCollections(t)
	base := f.store.Store
	wrapper := &indexReceiptFailure{Store: base, fail: true}
	reliable := reliability.NewService(transaction.NewMemory(f.repo, base.(transaction.Snapshotter)), wrapper)
	f.s.reliable = reliable
	consumer, e := newIndexRequestConsumer(f.repo, reliable)
	if e != nil {
		t.Fatal(e)
	}
	reliable.RegisterConsumer(selectionChangedEvent, IndexRequestConsumerName, consumer.consume)
	c := f.collection(t)
	if n, e := reliable.ProcessOnce(t.Context()); n != 1 || e == nil {
		t.Fatal("receipt failure was not propagated", n, e)
	}
	rows := indexRows(t, f.repo)
	if len(rows) != 1 || rows[0].TargetVersion != c.Version {
		t.Fatal(rows)
	}
	events, _, e := base.List(t.Context(), reliability.EventFilter{}, reliability.PageRequest{Page: 1, PageSize: 10})
	if e != nil || len(events) != 1 {
		t.Fatal(events, e)
	}
	if ok, e := base.HasConsumerReceipt(t.Context(), IndexRequestConsumerName, events[0].ID); e != nil || ok {
		t.Fatal("receipt falsely recorded", ok, e)
	}
	// A process can retry after the task transaction committed but before ACK.
	wrapper.fail = false
	if e = consumer.consume(t.Context(), events[0]); e != nil {
		t.Fatal(e)
	}
	if len(indexRows(t, f.repo)) != 1 {
		t.Fatal("retry duplicated committed request")
	}
}

func TestIndexRequestsModuleRegistersBeforeWorkerAndRetainsStopBarrier(t *testing.T) {
	f := fixtureCollections(t)
	collection := f.collection(t)
	app := platformmodule.NewAppContext()
	for name, value := range map[string]any{
		"community.content-query":            f.s.query,
		"community.source-category-catalog":  f.cat,
		richtext.PortPublicTextSource:        articleTextFunc(func(context.Context, string) (richtext.PublicTextSource, error) { return articleTextFixture(), nil }),
		portRepository:                       Repository(f.repo),
		"platform.reliability.service":       f.s.reliable,
		"identity.admin-execution-authority": identityport.AdminExecutionAuthorityReader(f.admins),
		"identity.admin-session-authority":   identityport.AdminSessionAuthorityReader(noAdminSession{}),
		"identity.integration-read-gateway":  integrationReadFunc(denyIntegrationRead),
	} {
		if e := app.Provide(name, value); e != nil {
			t.Fatal(e)
		}
	}
	f.s.reliable.SetFallbackHandler(func(context.Context, reliability.Event) error { return nil })
	m := NewModule(func() bool { return true })
	if e := m.Register(app); e != nil {
		t.Fatal(e)
	}
	// No m.Start yet. Production Core starts its worker earlier in StartAll.
	if n, e := f.s.reliable.ProcessOnce(t.Context()); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if len(indexRows(t, f.repo)) != 1 {
		t.Fatal("pre-start event lost to fallback")
	}
	if e := m.Start(t.Context()); e != nil {
		t.Fatal(e)
	}
	if e := m.Stop(t.Context()); e != nil {
		t.Fatal(e)
	}
	collection.Title = "shutdown-window"
	if _, e := f.s.SaveCollection(t.Context(), collection, f.resolve); e != nil {
		t.Fatal(e)
	}
	if n, e := f.s.reliable.ProcessOnce(t.Context()); e == nil || n != 1 {
		t.Fatal("stop callback disappeared and fallback ACKed", n, e)
	}
	if len(indexRows(t, f.repo)) != 1 {
		t.Fatal("stop barrier was ignored")
	}
	if e := m.Start(t.Context()); e != nil {
		t.Fatal(e)
	}
	if m.indexRequests.stopped.Load() {
		t.Fatal("restart did not resume callback")
	}
}
