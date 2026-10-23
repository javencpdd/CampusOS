package knowledge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

func TestRevisionMemoNeverCachesCurrentAuthority(t *testing.T) {
	row := publicFixture()
	row.FactsVersion = 7
	s, e := NewPublicSourceService(queryFunc(func(context.Context, string) (*domain.Thread, error) { return row, nil }), func() bool { return true })
	if e != nil {
		t.Fatal(e)
	}
	ctx, end := withRevisionMemo(t.Context())
	a, e := s.PreviewPublicThread(ctx, row.ID)
	if e != nil {
		t.Fatal(e)
	}
	row.Content = "changed bytes with same claimed epoch"
	b, e := s.PreviewPublicThread(ctx, row.ID)
	if e != nil || a.Reference.Revision == b.Reference.Revision {
		t.Fatal("digest keyed only by epoch", e)
	}
	row.PublicationStatus = domain.PublicationStatusPrivate
	if _, e = s.PreviewPublicThread(ctx, row.ID); !errors.Is(e, ErrSourceUnavailable) {
		t.Fatal("memo reused a public allow", e)
	}
	end()
	called := 0
	compute := func() (string, error) { called++; return "fresh", nil }
	for i := 0; i < 2; i++ {
		if _, e := memoRevision(ctx, revisionMemoSlot{"test", "id"}, "expired", 0, compute); e != nil {
			t.Fatal(e)
		}
	}
	if called != 2 {
		t.Fatal("memo outlived its callback")
	}
}

func TestRevisionMemoDoesNotRetainOversizedValuesOrErrors(t *testing.T) {
	ctx, end := withRevisionMemo(t.Context())
	defer end()
	calls := 0
	compute := func() (string, error) { calls++; return "derived", nil }
	for i := 0; i < 2; i++ {
		if _, e := memoRevision(ctx, revisionMemoSlot{"test", "id"}, "large", MaxSearchTextBytes+1, compute); e != nil {
			t.Fatal(e)
		}
	}
	if calls != 2 {
		t.Fatal("oversized digest key retained")
	}
	for i := 0; i < 2; i++ {
		if _, e := memoRevision(ctx, revisionMemoSlot{"test", "id"}, []string{"not-comparable"}, 0, compute); e != nil {
			t.Fatal(e)
		}
	}
	if calls != 4 {
		t.Fatal("non-comparable value cached")
	}
	fresh, stop := withRevisionMemo(t.Context())
	defer stop()
	failed := 0
	for i := 0; i < 2; i++ {
		_, e := memoRevision(fresh, revisionMemoSlot{"test", "id"}, "failure", 0, func() (string, error) { failed++; return "", ErrSourceUnavailable })
		if !errors.Is(e, ErrSourceUnavailable) {
			t.Fatal(e)
		}
	}
	if failed != 2 {
		t.Fatal("digest error cached")
	}
}

func TestRevisionMemoConcurrentDifferentFactsNeverShareDigest(t *testing.T) {
	ctx, end := withRevisionMemo(t.Context())
	defer end()
	var wg sync.WaitGroup
	errs := make(chan string, 16)
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				bound := fmt.Sprintf("body:%d:%d", n, i)
				got, err := memoRevision(ctx, revisionMemoSlot{"source", "same-id"}, bound, len(bound), func() (string, error) { return bound, nil })
				if err != nil || got != bound {
					errs <- "different bound facts shared a digest"
					return
				}
			}
		}(n)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
