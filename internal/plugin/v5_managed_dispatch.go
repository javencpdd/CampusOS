package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
)

// Consumer is a compiled host binding. Resource authorization must return a
// nonempty current fact revision. No request field can supply this binding.
type V5Consumer struct {
	Feature   V5ChatFeatureReader
	FeatureID string
	Authorize func(context.Context, identity.PrincipalContext, V5ContributionDescriptor, json.RawMessage) (string, error)
}

// One guard per admitted call (deployment ceiling8). Compiled adapters honor
// cancellation; this is current-fact fencing, not OS-level preemption.
func v5WatchCall(ctx context.Context, cancel context.CancelFunc, check func() error) func() {
	done, stop := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if check() != nil {
					cancel()
					return
				}
			}
		}
	}()
	return func() { close(stop); <-done }
}

type v5BoundedOutput struct {
	mu     sync.Mutex
	ctx    context.Context
	buf    bytes.Buffer
	limit  int
	err    error
	closed bool
}

func (w *v5BoundedOutput) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, ErrV5RuntimeMissing
	}
	if w.err == nil {
		w.err = w.ctx.Err()
	}
	if w.err == nil && len(b) > w.limit-w.buf.Len() {
		w.err = ErrV5ContributionBudget
	}
	if w.err != nil {
		return 0, w.err
	}
	return w.buf.Write(b)
}
func (w *v5BoundedOutput) finish() ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return bytes.Clone(w.buf.Bytes()), w.err
}

func (h *V5ManagedRuntime) invokeProof(ctx context.Context, c V5Consumer, b V5ManagedBinding, payload json.RawMessage, resolve identity.SessionPrincipalResolver, previous *identity.PrincipalContext) (identity.PrincipalContext, string, error) {
	var p identity.PrincipalContext
	if err := ctx.Err(); err != nil {
		return p, "", err
	}
	if c.Feature == nil || c.FeatureID == "" || !c.Feature.Enabled(c.FeatureID) {
		return p, "", ErrV5FeatureDisabled
	}
	if resolve == nil {
		return p, "", ErrV5Denied
	}
	p, err := resolve(ctx)
	if err != nil || p.Validate() != nil || p.Actor.Kind != "user" || b.binding.owner == "" || p.Actor.ID != b.binding.owner {
		return p, "", ErrV5Denied
	}
	if previous != nil && !reflect.DeepEqual(p, *previous) {
		return p, "", ErrV5Denied
	}
	if _, err := h.checkCallBinding(ctx, b, previous != nil); err != nil {
		return p, "", err
	}
	info := b.Descriptor()
	request, err := parseV5Extension(info.Point, payload, false)
	if err != nil {
		return p, "", err
	}
	needsResource := info.Point != V5AIChat || request["context_class"] != "none"
	stamp := ""
	if needsResource || c.Authorize != nil {
		if c.Authorize == nil {
			return p, "", ErrV5Denied
		}
		stamp, err = c.Authorize(ctx, p, info, payload)
		if err != nil || stamp == "" {
			return p, "", ErrV5Denied
		}
	}
	return p, stamp, nil
}

func v5ManagedFacts(i *v5ManagedInstance, g V5Grant, c campusossdk.V5DispatchCall, feature bool) v5DispatchFacts {
	maxTimeout := min(g.Resources.TimeoutMS, int64(v5ChatMaxTimeoutMS))
	return v5DispatchFacts{NowMS: time.Now().UnixMilli(), CurrentGeneration: i.generation, Desired: "enabled", Observed: "ready", GlobalInstalled: true, UserRequired: i.owner != "", UserInstalled: true, Grant: g.Approved, Consent: true, Declared: true, Published: true, Feature: feature, Audience: "host-api", CredentialExpiry: i.expiresAt().UnixMilli(), Selected: c.Target.ID, MaxRequest: V5ChatMaxRequestBytes, MaxResponse: V5ChatMaxResponseBytes, MaxDeadline: time.Now().Add(time.Duration(maxTimeout) * time.Millisecond).UnixMilli()}
}

// Invoke implements the untrusted outer envelope and all three frozen typed
// extension contracts. Context deadlines include admission and response checks.
func (h *V5ManagedRuntime) Invoke(ctx context.Context, consumer V5Consumer, b V5ManagedBinding, envelope, payload []byte, resolve identity.SessionPrincipalResolver) (out json.RawMessage, err error) {
	call, err := ParseV5DispatchCall(envelope)
	if err != nil {
		return nil, err
	}
	if len(payload) > V5ChatMaxRequestBytes || call.RequestBytes != int64(len(payload)) {
		return nil, ErrV5ContributionBudget
	}
	info := b.Descriptor()
	request, err := parseV5Extension(info.Point, payload, false)
	if err != nil {
		return nil, err
	}
	if request["request_id"] != call.RequestID || request["contribution_id"] != call.Target.ID || call.Target.ID != info.ID || v5WireInt(request["instance_generation"]) != call.InstanceGeneration {
		return nil, ErrV5CallTarget
	}
	i, err := h.checkBinding(ctx, b)
	if err != nil {
		return nil, err
	}
	g, err := h.directory.platform.grant(ctx, i.coordinate)
	if err != nil || g.Resources == nil {
		return nil, ErrV5Denied
	}
	if v5WireInt(request["deadline_ms"]) > min(g.Resources.TimeoutMS, int64(v5ChatMaxTimeoutMS)) {
		return nil, ErrV5CallDeadline
	}
	if err := decideV5Dispatch("host_to_plugin", call, v5ManagedFacts(i, g, call, consumer.Feature != nil && consumer.Feature.Enabled(consumer.FeatureID))); err != nil {
		return nil, err
	}
	deadline := time.UnixMilli(call.DeadlineMS)
	localDeadline := time.Now().Add(time.Duration(v5WireInt(request["deadline_ms"])) * time.Millisecond)
	if localDeadline.Before(deadline) {
		deadline = localDeadline
	}
	callCtx, cancel := context.WithDeadline(ctx, deadline)
	defer func() {
		if e := callCtx.Err(); e != nil {
			out = nil
			err = e
			if errors.Is(e, context.DeadlineExceeded) {
				err = ErrV5CallDeadline
			}
		}
		cancel()
	}()
	principal, stamp, err := h.invokeProof(callCtx, consumer, b, payload, resolve, nil)
	if err != nil {
		return nil, err
	}
	leave, err := h.enter(i, g.Resources.MaxConcurrency, cancel)
	if err != nil {
		return nil, err
	}
	defer leave()
	// At most8 guards exist, one per admitted call. Non-plugin account/Feature
	// changes also cancel cooperative adapters; v5 mutations cancel immediately.
	stopGuard := v5WatchCall(callCtx, cancel, func() error {
		_, _, err := h.invokeProof(callCtx, consumer, b, payload, resolve, &principal)
		return err
	})
	defer stopGuard()
	w := &v5BoundedOutput{ctx: callCtx, limit: int(call.ResponseLimitBytes)}
	invokeCtx := v5BindInvocation(callCtx, i, resolve, principal)
	err = i.adapter.Invoke(context.WithValue(invokeCtx, v5NestedCallKey{}, i.id), V5ChatTarget{info, i.owner}, bytes.Clone(payload), w)
	raw, writeErr := w.finish()
	if writeErr != nil {
		return nil, writeErr
	}
	if err != nil {
		return nil, ErrV5RuntimeMissing
	}
	response, err := parseV5Extension(info.Point, raw, true)
	if err != nil {
		return nil, err
	}
	if response["request_id"] != call.RequestID || response["contribution_id"] != call.Target.ID || v5WireInt(response["instance_generation"]) != call.InstanceGeneration {
		return nil, ErrV5CallTarget
	}
	_, after, err := h.invokeProof(callCtx, consumer, b, payload, resolve, &principal)
	if err != nil {
		return nil, err
	}
	if stamp != after {
		return nil, ErrV5ContributionStale
	}
	if info.Point == V5PreviewRenderer && response["resource_revision"] != stamp {
		return nil, ErrV5ContributionStale
	}
	if info.Point == V5KnowledgeSource && int64(len(response["items"].([]any))) > v5WireInt(request["limit"]) {
		return nil, ErrV5ContributionBudget
	}
	return bytes.Clone(raw), nil
}
