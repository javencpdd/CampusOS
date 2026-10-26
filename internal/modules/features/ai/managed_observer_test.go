package ai

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManagedProofWatcherStopIsNotRevocation(t *testing.T) {
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	reading := make(chan struct{})
	done := make(chan struct{})
	reason := make(chan error, 1)
	cancelled := false
	go func() {
		defer close(done)
		monitorManagedProof(ctx, func(c context.Context) error { close(reading); <-c.Done(); return ErrManagedConflict }, reason, func() { cancelled = true })
	}()
	select {
	case <-reading:
	case <-time.After(time.Second):
		t.Fatal("observer did not enter current-fact read")
	}
	stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("observer did not stop")
	}
	if len(reason) != 0 || cancelled {
		t.Fatal("normal observer stop became a false model/authority failure")
	}
}
func TestManagedProofWatcherCurrentFailureCancels(t *testing.T) {
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	done := make(chan struct{})
	reason := make(chan error, 1)
	cancelled := false
	go func() {
		defer close(done)
		monitorManagedProof(ctx, func(context.Context) error { return ErrManagedDenied }, reason, func() { cancelled = true })
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("current authority failure did not cancel")
	}
	if !cancelled || len(reason) != 1 || !errors.Is(<-reason, ErrManagedDenied) {
		t.Fatal("real authority failure was suppressed")
	}
}
