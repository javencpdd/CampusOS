package knowledge

import (
	"context"
	"runtime"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type privateRAGLifecycleModel struct{}

func (privateRAGLifecycleModel) PlanPrivateLocal(context.Context, identityport.SessionPrincipalResolver, int64) (PrivateLocalModelPlan, error) {
	return PrivateLocalModelPlan{}, ErrRAGUnavailable
}
func (privateRAGLifecycleModel) AnswerLocal(context.Context, identityport.SessionPrincipalResolver, string, int64, string, func(context.Context) error) (LocalModelAnswer, error) {
	return LocalModelAnswer{}, ErrRAGUnavailable
}

func TestPrivateLocalRAGConcurrentRestartDrainsBeforeNewLifetime(t *testing.T) {
	s := newPrivateLocalRAG(&PrivateIndexService{}, privateRAGLifecycleModel{})
	ctx, release, e := s.begin(t.Context(), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	stopped := make(chan struct{})
	go func() { s.stop(); close(stopped) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel old lifetime")
	}
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		closed := s.stopped
		s.mu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stop did not reject new calls")
		}
		runtime.Gosched()
	}
	if _, _, e = s.begin(t.Context(), time.Minute); e == nil {
		t.Fatal("accepted a call while draining")
	}
	restarted := make(chan struct{})
	go func() { s.start(); close(restarted) }()
	// Complete the old call only after restart is concurrent with draining.
	release()
	<-stopped
	<-restarted
	fresh, finish, e := s.begin(t.Context(), time.Minute)
	if e != nil || fresh.Err() != nil {
		t.Fatal("restart reused canceled lifetime", e)
	}
	finish()
	s.stop()
}
