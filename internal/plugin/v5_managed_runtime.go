package plugin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Compiled runtime adapters must honor context and route to the exact instance.
// 03c supports bounded contract adapters;05/06 own OS isolation and hard kill.
type V5ManagedAdapter interface {
	Invoke(context.Context, V5ChatTarget, json.RawMessage, io.Writer) error
	Event(context.Context, json.RawMessage) error
}

type V5RuntimeCredential struct {
	ID        string
	Token     string `json:"-"`
	ExpiresAt time.Time
}
type v5ManagedInstance struct {
	id                           string
	digest                       [32]byte
	coordinate                   V5Coordinate
	owner                        string
	generation, globalGeneration int64
	grant                        string
	expires                      time.Time
	renewedExpiry                atomic.Int64
	adapter                      V5ManagedAdapter
	accepting                    bool
	draining                     bool
	calls                        map[uint64]context.CancelFunc
	idle                         chan struct{}
}

func (i *v5ManagedInstance) expiresAt() time.Time {
	if n := i.renewedExpiry.Load(); n != 0 {
		return time.UnixMilli(n)
	}
	return i.expires
}

type V5ManagedBinding struct {
	binding  V5ContributionBinding
	instance string
	query    V5ContributionQuery
	explicit bool
}

func (b V5ManagedBinding) Descriptor() V5ContributionDescriptor { return b.binding.Descriptor() }

// ManagedRuntime is host-owned publication/credential/call state. It is empty
// after restart. Durable03b desired/observed facts are always checked as well.
type V5ManagedRuntime struct {
	directory *V5ContributionDirectory
	mu        sync.Mutex
	instances map[string]*v5ManagedInstance
	closed    bool
	next      uint64
	total     int
	hosts     map[string]V5HostEndpoint
}

func NewV5ManagedRuntime(directory *V5ContributionDirectory) *V5ManagedRuntime {
	h := &V5ManagedRuntime{directory: directory, instances: map[string]*v5ManagedInstance{}, hosts: map[string]V5HostEndpoint{}}
	if directory != nil && directory.platform != nil {
		directory.platform.managed.Store(h)
	}
	return h
}

func (h *V5ManagedRuntime) Publish(ctx context.Context, c V5Coordinate, owner string, generation int64, ttl time.Duration, adapter V5ManagedAdapter) (V5RuntimeCredential, error) {
	var zero V5RuntimeCredential
	if h == nil || h.directory == nil || h.directory.platform == nil || adapter == nil || ttl <= 0 || ttl > 5*time.Minute {
		return zero, ErrV5RuntimeMissing
	}
	f, err := h.directory.platform.ExecutionFacts(ctx, c, owner)
	if err != nil {
		return zero, err
	}
	if !f.ReadyForDispatch || f.Installation.ActiveGeneration != generation {
		return zero, ErrV5ContributionStale
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return zero, ErrV5PlatformUnavailable
	}
	token := hex.EncodeToString(secret[:])
	digest := sha256.Sum256([]byte(token))
	id := v5RandomID()
	if id == "" {
		return zero, ErrV5PlatformUnavailable
	}
	i := &v5ManagedInstance{id: id, digest: digest, coordinate: c, owner: owner, generation: generation, globalGeneration: f.Global.ActiveGeneration, grant: f.GrantRevision, expires: time.Now().Add(ttl), adapter: adapter, accepting: true, calls: map[uint64]context.CancelFunc{}, idle: make(chan struct{})}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.instances) >= 128 {
		return zero, ErrV5RuntimeMissing
	}
	for _, old := range h.instances {
		if (old.accepting || old.draining) && old.coordinate.family() == c.family() && old.owner == owner {
			return zero, ErrV5ContributionConflict
		}
	}
	credential := V5RuntimeCredential{ID: id, Token: token, ExpiresAt: i.expires}
	if binder, ok := adapter.(v5CredentialBinder); ok {
		binder.BindCredential(credential)
	}
	h.instances[id] = i
	return credential, nil
}

func (h *V5ManagedRuntime) renew(ctx context.Context, id string) error {
	i, err := h.current(ctx, id)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.instances[id] != i || !i.accepting {
		return ErrV5ContributionUnpublished
	}
	i.renewedExpiry.Store(time.Now().Add(5 * time.Minute).UnixMilli())
	return nil
}

func (h *V5ManagedRuntime) current(ctx context.Context, id string) (*v5ManagedInstance, error) {
	return h.currentForCall(ctx, id, false)
}

func (h *V5ManagedRuntime) currentForCall(ctx context.Context, id string, admitted bool) (*v5ManagedInstance, error) {
	if h == nil || h.directory == nil || h.directory.platform == nil {
		return nil, ErrV5RuntimeMissing
	}
	h.mu.Lock()
	i := h.instances[id]
	live := !h.closed && i != nil && time.Now().Before(i.expiresAt())
	draining := live && i.draining && len(i.calls) > 0
	valid := live && (i.accepting || admitted && draining)
	h.mu.Unlock()
	if !valid {
		// A new caller/sweeper cannot turn a requested graceful drain into
		// cancellation. Admitted calls still recheck every current fact.
		if draining {
			return nil, ErrV5ContributionUnpublished
		}
		if i != nil {
			h.retire(id, true)
		}
		return nil, ErrV5ContributionUnpublished
	}
	f, err := h.directory.platform.ExecutionFacts(ctx, i.coordinate, i.owner)
	if err != nil {
		if ctx.Err() == nil && (errors.Is(err, ErrV5Denied) || errors.Is(err, ErrV5NotFound)) {
			h.retire(id, true)
		}
		return nil, err
	}
	if !f.ReadyForDispatch || f.Installation.ActiveGeneration != i.generation || f.Global.ActiveGeneration != i.globalGeneration || f.GrantRevision != i.grant {
		h.retire(id, true)
		return nil, ErrV5ContributionStale
	}
	return i, nil
}

func (h *V5ManagedRuntime) credential(ctx context.Context, token string) (*v5ManagedInstance, error) {
	return h.credentialForCall(ctx, token, false)
}

func (h *V5ManagedRuntime) credentialForCall(ctx context.Context, token string, admitted bool) (*v5ManagedInstance, error) {
	if h == nil || len(token) != 64 {
		return nil, errors.New("plugin.instance_credential_invalid")
	}
	digest := sha256.Sum256([]byte(token))
	id := ""
	h.mu.Lock()
	for _, i := range h.instances {
		if i.digest == digest && (i.accepting || admitted && i.draining && len(i.calls) > 0) {
			id = i.id
			break
		}
	}
	h.mu.Unlock()
	if id == "" {
		return nil, errors.New("plugin.instance_credential_invalid")
	}
	return h.currentForCall(ctx, id, admitted)
}

// Select filters actual managed publications, not merely ready declarations.
func (h *V5ManagedRuntime) Select(ctx context.Context, owner string, q V5ContributionQuery, selected *V5ContributionRef) (V5ManagedBinding, error) {
	var zero V5ManagedBinding
	if !v5OwnerValid(owner) || !v5ContributionQuerySupported(q) {
		return zero, ErrV5ContributionUnsupported
	}
	if h == nil || h.directory == nil {
		return zero, ErrV5RuntimeMissing
	}
	h.mu.Lock()
	ids := []string{}
	for id, i := range h.instances {
		if i.accepting && i.owner == owner {
			ids = append(ids, id)
		}
	}
	h.mu.Unlock()
	count := 0
	var found V5ManagedBinding
	for _, id := range ids {
		i, err := h.current(ctx, id)
		if err != nil {
			continue
		}
		if selected != nil && selected.V5Coordinate != i.coordinate {
			continue
		}
		r, err := h.directory.platform.release(ctx, i.coordinate)
		if err != nil {
			return zero, err
		}
		m, err := ParsePluginV5Manifest(r.Manifest)
		if err != nil {
			return zero, err
		}
		for _, p := range m.Provides {
			if p.Point != string(q.Point) || p.ContractVersion != q.ContractVersion || !v5Contains(p.Capabilities, q.Capability) {
				continue
			}
			ref := V5ContributionRef{i.coordinate, p.ID}
			if selected != nil && *selected != ref {
				continue
			}
			b := V5ContributionBinding{descriptor: V5ContributionDescriptor{V5ContributionRef: ref, Point: q.Point, ContractVersion: p.ContractVersion, Capabilities: append([]string(nil), p.Capabilities...), DataMode: p.DataMode, InstanceGeneration: i.generation, GlobalGeneration: i.globalGeneration}, owner: owner, query: q, explicit: true, grantRevision: i.grant}
			count++
			found = V5ManagedBinding{b, id, q, selected != nil}
			if count > 1 {
				return zero, ErrV5ContributionConflict
			}
		}
	}
	if count == 0 {
		return zero, ErrV5ContributionUnpublished
	}
	return found, nil
}

func (h *V5ManagedRuntime) checkBinding(ctx context.Context, b V5ManagedBinding) (*v5ManagedInstance, error) {
	return h.checkCallBinding(ctx, b, false)
}

func (h *V5ManagedRuntime) checkCallBinding(ctx context.Context, b V5ManagedBinding, admitted bool) (*v5ManagedInstance, error) {
	i, err := h.currentForCall(ctx, b.instance, admitted)
	if err != nil {
		return nil, err
	}
	info := b.Descriptor()
	if info.V5Coordinate != i.coordinate || b.binding.owner != i.owner || info.InstanceGeneration != i.generation || info.GlobalGeneration != i.globalGeneration || b.binding.grantRevision != i.grant {
		return nil, ErrV5ContributionStale
	}
	h.mu.Lock()
	draining := i.draining
	h.mu.Unlock()
	if !b.explicit && !(admitted && draining) {
		current, err := h.Select(ctx, b.binding.owner, b.query, nil)
		if err != nil {
			return nil, err
		}
		if current.instance != b.instance || current.Descriptor().V5ContributionRef != b.Descriptor().V5ContributionRef {
			return nil, ErrV5ContributionStale
		}
	}
	return i, nil
}

func (h *V5ManagedRuntime) enter(i *v5ManagedInstance, limit int64, cancel context.CancelFunc) (func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || !i.accepting || !time.Now().Before(i.expiresAt()) {
		return nil, ErrV5ContributionUnpublished
	}
	if limit < 1 || int64(len(i.calls)) >= limit || h.total >= 8 {
		return nil, ErrV5ContributionBudget
	}
	h.next++
	n := h.next
	i.calls[n] = cancel
	h.total++
	return func() {
		h.mu.Lock()
		defer func() {
			abort := !i.accepting && len(i.calls) == 0
			h.mu.Unlock()
			if abort {
				if a, ok := i.adapter.(v5RuntimeAborter); ok {
					a.Abort()
				}
			}
		}()
		delete(i.calls, n)
		h.total--
		if !i.accepting && len(i.calls) == 0 {
			select {
			case <-i.idle:
			default:
				close(i.idle)
			}
			delete(h.instances, i.id)
		}
	}, nil
}

// Withdrawal marks admission/credentials/publications closed before cancelling
// or waiting for any in-flight adapter IO.
func (h *V5ManagedRuntime) retire(id string, cancel bool) <-chan struct{} {
	h.mu.Lock()
	i := h.instances[id]
	defer func() {
		abort := i != nil && (cancel || len(i.calls) == 0)
		h.mu.Unlock()
		if abort {
			if a, ok := i.adapter.(v5RuntimeAborter); ok {
				a.Abort()
			}
		}
	}()
	if i == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	if i.accepting {
		i.draining = !cancel
	} else if cancel {
		i.draining = false
	}
	i.accepting = false
	if cancel {
		for _, stop := range i.calls {
			stop()
		}
	}
	if len(i.calls) == 0 {
		select {
		case <-i.idle:
		default:
			close(i.idle)
		}
		delete(h.instances, id)
	}
	return i.idle
}
func (h *V5ManagedRuntime) Withdraw(ctx context.Context, id string, cancel bool) error {
	done := h.retire(id, cancel)
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.New("plugin.lifecycle_drain_incomplete")
	}
}
func (h *V5ManagedRuntime) withdrawFamily(family, owner string) {
	h.mu.Lock()
	ids := []string{}
	for id, i := range h.instances {
		if i.coordinate.family() == family && (owner == "" || i.owner == owner) {
			ids = append(ids, id)
		}
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.retire(id, true)
	}
}
func (h *V5ManagedRuntime) Sweep(ctx context.Context) {
	h.mu.Lock()
	ids := []string{}
	for id := range h.instances {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		_, _ = h.current(ctx, id)
	}
}
func (h *V5ManagedRuntime) Close(ctx context.Context) error {
	h.mu.Lock()
	h.closed = true
	ids := []string{}
	for id := range h.instances {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	var err error
	for _, id := range ids {
		if e := h.Withdraw(ctx, id, true); e != nil {
			err = e
		}
	}
	return err
}

// Endpoint is a compiled typed Host API adapter. Never registered by a plugin.
type V5HostEndpoint struct {
	Feature   V5ChatFeatureReader
	FeatureID string
	Authorize func(context.Context, string, PluginV5Consume, json.RawMessage) (string, error)
	Call      func(context.Context, string, PluginV5Consume, json.RawMessage, io.Writer) error
	// Optional compiled result proof binds the buffered bytes to Authorize.
	// Knowledge requires it so an intermediate result cannot survive an ABA.
	ResultStamp func(json.RawMessage) string
}
