package plugin

import (
	"context"
	"encoding/json"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"io"
	"sync"
	"testing"
)

// Contract-only fakes here; actual signed Wasm + native isolation + PG are
// separate required target tests. No fake is registered in production.
type v5CoordinatorTestLeader struct{ lost bool }

func (l *v5CoordinatorTestLeader) Check(context.Context) error {
	if l.lost {
		return ErrV5RuntimeMissing
	}
	return nil
}
func (l *v5CoordinatorTestLeader) Release(context.Context) error { return nil }

type v5CoordinatorTestRun struct {
	done  chan struct{}
	once  sync.Once
	token string
}

func (r *v5CoordinatorTestRun) BindCredential(c V5RuntimeCredential) { r.token = c.Token }
func (r *v5CoordinatorTestRun) Invoke(context.Context, V5ChatTarget, json.RawMessage, io.Writer) error {
	return nil
}
func (r *v5CoordinatorTestRun) Event(context.Context, json.RawMessage) error { return nil }
func (r *v5CoordinatorTestRun) Abort()                                       { r.once.Do(func() { close(r.done) }) }
func (r *v5CoordinatorTestRun) Done() <-chan struct{}                        { return r.done }
func (r *v5CoordinatorTestRun) Close(context.Context) error                  { r.Abort(); return nil }

type v5CoordinatorTestFactory struct {
	t     *testing.T
	run   *v5CoordinatorTestRun
	fail  bool
	calls int
}

func (f *v5CoordinatorTestFactory) Prepare(ctx context.Context, spec V5RuntimeSpec, _ V5RuntimeBroker) (V5RunningRuntime, error) {
	if transaction.Active(ctx) {
		f.t.Fatal("guest IO crossed a database transaction")
	}
	f.calls++
	if spec.Effective.MaxConcurrency != 1 || spec.Generation < 1 || spec.Program.Digest() == "" {
		f.t.Fatal("unverified runner input")
	}
	if f.fail {
		return nil, ErrV5RuntimeMissing
	}
	return f.run, nil
}

func TestV5WasmCoordinatorReadinessPublicationAndWithdrawal(t *testing.T) {
	ctx := context.Background()
	v, _ := newV5ContributionTestPlatform(t)
	i := v5ContributionApproved(t, v)
	op := v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin)
	h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
	l := &v5CoordinatorTestLeader{}
	f := &v5CoordinatorTestFactory{t: t, run: &v5CoordinatorTestRun{done: make(chan struct{})}}
	c := NewV5RuntimeCoordinator(v.s, h, f, l)
	defer c.Close(ctx)
	v.process(t)
	if _, err := h.Select(ctx, "", v5ContributionQueries()[1], nil); err == nil {
		t.Fatal("pending runtime published")
	}
	if _, err := c.ProcessOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Select(ctx, "", v5ContributionQueries()[1], nil); err != nil || len(f.run.token) != 64 {
		t.Fatal("ready runtime not credential-bound/published", err)
	}
	var current V5Operation
	if err := v.s.store.load(ctx, v5Operation, op.ID, &current); err != nil || current.Status != "done" {
		t.Fatal("observation not durable", err)
	}
	if _, err := c.ProcessOnce(ctx); err != nil || f.calls != 1 {
		t.Fatal("duplicate runner", f.calls, err)
	}
	i, _ = v.s.installation(ctx, v.c, "")
	v.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: i.Generation, IdempotencyKey: "disable"}, true, v.admin)
	select {
	case <-f.run.done:
	default:
		t.Fatal("committed stop did not abort runner immediately")
	}
	if _, err := h.Select(ctx, "", v5ContributionQueries()[1], nil); err == nil {
		t.Fatal("stopped contribution survived")
	}
}

func TestV5WasmCoordinatorFailureAndLostLeadership(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "leadership-loss", true: "readiness-failed"}[failed], func(t *testing.T) {
			ctx := context.Background()
			v, _ := newV5ContributionTestPlatform(t)
			i := v5ContributionApproved(t, v)
			v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin)
			h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
			l := &v5CoordinatorTestLeader{}
			f := &v5CoordinatorTestFactory{t: t, fail: failed, run: &v5CoordinatorTestRun{done: make(chan struct{})}}
			c := NewV5RuntimeCoordinator(v.s, h, f, l)
			defer c.Close(ctx)
			v.process(t)
			if _, err := c.ProcessOnce(ctx); err != nil {
				t.Fatal(err)
			}
			if failed {
				i, _ = v.s.installation(ctx, v.c, "")
				if i.Observed != "failed" || i.ActiveGeneration != 0 {
					t.Fatal("failed readiness became ready")
				}
				return
			}
			l.lost = true
			if _, err := c.ProcessOnce(ctx); err == nil {
				t.Fatal("lost exclusive lease accepted")
			}
			select {
			case <-f.run.done:
			default:
				t.Fatal("lost lease did not stop native runtime")
			}
			if _, err := h.Select(ctx, "", v5ContributionQueries()[1], nil); err == nil {
				t.Fatal("lease loss left publication live")
			}
		})
	}
}
