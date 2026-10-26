package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func seedBackfill(t *testing.T, f *collectionsFixture, kind string, count int) []string {
	t.Helper()
	k, _ := backfillTarget(kind, 1)
	prefix := "kc_"
	if kind == "official" {
		prefix = "ko_"
	}
	ids := []string{}
	for i := 1; i <= count; i++ {
		id := fmt.Sprintf("%s%032x", prefix, i)
		if _, err := f.repo.save(t.Context(), k, id, 0, map[string]string{"id": id, "content": "PRIVATE_BACKFILL_BODY"}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}
func backfillCheckpoint(t *testing.T, repo *MemoryRepository, kind string, due bool) indexBackfillCheckpoint {
	t.Helper()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	var cp indexBackfillCheckpoint
	r := repo.rows[indexBackfillCheckpoints][kind]
	if err := json.Unmarshal(r.Data, &cp); err != nil {
		t.Fatal(err)
	}
	if due {
		cp.NextScanAt = time.Time{}
		r.Data, _ = json.Marshal(cp)
		repo.rows[indexBackfillCheckpoints][kind] = r
	}
	return cp
}
func TestIndexBackfillDurablePagesRestartAndMetadataOnly(t *testing.T) {
	f := fixtureCollections(t)
	ids := seedBackfill(t, f, "collection", 73)
	seedBackfill(t, f, "official", 6)
	f.enabled = false // enumeration is metadata maintenance, not a Feature grant
	b, err := newIndexBackfiller(f.repo, f.s.reliable)
	if err != nil {
		t.Fatal(err)
	}
	first, err := b.page(t.Context(), "collection")
	if err != nil || first.Targets != 32 || first.Checkpoint.AfterID != ids[31] || first.Checkpoint.CompletedPasses != 0 {
		t.Fatal(first, err)
	}
	// A new process/service starts from the persisted adapter checkpoint.
	b, err = newIndexBackfiller(f.repo, f.s.reliable)
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.page(t.Context(), "collection")
	if err != nil || second.Targets != 32 || second.Checkpoint.AfterID != ids[63] {
		t.Fatal(second, err)
	}
	last, err := b.page(t.Context(), "collection")
	if err != nil || last.Targets != 9 || last.Checkpoint.AfterID != "" || last.Checkpoint.CompletedPasses != 1 {
		t.Fatal(last, err)
	}
	idle, err := b.page(t.Context(), "collection")
	if err != nil || idle.Advanced {
		t.Fatal("completed pass did not idle", idle, err)
	}
	if page, err := b.page(t.Context(), "official"); err != nil || page.Targets != 6 || page.Checkpoint.CompletedPasses != 1 {
		t.Fatal(page, err)
	}
	rows := indexRows(t, f.repo)
	if len(rows) != 79 {
		t.Fatal(len(rows))
	}
	wire, _ := json.Marshal(rows)
	if strings.Contains(string(wire), "PRIVATE_BACKFILL_BODY") {
		t.Fatal("source body copied")
	}
	if _, err := f.s.PublicCollections(t.Context()); !errors.Is(err, ErrDisabled) {
		t.Fatal("backfill enabled source reads", err)
	}
}

type failingBackfillStore struct {
	*MemoryRepository
	after func() error
}

func (s failingBackfillStore) backfillIndexPage(ctx context.Context, kind string, limit int) (indexBackfillPage, error) {
	page, err := s.MemoryRepository.backfillIndexPage(ctx, kind, limit)
	if err != nil {
		return page, err
	}
	return page, s.after()
}
func TestIndexBackfillPageFailureAndCancellationRollbackTogether(t *testing.T) {
	for _, mode := range []string{"checkpoint_failure", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			f := fixtureCollections(t)
			seedBackfill(t, f, "collection", 65)
			b, _ := newIndexBackfiller(f.repo, f.s.reliable)
			if _, err := b.page(t.Context(), "collection"); err != nil {
				t.Fatal(err)
			}
			before := backfillCheckpoint(t, f.repo, "collection", false)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			b.store = failingBackfillStore{f.repo, func() error {
				if mode == "canceled" {
					cancel()
					return nil
				}
				return errors.New("owned checkpoint failure")
			}}
			if result, err := b.page(ctx, "collection"); err == nil || result.Advanced {
				t.Fatal("failed page returned committed progress", result, err)
			}
			if len(indexRows(t, f.repo)) != 32 || backfillCheckpoint(t, f.repo, "collection", false) != before {
				t.Fatal("page checkpoint and requests did not roll back together")
			}
			b.store = f.repo
			if page, err := b.page(t.Context(), "collection"); err != nil || page.Targets != 32 {
				t.Fatal(page, err)
			}
		})
	}
}
func TestIndexBackfillConcurrentPagesAndNewPassFindsBehindCursor(t *testing.T) {
	f := fixtureCollections(t)
	ids := seedBackfill(t, f, "collection", 70)
	b, _ := newIndexBackfiller(f.repo, f.s.reliable)
	if _, err := b.page(t.Context(), "collection"); err != nil {
		t.Fatal(err)
	}
	low := "kc_" + strings.Repeat("0", 32)
	if _, err := f.repo.save(t.Context(), collections, low, 0, map[string]string{"id": low}); err != nil {
		t.Fatal(err)
	}
	previous, _ := f.repo.get(t.Context(), collections, ids[0], false)
	current, err := f.repo.save(t.Context(), collections, ids[0], previous.Version, map[string]string{"id": ids[0]})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := b.page(t.Context(), "collection"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(indexRows(t, f.repo)) != 70 || backfillCheckpoint(t, f.repo, "collection", false).CompletedPasses != 1 {
		t.Fatal("concurrent scanners skipped or duplicated a page")
	}
	backfillCheckpoint(t, f.repo, "collection", true)
	for range 3 {
		if _, err := b.page(t.Context(), "collection"); err != nil {
			t.Fatal(err)
		}
	}
	rows := indexRows(t, f.repo)
	if len(rows) != 72 {
		t.Fatal("next pass missed insert/update behind cursor", len(rows))
	}
	lowFound, versionFound := false, false
	for _, request := range rows {
		lowFound = lowFound || request.TargetID == low
		versionFound = versionFound || request.TargetID == ids[0] && request.TargetVersion == current.Version
	}
	if !lowFound || !versionFound {
		t.Fatal("next pass did not reload current facts")
	}
}
func TestIndexBackfillRejectsInvalidScopeAndRequiresTransaction(t *testing.T) {
	f := fixtureCollections(t)
	b, _ := newIndexBackfiller(f.repo, f.s.reliable)
	if _, err := b.page(t.Context(), "threads"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := f.repo.backfillIndexPage(t.Context(), "collection", 1); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatal(err)
	}
	if len(indexRows(t, f.repo)) != 0 {
		t.Fatal("invalid operation wrote requests")
	}
}
