package plugin

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeTrustReadDoesNotWaitForPublicationMutex(t *testing.T) {
	v, _ := newV5ContributionTestPlatform(t)
	c := v.s.catalog
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- c.WithVerified(t.Context(), v.raw, v.envelope, func(*PluginV5Package) error { close(entered); <-release; return nil })
	}()
	<-entered
	readDone := make(chan error, 1)
	go func() {
		readDone <- c.withVerifiedRead(t.Context(), v.raw, v.envelope, func(*PluginV5Package) error { return nil })
	}()
	select {
	case e := <-readDone:
		if e != nil {
			close(release)
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("runtime fact read blocked behind a publication waiting on its domain row")
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestRuntimeTrustReadFencesRevocationAndABA(t *testing.T) {
	for _, mode := range []string{"revoke", "ABA", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			v, _ := newV5ContributionTestPlatform(t)
			c := v.s.catalog
			original := append([]byte(nil), c.trust...)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			e := c.withVerifiedRead(ctx, v.raw, v.envelope, func(*PluginV5Package) error {
				if mode == "cancel" {
					cancel()
					return nil
				}
				if e := c.SetTrust([]byte(`{"contract":"campusos.plugin-trust/v1","keys":[]}`)); e != nil {
					return e
				}
				if mode == "ABA" {
					return c.SetTrust(original)
				}
				return nil
			})
			if e == nil {
				t.Fatal("changed trust returned facts")
			}
			if mode == "revoke" {
				called := false
				if e = c.withVerifiedRead(t.Context(), v.raw, v.envelope, func(*PluginV5Package) error { called = true; return nil }); e == nil || called {
					t.Fatal("revoked key accepted")
				}
			}
		})
	}
}
