package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
)

type V5MetadataAuthorizer func(context.Context, string, string) (bool, error)
type V5EventService struct {
	hub       *V5ManagedRuntime
	queue     V5EventStore
	authorize V5MetadataAuthorizer
	now       func() time.Time
}

func NewV5EventService(hub *V5ManagedRuntime, queue V5EventStore, authorize V5MetadataAuthorizer) *V5EventService {
	s := &V5EventService{hub: hub, queue: queue, authorize: authorize, now: time.Now}
	if hub != nil && hub.directory != nil && hub.directory.platform != nil {
		if snapshot, ok := queue.(transaction.Snapshotter); ok {
			hub.directory.platform.reliable.RegisterMemorySnapshotters(snapshot)
		}
	}
	return s
}

func validV5SafeEvent(e campusossdk.V5SafeEvent) bool {
	wire, err := json.Marshal(e)
	if err != nil || len(wire) > 4096 {
		return false
	}
	v, ok := v5JSON(wire)
	return ok && v5Object("contract event_id type revision created_at_ms expires_at_ms payload", map[string]v5Check{
		"contract": v5Enum("campusos.plugin-event/v1"), "event_id": v5String(1, 128, `(?s)^.*$`),
		"type":     v5Enum("plugin.lifecycle.changed/v1", "community.thread.public.changed/v1", "knowledge.source.public.changed/v1"),
		"revision": v5Integer(1, math.MaxInt64), "created_at_ms": v5Integer(1, math.MaxInt64), "expires_at_ms": v5Integer(1, math.MaxInt64),
		"payload": v5Object("resource_ref state", map[string]v5Check{"resource_ref": v5String(1, 256, `^[a-z][a-z0-9.]+:[a-zA-Z0-9_-]{1,128}$`), "state": v5Enum("available", "changed", "removed", "disabled")}),
	})(v) && e.ExpiresAtMS > e.CreatedAtMS && e.ExpiresAtMS-e.CreatedAtMS <= 300000
}

func v5EventCapability(kind string) string {
	switch kind {
	case "plugin.lifecycle.changed/v1":
		return "plugin.lifecycle.subscribe"
	case "community.thread.public.changed/v1":
		return "community.thread.public.subscribe"
	case "knowledge.source.public.changed/v1":
		return "knowledge.source.public.subscribe"
	}
	return ""
}
func (s *V5EventService) subscribed(ctx context.Context, i *v5ManagedInstance, event campusossdk.V5SafeEvent) bool {
	p := s.hub.directory.platform
	r, err := p.release(ctx, i.coordinate)
	if err != nil {
		return false
	}
	m, err := ParsePluginV5Manifest(r.Manifest)
	if err != nil {
		return false
	}
	g, err := p.grant(ctx, i.coordinate)
	if err != nil || !g.Approved {
		return false
	}
	code := v5EventCapability(event.Type)
	for _, d := range m.Consumes {
		if d.Capability == code && d.Scope.Kind == "public" {
			for _, a := range g.Consumes {
				if reflect.DeepEqual(a, d) {
					return true
				}
			}
		}
	}
	return false
}
func (s *V5EventService) authorized(ctx context.Context, i *v5ManagedInstance, event campusossdk.V5SafeEvent) bool {
	if event.Type == "plugin.lifecycle.changed/v1" {
		var op V5Operation
		id := strings.TrimPrefix(event.Payload.ResourceRef, "plugin:")
		if s.hub.directory.platform.store.load(ctx, v5Operation, id, &op) != nil {
			return false
		}
		return op.Owner == "" || op.Owner == i.owner && op.family() == i.coordinate.family()
	}
	if s.authorize == nil {
		return false
	}
	allowed, err := s.authorize(ctx, event.Type, event.Payload.ResourceRef)
	return err == nil && allowed
}

// CaptureCommitted is a post-commit adapter, not a boolean supplied by a caller.
// It fetches the durable event by ID under a checked read. Raw payload/headers
// are never copied into this independent queue or a runtime callback.
func (s *V5EventService) CaptureCommitted(ctx context.Context, id string) error {
	if s == nil || s.hub == nil || s.hub.directory == nil || s.hub.directory.platform == nil || s.queue == nil {
		return ErrV5PlatformUnavailable
	}
	if transaction.Active(ctx) {
		return errors.New("plugin.event_uncommitted")
	}
	p := s.hub.directory.platform
	var source *reliability.Event
	if err := p.reliable.CheckedRead(ctx, func(tx context.Context) error {
		var err error
		source, err = p.reliable.Store().Get(tx, id)
		return err
	}); err != nil {
		return errors.New("plugin.event_uncommitted")
	}
	kind, resource := "", ""
	switch source.Type {
	case "plugin.lifecycle.changed":
		if source.AggregateType != "plugin_v5_operation" {
			return nil
		}
		kind = "plugin.lifecycle.changed/v1"
		resource = "plugin:" + source.AggregateID
	case "thread.created", "thread.updated", "thread.deleted":
		if source.AggregateType != "thread" {
			return nil
		}
		kind = "community.thread.public.changed/v1"
		resource = "thread:" + source.AggregateID
	case "knowledge.source.public.changed":
		if source.AggregateType != "knowledge_source" {
			return nil
		}
		kind = "knowledge.source.public.changed/v1"
		resource = "knowledge.source:" + source.AggregateID
	default:
		return nil
	}
	revision, err := strconv.ParseInt(source.ID, 10, 64)
	if err != nil || revision < 1 {
		return errors.New("plugin.event_invalid")
	}
	event := campusossdk.V5SafeEvent{Contract: "campusos.plugin-event/v1", EventID: source.ID, Type: kind, Revision: revision, CreatedAtMS: source.CreatedAt.UnixMilli(), ExpiresAtMS: source.CreatedAt.Add(5 * time.Minute).UnixMilli(), Payload: campusossdk.V5SafeEventPayload{ResourceRef: resource, State: "changed"}}
	if !validV5SafeEvent(event) {
		return errors.New("plugin.event_invalid")
	}
	if s.now().UnixMilli() < event.CreatedAtMS || s.now().UnixMilli() >= event.ExpiresAtMS {
		return nil
	}
	s.hub.mu.Lock()
	ids := []string{}
	for id, i := range s.hub.instances {
		if i.accepting {
			ids = append(ids, id)
		}
	}
	s.hub.mu.Unlock()
	deliveries := []V5EventDelivery{}
	for _, id := range ids {
		i, err := s.hub.current(ctx, id)
		if err != nil || !s.subscribed(ctx, i, event) || !s.authorized(ctx, i, event) {
			continue
		}
		deliveries = append(deliveries, V5EventDelivery{V5Coordinate: i.coordinate, ID: v5RandomID(), Source: source.ID, Instance: i.id, Owner: i.owner, Generation: i.generation, Event: event, Status: "queued", NextAt: s.now().UnixMilli()})
	}
	if len(deliveries) == 0 {
		return nil
	}
	return p.reliable.Execute(ctx, reliability.Command{Code: "plugin.v5.metadata.capture", ActorType: "system", ActorID: "plugin.v5.events", ResourceType: "plugin_v5_event", ResourceID: source.ID, OperationCode: "plugin.v5.metadata.capture"}, func(tx context.Context) error {
		for _, d := range deliveries {
			if err := s.queue.Enqueue(tx, d); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *V5EventService) Register() {
	if s == nil || s.hub == nil || s.hub.directory == nil || s.hub.directory.platform == nil {
		return
	}
	for _, kind := range []string{"plugin.lifecycle.changed", "thread.created", "thread.updated", "thread.deleted", "knowledge.source.public.changed"} {
		s.hub.directory.platform.reliable.RegisterConsumer(kind, "plugin.v5.metadata", func(ctx context.Context, e reliability.Event) error { return s.CaptureCommitted(ctx, e.ID) })
	}
}

// ProcessOnce is invoked by the independent plugin worker, never by a Core
// transaction or the core outbox consumer. Retries/leases live only here.
func (s *V5EventService) ProcessOnce(ctx context.Context, limit int) (int, error) {
	if s == nil || s.queue == nil || s.hub == nil || s.hub.directory == nil || s.hub.directory.platform == nil {
		return 0, ErrV5PlatformUnavailable
	}
	if transaction.Active(ctx) {
		return 0, errors.New("plugin.event_uncommitted")
	}
	if limit < 1 || limit > 50 {
		limit = 16
	}
	var rows []V5EventDelivery
	var err error
	if _, memory := s.queue.(*MemoryV5EventStore); memory {
		// Serialize only memory claim/ACK with snapshot rollback. Callback IO
		// stays outside this transaction; PostgreSQL keeps its atomic SQL lease.
		err = s.hub.directory.platform.reliable.CheckedRead(ctx, func(tx context.Context) error { rows, err = s.queue.Claim(tx, s.now().UnixMilli(), limit); return err })
	} else {
		rows, err = s.queue.Claim(ctx, s.now().UnixMilli(), limit)
	}
	if err != nil {
		return 0, err
	}
	for _, d := range rows {
		status, code := "done", ""
		now := s.now().UnixMilli()
		i, err := s.hub.current(ctx, d.Instance)
		if !validV5SafeEvent(d.Event) {
			status, code = "dead", "plugin.event_invalid"
		} else if now >= d.Event.ExpiresAtMS {
			status, code = "dead", "plugin.event_expired"
		} else if err != nil || i == nil || i.generation != d.Generation || i.coordinate != d.V5Coordinate || i.owner != d.Owner {
			status, code = "dead", "plugin.event_stale_generation"
		} else if !s.subscribed(ctx, i, d.Event) || !s.authorized(ctx, i, d.Event) {
			status, code = "dead", "plugin.event_grant_revoked"
		} else {
			callCtx, cancel := context.WithTimeout(ctx, time.Second)
			leave, admitErr := s.hub.enter(i, 1, cancel)
			if admitErr == nil {
				wire, _ := json.Marshal(d.Event)
				admitErr = i.adapter.Event(context.WithValue(callCtx, v5NestedCallKey{}, i.id), wire)
				if callCtx.Err() != nil {
					admitErr = callCtx.Err()
				}
				leave()
			}
			cancel()
			if admitErr != nil {
				status, code = "retry", "plugin.event_runtime_unavailable"
				if d.Attempts >= 5 {
					status, code = "dead", "plugin.event_dead_letter"
				}
			}
		}
		finish := func(tx context.Context) error {
			return s.queue.Finish(tx, d, s.now().UnixMilli(), status, code, s.now().Add(time.Duration(d.Attempts)*time.Second).UnixMilli())
		}
		if _, memory := s.queue.(*MemoryV5EventStore); memory {
			err = s.hub.directory.platform.reliable.CheckedRead(ctx, finish)
		} else {
			err = finish(ctx)
		}
		if err != nil {
			return len(rows), err
		}
	}
	return len(rows), nil
}
