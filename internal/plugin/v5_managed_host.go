package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community/port"
)

func (h *V5ManagedRuntime) BindHost(code string, e V5HostEndpoint) error {
	// This finite catalog is a contract allowlist, not a registration wildcard.
	if (code != "community.thread.public.read" && code != V5KnowledgeSearch && code != V5KnowledgeSourceRead && code != V5KnowledgeAnswer && code != V5AIChatLocal && code != V5AIChatRemote) || e.Feature == nil || e.FeatureID == "" || e.Authorize == nil || e.Call == nil {
		return ErrV5CallTarget
	}
	if (code == V5KnowledgeSearch || code == V5KnowledgeSourceRead) && e.ResultStamp == nil {
		return ErrV5CallTarget
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.hosts[code]; ok {
		return ErrV5ContributionConflict
	}
	h.hosts[code] = e
	return nil
}
func (h *V5ManagedRuntime) hostProof(ctx context.Context, token, code string) (*v5ManagedInstance, V5HostEndpoint, PluginV5Consume, V5Grant, error) {
	return h.hostCallProof(ctx, token, code, false)
}

func (h *V5ManagedRuntime) hostCallProof(ctx context.Context, token, code string, admitted bool) (*v5ManagedInstance, V5HostEndpoint, PluginV5Consume, V5Grant, error) {
	var empty V5HostEndpoint
	var none PluginV5Consume
	var grant V5Grant
	i, err := h.credentialForCall(ctx, token, admitted)
	if err != nil {
		return nil, empty, none, grant, err
	}
	h.mu.Lock()
	endpoint, ok := h.hosts[code]
	h.mu.Unlock()
	if !ok {
		return nil, empty, none, grant, ErrV5ContributionUnpublished
	}
	if !endpoint.Feature.Enabled(endpoint.FeatureID) {
		return nil, empty, none, grant, ErrV5FeatureDisabled
	}
	s := h.directory.platform
	release, err := s.release(ctx, i.coordinate)
	if err != nil {
		return nil, empty, none, grant, err
	}
	m, err := ParsePluginV5Manifest(release.Manifest)
	if err != nil {
		return nil, empty, none, grant, err
	}
	grant, err = s.grant(ctx, i.coordinate)
	if err != nil {
		return nil, empty, none, grant, err
	}
	for _, declared := range m.Consumes {
		if declared.Capability != code {
			continue
		}
		for _, approved := range grant.Consumes {
			if reflect.DeepEqual(declared, approved) {
				return i, endpoint, declared, grant, nil
			}
		}
	}
	return nil, empty, none, grant, errors.New("plugin.target_undeclared")
}

// HostCall authenticates an opaque managed instance credential; it never
// accepts a user/admin token or authority fields in either JSON document.
func (h *V5ManagedRuntime) HostCall(ctx context.Context, credential string, envelope, payload []byte) (out json.RawMessage, err error) {
	call, err := ParseV5DispatchCall(envelope)
	if err != nil {
		return nil, err
	}
	if call.Target.Kind != "host_api" {
		return nil, ErrV5CallTarget
	}
	if len(payload) > V5ChatMaxRequestBytes || call.RequestBytes != int64(len(payload)) {
		return nil, ErrV5ContributionBudget
	}
	nestedID, _ := ctx.Value(v5NestedCallKey{}).(string)
	i, endpoint, scope, g, err := h.hostCallProof(ctx, credential, call.Target.ID, nestedID != "")
	if err != nil {
		return nil, err
	}
	if g.Resources == nil {
		return nil, ErrV5Denied
	}
	if err := decideV5Dispatch("plugin_to_host", call, v5ManagedFacts(i, g, call, true)); err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithDeadline(ctx, time.UnixMilli(call.DeadlineMS))
	if invocation, ok := ctx.Value(v5InvocationKey{}).(v5Invocation); ok && invocation.instance == i.id && invocation.owner == i.owner {
		invocation.require = func(c context.Context, code string) (PluginV5Consume, error) {
			_, _, s, _, e := h.hostCallProof(c, credential, code, true)
			return s, e
		}
		callCtx = context.WithValue(callCtx, v5InvocationKey{}, invocation)
	}
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
	stamp, err := endpoint.Authorize(callCtx, i.owner, scope, payload)
	if err != nil || stamp == "" {
		return nil, ErrV5Denied
	}
	leave := func() {}
	if nestedID != "" && nestedID != i.id {
		return nil, ErrV5Denied
	}
	if nestedID == "" {
		leave, err = h.enter(i, g.Resources.MaxConcurrency, cancel)
	}
	if err != nil {
		return nil, err
	}
	defer leave()
	stopGuard := v5WatchCall(callCtx, cancel, func() error {
		if _, _, _, _, err := h.hostCallProof(callCtx, credential, call.Target.ID, true); err != nil {
			return err
		}
		after, err := endpoint.Authorize(callCtx, i.owner, scope, payload)
		if err != nil || after != stamp {
			return ErrV5Denied
		}
		return nil
	})
	defer stopGuard()
	w := &v5BoundedOutput{ctx: callCtx, limit: int(call.ResponseLimitBytes)}
	err = endpoint.Call(callCtx, i.owner, scope, bytes.Clone(payload), w)
	raw, writeErr := w.finish()
	if writeErr != nil {
		return nil, writeErr
	}
	if err != nil {
		return nil, ErrV5RuntimeMissing
	}
	if endpoint.ResultStamp != nil && endpoint.ResultStamp(raw) != stamp {
		return nil, ErrV5Denied
	}
	if _, _, _, _, err := h.hostCallProof(callCtx, credential, call.Target.ID, true); err != nil {
		return nil, err
	}
	after, err := endpoint.Authorize(callCtx, i.owner, scope, payload)
	if err != nil || after != stamp {
		return nil, ErrV5Denied
	}
	if _, ok := v5JSON(raw); !ok {
		return nil, ErrV5CallInvalid
	}
	return raw, nil
}

// Only a protected current public-read Core Port supplies thread data. No raw
// repository, resource ID guess, archived-state predicate or SQL is accepted.
func V5PublicThreadEndpoint(reader community.ContentQuery, feature V5ChatFeatureReader, featureID string) V5HostEndpoint {
	read := func(ctx context.Context, scope PluginV5Consume, raw json.RawMessage) (string, json.RawMessage, error) {
		if reader == nil || scope.Capability != "community.thread.public.read" || scope.Scope.Kind != "public" {
			return "", nil, ErrV5Denied
		}
		v, ok := v5JSON(raw)
		if !ok || !v5Object("thread_id", map[string]v5Check{"thread_id": v5String(1, 19, `^[1-9][0-9]*$`)})(v) {
			return "", nil, ErrV5CallInvalid
		}
		t, err := reader.GetPublicThread(ctx, v.(map[string]any)["thread_id"].(string))
		if err != nil || t == nil || t.FactsVersion < 1 {
			return "", nil, ErrV5Denied
		}
		stamp := v5Counter(t.FactsVersion)
		wire, err := json.Marshal(struct {
			ID       string `json:"thread_id"`
			Category string `json:"category_id"`
			Title    string `json:"title"`
			Revision string `json:"revision"`
		}{t.ID, t.CategoryID, t.Title, stamp})
		return stamp, wire, err
	}
	return V5HostEndpoint{Feature: feature, FeatureID: featureID, Authorize: func(ctx context.Context, _ string, s PluginV5Consume, r json.RawMessage) (string, error) {
		stamp, _, err := read(ctx, s, r)
		return stamp, err
	}, Call: func(ctx context.Context, _ string, s PluginV5Consume, r json.RawMessage, w io.Writer) error {
		_, wire, err := read(ctx, s, r)
		if err != nil {
			return err
		}
		_, err = w.Write(wire)
		return err
	}}
}
