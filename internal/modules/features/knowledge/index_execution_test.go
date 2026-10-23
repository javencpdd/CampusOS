package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
)

func indexFixture(t *testing.T, count int) (*collectionsFixture, *IndexService, Collection) {
	t.Helper()
	f := fixtureCollections(t)
	for i := 0; i < count; i++ {
		row := publicFixture()
		row.ID = fmt.Sprint(100 + i)
		if err := f.threads.Create(t.Context(), row); err != nil {
			t.Fatal(err)
		}
	}
	c := f.collection(t)
	state := f.selection(t, c, "board", "91003")
	c = state.Collection
	s, err := newIndexService(f.repo, f.s, f.s.query.(communityport.SourceReferenceInventory))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.reliable.CheckedRead(t.Context(), func(ctx context.Context) error { return f.repo.ensureIndexJob(ctx, c.ID, false) }); err != nil {
		t.Fatal(err)
	}
	return f, s, c
}
func indexJob(t *testing.T, f *collectionsFixture, id string) IndexJob {
	t.Helper()
	j, e := load[IndexJob](t.Context(), f.repo, indexJobs, id, false)
	if e != nil {
		t.Fatal(e)
	}
	return j
}
func indexDrain(t *testing.T, f *collectionsFixture, s *IndexService, id string) {
	t.Helper()
	for n := 0; n < 200; n++ {
		if indexJob(t, f, id).State == "idle" {
			return
		}
		if err := s.runPage(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("index did not finish")
}
func indexDocs(t *testing.T, f *collectionsFixture, id string) []IndexDocument {
	t.Helper()
	d, e := list[IndexDocument](t.Context(), f.repo, indexDocuments, id, 2000)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func indexWake(t *testing.T, f *collectionsFixture, id string) {
	t.Helper()
	if err := f.s.reliable.CheckedRead(t.Context(), func(ctx context.Context) error { return f.repo.ensureIndexJob(ctx, id, true) }); err != nil {
		t.Fatal(err)
	}
}

func TestIndexExecutionPagedGenerationAndRevisions(t *testing.T) {
	f, s, c := indexFixture(t, 70)
	if err := s.runPage(t.Context()); err != nil {
		t.Fatal(err)
	} // member phase
	if err := s.runPage(t.Context()); err != nil {
		t.Fatal(err)
	} // 32 sources
	j := indexJob(t, f, c.ID)
	if j.Phase != "sources" || j.SourceAfter == "" || j.ReadyGeneration != 0 || len(indexDocs(t, f, c.ID)) != 32 {
		t.Fatal(j)
	}
	for _, d := range indexDocs(t, f, c.ID) {
		if _, err := s.CurrentDocument(t.Context(), d.ID); !errors.Is(err, ErrIndexNotReady) {
			t.Fatal("partial run released", err)
		}
	}
	// A fresh service resumes the durable cursor/generation.
	s2, err := newIndexService(f.repo, f.s, s.inventory)
	if err != nil {
		t.Fatal(err)
	}
	indexDrain(t, f, s2, c.ID)
	docs := indexDocs(t, f, c.ID)
	if len(docs) != 71 {
		t.Fatal("source keyset lost rows", len(docs))
	}
	for _, d := range docs {
		if got, err := s2.CurrentDocument(t.Context(), d.ID); err != nil || got.Source != d.Source {
			t.Fatal("ready rejected", err)
		}
	}
	old := docs[0]
	row, _ := f.threads.GetByID(t.Context(), old.Source.ID)
	row.Content = "新的修订"
	if err = f.threads.Update(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	if _, err = s2.CurrentDocument(t.Context(), old.ID); err == nil {
		t.Fatal("source revision returned stale body")
	}
	indexWake(t, f, c.ID)
	indexDrain(t, f, s2, c.ID)
	now, err := s2.CurrentDocument(t.Context(), old.ID)
	if err != nil || now.Source.Revision == old.Source.Revision || strings.Join(now.Chunks, "") != "新的修订" {
		t.Fatal(now, err)
	}
	for _, d := range indexDocs(t, f, c.ID) {
		if d.Generation != indexJob(t, f, c.ID).ReadyGeneration {
			t.Fatal("mixed generations")
		}
	}
}
func TestIndexExecutionLeaseFencingAndFiniteRetry(t *testing.T) {
	f, s, c := indexFixture(t, 0)
	clock := time.Now()
	s.now = func() time.Time { return clock }
	var wg sync.WaitGroup
	var mu sync.Mutex
	leases := []IndexJob{}
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := s.claim(t.Context())
			if e == nil {
				mu.Lock()
				leases = append(leases, j)
				mu.Unlock()
			} else if !errors.Is(e, ErrNotFound) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if len(leases) != 1 {
		t.Fatal("concurrent claims", len(leases))
	}
	clock = clock.Add(31 * time.Second)
	current, e := s.claim(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if current.LeaseToken == leases[0].LeaseToken || current.RunGeneration != leases[0].RunGeneration {
		t.Fatal("epoch/generation fence")
	}
	if e = s.step(t.Context(), leases[0]); !errors.Is(e, ErrIndexLease) {
		t.Fatal("old lease wrote", e)
	}
	if e = s.fail(t.Context(), leases[0]); !errors.Is(e, ErrIndexLease) {
		t.Fatal("old failure overwrote owner", e)
	}
	for current.Attempts < 8 {
		if e = s.fail(t.Context(), current); e != nil {
			t.Fatal(e)
		}
		clock = clock.Add(time.Minute)
		current, e = s.claim(t.Context())
		if e != nil {
			t.Fatal(e)
		}
	}
	if e = s.fail(t.Context(), current); e != nil {
		t.Fatal(e)
	}
	if j := indexJob(t, f, c.ID); j.State != "dead" || j.Attempts != 8 || j.LastError != "page_failed" {
		t.Fatal(j)
	}
	if _, e = s.claim(t.Context()); !errors.Is(e, ErrNotFound) {
		t.Fatal("dead busy retry", e)
	}
	clock = clock.Add(time.Minute)
	current, e = s.claim(t.Context())
	if e != nil || current.Attempts != 1 {
		t.Fatal(current, e)
	}
}

type checkpointFault struct {
	*MemoryRepository
	fail bool
}

func (f *checkpointFault) save(ctx context.Context, k recordKind, id string, v int64, data any) (record, error) {
	if f.fail && k == indexJobs && data.(IndexJob).State == "pending" {
		return record{}, errors.New("injected checkpoint failure")
	}
	return f.MemoryRepository.save(ctx, k, id, v, data)
}
func TestIndexExecutionAtomicPageAndFeatureOffPurge(t *testing.T) {
	f, s, c := indexFixture(t, 70)
	if err := s.runPage(t.Context()); err != nil {
		t.Fatal(err)
	}
	lease, err := s.claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	before := indexJob(t, f, c.ID)
	fault := &checkpointFault{MemoryRepository: f.repo, fail: true}
	s.store = fault
	if err = s.step(t.Context(), lease); err == nil {
		t.Fatal("fault not injected")
	}
	if len(indexDocs(t, f, c.ID)) != 0 || indexJob(t, f, c.ID).SourceAfter != before.SourceAfter {
		t.Fatal("partial body or cursor committed")
	}
	fault.fail = false
	if err = s.step(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	indexDrain(t, f, s, c.ID)
	if len(indexDocs(t, f, c.ID)) != 71 {
		t.Fatal("recovery")
	}
	f.enabled = false
	for _, d := range indexDocs(t, f, c.ID) {
		if _, err = s.CurrentDocument(t.Context(), d.ID); !errors.Is(err, ErrDisabled) {
			t.Fatal("disabled released", err)
		}
	}
	indexWake(t, f, c.ID)
	// A body reader that fails the test is not touched by feature-off cleanup.
	s.collections.plain = panicIndexReader{t}
	indexDrain(t, f, s, c.ID)
	if len(indexDocs(t, f, c.ID)) != 0 || indexJob(t, f, c.ID).ReadyGeneration != 0 {
		t.Fatal("disabled retained projection")
	}
}

type panicIndexReader struct{ t *testing.T }

func (p panicIndexReader) PreviewPublicThread(context.Context, string) (PublicThreadCandidate, error) {
	p.t.Fatal("disabled origin read")
	return PublicThreadCandidate{}, ErrDisabled
}
func (p panicIndexReader) RecheckPublicThread(context.Context, SourceReference) (PublicThreadCandidate, error) {
	p.t.Fatal("disabled origin recheck")
	return PublicThreadCandidate{}, ErrDisabled
}

func TestIndexExecutionAuthoritativeEvictionAndMissingCollection(t *testing.T) {
	for _, mode := range []string{"private", "deleted", "moved", "board", "rule", "missing"} {
		t.Run(mode, func(t *testing.T) {
			f, s, c := indexFixture(t, 0)
			indexDrain(t, f, s, c.ID)
			d := indexDocs(t, f, c.ID)[0]
			switch mode {
			case "private", "deleted", "moved":
				row, _ := f.threads.GetByID(t.Context(), d.Source.ID)
				if mode == "private" {
					row.PublicationStatus = "private"
				}
				if mode == "deleted" {
					row.DeletionStatus = "trashed"
				}
				if mode == "moved" {
					row.CategoryID = "999"
				}
				if e := f.threads.Update(t.Context(), row); e != nil {
					t.Fatal(e)
				}
			case "board":
				f.cat.rows[0].LifecycleStatus = "archived"
				f.cat.rows[0].Version++
			case "rule":
				r, _ := load[Selection](t.Context(), f.repo, selections, d.SelectionID, true)
				r.Active = false
				if _, e := save(t.Context(), f.repo, selections, r.ID, r.Version, r); e != nil {
					t.Fatal(e)
				}
			case "missing":
				f.repo.mu.Lock()
				delete(f.repo.rows[collections], c.ID)
				f.repo.mu.Unlock()
			}
			if got, e := s.CurrentDocument(t.Context(), d.ID); e == nil || got.ID != "" {
				t.Fatal("revoked source released")
			}
			// No event or task is created: periodic job revisits authoritative facts.
			s.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
			if e := s.runPage(t.Context()); e != nil {
				t.Fatal(e)
			}
			indexDrain(t, f, s, c.ID)
			if len(indexDocs(t, f, c.ID)) != 0 {
				t.Fatal("orphan body retained", mode)
			}
		})
	}
}
func TestIndexSourceEventsAndChunkContract(t *testing.T) {
	f, s, c := indexFixture(t, 0)
	indexDrain(t, f, s, c.ID)
	consumer := &indexSourceConsumer{store: f.repo, reliable: f.s.reliable}
	for kind, aggregate := range indexSourceEvents {
		before := indexJob(t, f, c.ID).Requested
		event, e := reliability.NewEvent(kind, aggregate, "91001", map[string]string{"private": "NEVER_COPY_PRIVATE_EVENT_BODY"})
		if e != nil {
			t.Fatal(e)
		}
		if e = consumer.consume(t.Context(), event); e != nil || indexJob(t, f, c.ID).Requested != before+1 {
			t.Fatal(kind, e)
		}
	}
	consumer.stopped.Store(true)
	if e := consumer.consume(t.Context(), reliability.Event{}); !errors.Is(e, ErrIndexConsumerStopped) {
		t.Fatal(e)
	}
	consumer.stopped.Store(false)
	if e := consumer.consume(t.Context(), reliability.Event{}); e == nil {
		t.Fatal("invalid event accepted")
	}
	for _, body := range []string{strings.Repeat("中", 2100), strings.Repeat("a", 65536)} {
		chunks := indexChunks(body)
		if strings.Join(chunks, "") != body || len(chunks) > 64 {
			t.Fatal("chunk loss")
		}
		for _, chunk := range chunks {
			if !utf8.ValidString(chunk) || utf8.RuneCountInString(chunk) > 1024 {
				t.Fatal("chunk bounds")
			}
		}
	}
}

type indexFlipReader struct {
	PublicSourceCandidates
	flip  func()
	calls int
}

func (r *indexFlipReader) PreviewPublicThread(ctx context.Context, id string) (PublicThreadCandidate, error) {
	v, e := r.PublicSourceCandidates.PreviewPublicThread(ctx, id)
	r.calls++
	if r.calls == 2 {
		r.flip()
	}
	return v, e
}
func TestIndexExecutionMidPageGateAndCancellation(t *testing.T) {
	f, s, c := indexFixture(t, 2)
	if e := s.runPage(t.Context()); e != nil {
		t.Fatal(e)
	}
	lease, e := s.claim(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	reader := &indexFlipReader{PublicSourceCandidates: f.s.plain, flip: func() { f.enabled = false }}
	f.s.plain = reader
	if e = s.step(t.Context(), lease); !errors.Is(e, ErrDisabled) {
		t.Fatal("mid-page gate", e)
	}
	if len(indexDocs(t, f, c.ID)) != 0 || indexJob(t, f, c.ID).SourceAfter != "" {
		t.Fatal("disabled page committed")
	}
	f.enabled = true
	f.s.plain = reader.PublicSourceCandidates
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if e = s.step(cancelled, lease); !errors.Is(e, context.Canceled) {
		t.Fatal("cancelled page", e)
	}
	if len(indexDocs(t, f, c.ID)) != 0 {
		t.Fatal("cancelled bytes")
	}
	if e = s.step(t.Context(), lease); e != nil {
		t.Fatal(e)
	}
	indexDrain(t, f, s, c.ID)
}
func TestIndexExecutionBehindCursorAndCollectionChange(t *testing.T) {
	f, s, c := indexFixture(t, 70)
	for n := 0; n < 2; n++ {
		if e := s.runPage(t.Context()); e != nil {
			t.Fatal(e)
		}
	}
	row := publicFixture()
	row.ID = "50"
	if e := f.threads.Create(t.Context(), row); e != nil {
		t.Fatal(e)
	}
	indexDrain(t, f, s, c.ID)
	if len(indexDocs(t, f, c.ID)) != 71 {
		t.Fatal("cursor is not a commit snapshot")
	}
	s.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if e := s.runPage(t.Context()); e != nil {
		t.Fatal(e)
	}
	indexDrain(t, f, s, c.ID)
	if len(indexDocs(t, f, c.ID)) != 72 {
		t.Fatal("periodic scan missed source behind cursor")
	}
	indexWake(t, f, c.ID)
	for n := 0; n < 2; n++ {
		if e := s.runPage(t.Context()); e != nil {
			t.Fatal(e)
		}
	}
	oldGeneration := indexJob(t, f, c.ID).RunGeneration
	c.Title = "new collection revision"
	next, e := f.s.SaveCollection(t.Context(), c, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range indexDocs(t, f, c.ID) {
		if got, e := s.CurrentDocument(t.Context(), d.ID); e == nil || got.ID != "" {
			t.Fatal("changed collection released old build")
		}
	}
	indexDrain(t, f, s, c.ID)
	j := indexJob(t, f, c.ID)
	if j.ReadyGeneration == oldGeneration || j.CollectionVersion != next.Version {
		t.Fatal("mixed collection build", j)
	}
}

func TestIndexExecutionBoardAndMemberEpochABA(t *testing.T) {
	f, s, c := indexFixture(t, 0)
	indexDrain(t, f, s, c.ID)
	d := indexDocs(t, f, c.ID)[0]
	f.cat.rows[0].Version++ // same active board and bytes, a new authority epoch
	if got, e := s.CurrentDocument(t.Context(), d.ID); e == nil || got.ID != "" {
		t.Fatal("board ABA reused projection")
	}
	indexWake(t, f, c.ID)
	indexDrain(t, f, s, c.ID)
	now, e := s.CurrentDocument(t.Context(), d.ID)
	if e != nil || now.VisibilityRevision == d.VisibilityRevision {
		t.Fatal("new board proof", e)
	}
	m, e := load[Member](t.Context(), f.repo, members, now.MemberID, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = save(t.Context(), f.repo, members, m.ID, m.Version, m); e != nil {
		t.Fatal(e)
	}
	if got, e := s.CurrentDocument(t.Context(), d.ID); e == nil || got.ID != "" {
		t.Fatal("member ABA reused projection")
	}
}

func TestIndexExecutionBackoffHealthRequiresSuccessfulPage(t *testing.T) {
	f, s, _ := indexFixture(t, 0)
	clock := time.Now()
	s.now = func() time.Time { return clock }
	fault := &checkpointFault{MemoryRepository: f.repo, fail: true}
	s.store = fault
	if e := s.runPage(t.Context()); e == nil || !s.failed.Load() {
		t.Fatal("failed page not degraded")
	}
	if e := s.runPage(t.Context()); e != nil || !s.failed.Load() {
		t.Fatal("idle poll falsely declared recovery")
	}
	fault.fail = false
	clock = clock.Add(3 * time.Second)
	if e := s.runPage(t.Context()); e != nil || s.failed.Load() {
		t.Fatal("successful page did not recover", e)
	}
	expired, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	if e := s.runPage(expired); !errors.Is(e, context.DeadlineExceeded) || !s.failed.Load() {
		t.Fatal("deadline failure not degraded", e)
	}
}

func TestIndexExecutionStopCancelsBothLoopsWithExpiredContext(t *testing.T) {
	indexCancelled, backfillCancelled := false, false
	indexDone, backfillDone := make(chan struct{}), make(chan struct{})
	m := &Module{indexer: &IndexService{cancel: func() { indexCancelled = true }, done: indexDone}, backfill: &indexBackfiller{cancel: func() { backfillCancelled = true }, done: backfillDone}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if e := m.Stop(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if !indexCancelled || !backfillCancelled {
		t.Fatal("expired Stop left a loop running")
	}
	close(indexDone)
	close(backfillDone)
}
