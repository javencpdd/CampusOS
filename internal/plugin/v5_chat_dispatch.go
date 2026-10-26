package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sync"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
)

const (
	V5ChatMaxRequestBytes  = 64 * 1024
	V5ChatMaxResponseBytes = 256 * 1024
	v5ChatMaxConcurrency   = 8
	v5ChatMaxTimeoutMS     = V5ModelMaxTimeoutMS
)

var (
	ErrV5CallInvalid     = errors.New("plugin.call_invalid")
	ErrV5CallTarget      = errors.New("plugin.call_target_mismatch")
	ErrV5CallDeadline    = errors.New("plugin.call_deadline")
	ErrV5FeatureDisabled = errors.New("plugin.feature_disabled")
	ErrV5RuntimeMissing  = errors.New("plugin.runtime_unavailable")
)

type V5ChatFeatureReader interface{ Enabled(string) bool }

// Target is host-to-adapter routing state, never a plugin-supplied credential.
// The adapter must route to this exact owner/release/generation, honor ctx and
// write a bounded v1 JSON response. It receives no session, config or Secret.
type V5ChatTarget struct {
	Contribution V5ContributionDescriptor
	Owner        string
}

// A compiled transport owns cancellation of its underlying IO. Dispatcher does
// not launch an unbounded goroutine or pretend it can preempt plugin code.
// Real process isolation, runtime publication and hard kill belong to05/06.
type V5ChatTransport interface {
	Chat(context.Context, V5ChatTarget, campusossdk.V5ChatRequest, io.Writer) error
}

// V5ChatDispatcher forwards only a current user's own explicit prompt with
// context_class=none. No host resource reader or built-in AI/RAG is involved.
// Production supplies no transport until a real managed adapter is available.
type V5ChatDispatcher struct {
	directory *V5ContributionDirectory
	features  V5ChatFeatureReader
	featureID string
	transport V5ChatTransport
	mu        sync.Mutex
	active    map[string]int64
	total     int
}

func NewV5ChatDispatcher(directory *V5ContributionDirectory, features V5ChatFeatureReader, consumerFeatureID string, transport V5ChatTransport) *V5ChatDispatcher {
	return &V5ChatDispatcher{directory: directory, features: features, featureID: consumerFeatureID, transport: transport, active: map[string]int64{}}
}

func parseV5ChatRequest(raw []byte) (campusossdk.V5ChatRequest, error) {
	var out campusossdk.V5ChatRequest
	v, ok := v5JSON(raw)
	if !ok || !v5Object("request_id contribution_id instance_generation deadline_ms prompt context_class", map[string]v5Check{
		"request_id": v5String(1, 128, `(?s)^.*$`), "contribution_id": v5String(1, 128, `(?s)^.*$`),
		"instance_generation": v5Integer(1, math.MaxInt64), "deadline_ms": v5Integer(1, 300000),
		"prompt": v5String(1, 12000, `(?s)^.*$`), "context_class": v5Enum("none"),
	})(v) {
		return out, ErrV5CallInvalid
	}
	wire, err := json.Marshal(v5CanonicalNumbers(v))
	if err != nil || json.Unmarshal(wire, &out) != nil {
		return out, ErrV5CallInvalid
	}
	return out, nil
}

func parseV5ChatResponse(raw []byte) (campusossdk.V5ChatResponse, error) {
	var out campusossdk.V5ChatResponse
	v, ok := v5JSON(raw)
	if !ok || !v5Object("request_id contribution_id instance_generation text input_tokens output_tokens finish_reason", map[string]v5Check{
		"request_id": v5String(1, 128, `(?s)^.*$`), "contribution_id": v5String(1, 128, `(?s)^.*$`),
		"instance_generation": v5Integer(1, math.MaxInt64), "text": v5String(0, 50000, `(?s)^.*$`),
		"input_tokens": v5Integer(0, math.MaxInt64), "output_tokens": v5Integer(0, math.MaxInt64),
		"finish_reason": v5Enum("completed", "length"),
	})(v) {
		return out, ErrV5CallInvalid
	}
	wire, err := json.Marshal(v5CanonicalNumbers(v))
	if err != nil || json.Unmarshal(wire, &out) != nil {
		return out, ErrV5CallInvalid
	}
	return out, nil
}

type v5ChatOutput struct {
	ctx context.Context
	buf bytes.Buffer
	err error
}

func (w *v5ChatOutput) Write(p []byte) (int, error) {
	if w.err == nil {
		w.err = w.ctx.Err()
	}
	if w.err == nil && len(p) > V5ChatMaxResponseBytes-w.buf.Len() {
		w.err = ErrV5ContributionBudget
	}
	if w.err != nil {
		return 0, w.err
	}
	return w.buf.Write(p)
}

func (d *V5ChatDispatcher) checked(ctx context.Context, b V5ContributionBinding, resolve identity.SessionPrincipalResolver, previous *v5Actor) (v5Actor, error) {
	var zero v5Actor
	if d == nil || d.directory == nil || d.directory.platform == nil || d.features == nil || d.featureID == "" || d.transport == nil {
		return zero, ErrV5RuntimeMissing
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return zero, ErrV5CallDeadline
		}
		return zero, err
	}
	s := d.directory.platform
	a, err := s.actor(ctx, false, "read", resolve, false)
	if err != nil || b.owner == "" || a.p.Actor.ID != b.owner {
		return zero, ErrV5Denied
	}
	if previous != nil {
		if err := s.recheck(ctx, *previous, "read", resolve); err != nil {
			return zero, ErrV5Denied
		}
	}
	if !d.features.Enabled(d.featureID) {
		return zero, ErrV5FeatureDisabled
	}
	if err := d.directory.Recheck(ctx, b); err != nil {
		return zero, err
	}
	return a, nil
}

// Chat accepts the frozen extension DTO, not the G1 decision oracle's trusted
// fields. The extension deadline_ms is a remaining duration (<=300000); ctx
// carries the absolute host deadline. Grant/deployment ceilings can lower it.
func (d *V5ChatDispatcher) Chat(ctx context.Context, b V5ContributionBinding, raw []byte, resolve identity.SessionPrincipalResolver) (response campusossdk.V5ChatResponse, err error) {
	zero := campusossdk.V5ChatResponse{}
	if len(raw) > V5ChatMaxRequestBytes {
		return zero, ErrV5ContributionBudget
	}
	req, err := parseV5ChatRequest(raw)
	if err != nil {
		return zero, err
	}
	if d == nil || d.directory == nil || d.features == nil || d.featureID == "" || d.transport == nil {
		return zero, ErrV5RuntimeMissing
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(req.DeadlineMS)*time.Millisecond)
	defer func() {
		if expired := callCtx.Err(); expired != nil {
			response, err = zero, expired
			if errors.Is(expired, context.DeadlineExceeded) {
				err = ErrV5CallDeadline
			}
		}
		cancel()
	}()
	info := b.Descriptor()
	if info.Point != V5AIChat || info.ContractVersion != "v1" || req.ContributionID != info.ID || (info.DataMode != "public_only" && info.DataMode != "no_host_private_data") {
		return zero, ErrV5CallTarget
	}
	if req.InstanceGeneration != info.InstanceGeneration {
		return zero, ErrV5ContributionStale
	}
	a, err := d.checked(callCtx, b, resolve, nil)
	if err != nil {
		return zero, err
	}
	g, err := d.directory.platform.grant(callCtx, info.V5Coordinate)
	if err != nil || !g.Approved || g.Resources == nil || v5Counter(g.Revision) != b.grantRevision {
		return zero, ErrV5Denied
	}
	if req.DeadlineMS > g.Resources.TimeoutMS || req.DeadlineMS > v5ChatMaxTimeoutMS {
		return zero, ErrV5CallDeadline
	}
	key := v5VersionOwnerID(info.V5Coordinate, b.owner) + "#" + v5Counter(info.InstanceGeneration)
	d.mu.Lock()
	if d.total >= v5ChatMaxConcurrency || d.active[key] >= g.Resources.MaxConcurrency || g.Resources.MaxConcurrency < 1 {
		d.mu.Unlock()
		return zero, ErrV5ContributionBudget
	}
	d.total++
	d.active[key]++
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.total--
		d.active[key]--
		if d.active[key] == 0 {
			delete(d.active, key)
		}
		d.mu.Unlock()
	}()
	if _, err := d.checked(callCtx, b, resolve, &a); err != nil {
		return zero, err
	}
	output := &v5ChatOutput{ctx: callCtx}
	err = d.transport.Chat(callCtx, V5ChatTarget{Contribution: info, Owner: b.owner}, req, output)
	// Never release a stale/late response, including after transport returns an
	// error. Checks run after IO without holding a database transaction over IO.
	if _, checkErr := d.checked(callCtx, b, resolve, &a); checkErr != nil {
		return zero, checkErr
	}
	if output.err != nil {
		return zero, output.err
	}
	if err != nil {
		return zero, ErrV5RuntimeMissing
	}
	response, err = parseV5ChatResponse(output.buf.Bytes())
	if err != nil {
		return zero, err
	}
	if response.RequestID != req.RequestID || response.ContributionID != req.ContributionID || response.InstanceGeneration != req.InstanceGeneration {
		return zero, ErrV5CallTarget
	}
	return response, nil
}
