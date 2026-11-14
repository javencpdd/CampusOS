package webcompanion

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestHubOneUsePairingAndOwnerIsolation(t *testing.T) {
	h := NewHub()
	ctx := context.Background()
	pair, err := h.Begin("alice-session", func(context.Context) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	token, reader, err := h.Connect(ctx, pair.Code, "Alice PC")
	if err != nil || token == "" {
		t.Fatal(err)
	}
	if _, _, err = h.Connect(ctx, pair.Code, "second"); err != ErrDenied {
		t.Fatal("pair reused", err)
	}
	if len(h.List("bob")) != 0 || h.Disconnect("bob", reader.ID) != ErrDenied {
		t.Fatal("another owner controlled helper")
	}
	if _, err = h.Fetch(ctx, "bob", reader.ID, "https://example.com/feed", 1024, func(context.Context) bool { return true }); err != ErrDenied {
		t.Fatal("another owner dispatched job", err)
	}
	if err = h.Disconnect("alice-session", reader.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Poll(ctx, token); err != ErrDenied {
		t.Fatal("revoked helper polled", err)
	}
}

func TestHubBoundedResultAndCurrentAuthority(t *testing.T) {
	for _, change := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "revoked-during-read"}[change], func(t *testing.T) {
			h := NewHub()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var allowed atomic.Bool
			allowed.Store(true)
			pair, _ := h.Begin("alice", func(context.Context) bool { return true })
			token, reader, _ := h.Connect(ctx, pair.Code, "PC")
			done := make(chan error, 1)
			go func() {
				raw, err := h.Fetch(ctx, "alice", reader.ID, "https://example.com/feed", 4, func(context.Context) bool { return allowed.Load() })
				if err == nil && string(raw) != "feed" {
					t.Error("bytes changed")
				}
				done <- err
			}()
			job, err := h.Poll(ctx, token)
			if err != nil || job == nil {
				t.Fatal("job missing", err)
			}
			if job.URL != "https://example.com/feed" || job.MaxBytes != 4 {
				t.Fatal("job widened")
			}
			if err = h.Complete(ctx, token, job.ID, []byte("oversize"), false); err != ErrDenied {
				t.Fatal("unbounded result accepted")
			}
			if change {
				allowed.Store(false)
			}
			if err = h.Complete(ctx, token, job.ID, []byte("feed"), false); err != nil {
				t.Fatal(err)
			}
			if err = h.Complete(ctx, token, job.ID, []byte("feed"), false); err != ErrDenied {
				t.Fatal("result replayed")
			}
			err = <-done
			if change && err != ErrDenied || !change && err != nil {
				t.Fatal("authority not enforced", err)
			}
		})
	}
}

func TestHubExpiryAndSessionRevocation(t *testing.T) {
	h := NewHub()
	now := time.Now()
	h.now = func() time.Time { return now }
	ctx := context.Background()
	var alive atomic.Bool
	alive.Store(true)
	pair, _ := h.Begin("owner", func(context.Context) bool { return alive.Load() })
	now = now.Add(3 * time.Minute)
	if _, _, err := h.Connect(ctx, pair.Code, "PC"); err != ErrDenied {
		t.Fatal("expired pair connected")
	}
	pair, _ = h.Begin("owner", func(context.Context) bool { return alive.Load() })
	token, _, _ := h.Connect(ctx, pair.Code, "PC")
	alive.Store(false)
	if _, err := h.Poll(ctx, token); err != ErrDenied {
		t.Fatal("revoked originating session polled")
	}
}
