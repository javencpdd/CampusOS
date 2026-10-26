package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestIndexAdminDiagnosticsAndFencedRetry(t *testing.T) {
	f, executor, c := indexFixture(t, 2)
	indexDrain(t, f, executor, c.ID)
	d, e := f.s.IndexDiagnostics(t.Context(), c.ID, f.resolve)
	if e != nil || d.State != "idle" || d.ProjectedDocuments != 3 || d.ActiveSelections != 1 || d.Retryable {
		t.Fatal(d, e)
	}
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "lease_token") || strings.Contains(string(raw), "chunks") {
		t.Fatal("internal data escaped")
	}
	setFailed := func() IndexJob {
		t.Helper()
		j := indexJob(t, f, c.ID)
		j.State = "dead"
		j.Attempts = 8
		j.LastError = "page_failed"
		e := f.s.reliable.CheckedRead(t.Context(), func(tx context.Context) error {
			var e error
			_, e = save(tx, f.repo, indexJobs, j.ID, j.Version, j)
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
		return indexJob(t, f, c.ID)
	}
	j := setFailed()
	f.store.fail = true
	if e = f.s.RetryIndex(t.Context(), c.ID, c.Version, j.Version, f.resolve); e == nil {
		t.Fatal("required audit bypassed")
	}
	f.store.fail = false
	if current := indexJob(t, f, c.ID); current.Version != j.Version || current.State != "dead" {
		t.Fatal("failed command changed job", current)
	}
	if e = f.s.RetryIndex(t.Context(), c.ID, c.Version, j.Version-1, f.resolve); !errors.Is(e, ErrConflict) {
		t.Fatal("stale CAS", e)
	}
	f.admins.strength = "password"
	if e = f.s.RetryIndex(t.Context(), c.ID, c.Version, j.Version, f.resolve); !errors.Is(e, ErrMFARequired) {
		t.Fatal("missing MFA", e)
	}
	f.admins.strength = "mfa"
	if e = f.s.RetryIndex(t.Context(), c.ID, c.Version, j.Version, f.resolve); e != nil {
		t.Fatal(e)
	}
	reset := indexJob(t, f, c.ID)
	if reset.State != "pending" || reset.Attempts != 0 || reset.LeaseToken != 0 || reset.ReadyGeneration != 0 || reset.Requested != j.Requested+1 {
		t.Fatal(reset)
	}
	if e = executor.step(t.Context(), j); !errors.Is(e, ErrIndexLease) {
		t.Fatal("old executor lease survived manual retry", e)
	}
	if e = f.s.RetryIndex(t.Context(), c.ID, c.Version, reset.Version, f.resolve); !errors.Is(e, ErrConflict) {
		t.Fatal("running retry accepted", e)
	}
	indexDrain(t, f, executor, c.ID)
	d, e = f.s.IndexDiagnostics(t.Context(), c.ID, f.resolve)
	if e != nil || d.State != "idle" || d.ReadyGeneration == 0 {
		t.Fatal(d, e)
	}
	f.admins.allowed = false
	if d, e = f.s.IndexDiagnostics(t.Context(), c.ID, f.resolve); e == nil || d.CollectionID != "" {
		t.Fatal("revoked metadata", d, e)
	}
	f.admins.allowed = true
	f.enabled = false
	if d, e = f.s.IndexDiagnostics(t.Context(), c.ID, f.resolve); !errors.Is(e, ErrDisabled) || d.CollectionID != "" {
		t.Fatal("Feature-off", d, e)
	}
}
