package knowledge

import (
	"context"
	"strings"
	"testing"
)

type sourceReadProbe struct {
	PublicSourceCandidates
	ids []string
}

func (p *sourceReadProbe) PreviewPublicThread(ctx context.Context, id string) (PublicThreadCandidate, error) {
	p.ids = append(p.ids, id)
	return p.PublicSourceCandidates.PreviewPublicThread(ctx, id)
}

func TestRetrievalProjectionHintsRemainUntrusted(t *testing.T) {
	f, index, s, c := retrievalFixture(t, 40)
	r, _ := f.threads.GetByID(t.Context(), "91001")
	r.Content = "rareterm CURRENT_PUBLIC_TEXT"
	if e := f.threads.Update(t.Context(), r); e != nil {
		t.Fatal(e)
	}
	indexWake(t, f, c.ID)
	indexDrain(t, f, index, c.ID)
	probe := &sourceReadProbe{PublicSourceCandidates: f.s.plain}
	f.s.plain = probe
	s.provider = retrievalProviderFunc(func(ctx context.Context, terms []string, docs []RetrievalDocument) ([]RetrievalMatch, error) {
		if len(docs) > 1 {
			t.Fatal("nonmatching projection reached Provider")
		}
		for _, d := range docs {
			if !strings.Contains(strings.Join(d.Chunks, ""), "CURRENT_PUBLIC_TEXT") {
				t.Fatal("unverified hint")
			}
		}
		return (LexicalProvider{}).Search(ctx, terms, docs)
	})
	p, e := s.SearchPublic(t.Context(), request(c, "rareterm"))
	if e != nil || p.Total != 1 {
		t.Fatal(p, e)
	}
	for _, id := range probe.ids {
		if id != "91001" {
			t.Fatal("noncontributing source read", id)
		}
	}
	r.PublicationStatus = "private"
	if e := f.threads.Update(t.Context(), r); e != nil {
		t.Fatal(e)
	}
	p, e = s.SearchPublic(t.Context(), request(c, "rareterm"))
	if e != nil || p.Total != 0 || len(p.Items) != 0 {
		t.Fatal("hint authorized private source", p, e)
	}
}

func TestRetrievalOversizedStalePrivateProjectionUsesCurrentBudget(t *testing.T) {
	f, index, s, c := retrievalFixture(t, 65)
	ids := []string{}
	after := ""
	for {
		page, e := f.threads.SourceThreadIDs(t.Context(), "91003", after, 33)
		if e != nil {
			t.Fatal(e)
		}
		if len(page) == 0 {
			break
		}
		ids = append(ids, page...)
		after = page[len(page)-1]
	}
	for _, id := range ids {
		r, _ := f.threads.GetByID(t.Context(), id)
		r.Content = strings.Repeat("a", MaxSourceBodyBytes)
		if e := f.threads.Update(t.Context(), r); e != nil {
			t.Fatal(e)
		}
	}
	indexWake(t, f, c.ID)
	indexDrain(t, f, index, c.ID)
	for _, id := range ids {
		r, _ := f.threads.GetByID(t.Context(), id)
		r.PublicationStatus = "private"
		if e := f.threads.Update(t.Context(), r); e != nil {
			t.Fatal(e)
		}
	}
	s.provider = retrievalProviderFunc(func(ctx context.Context, terms []string, docs []RetrievalDocument) ([]RetrievalMatch, error) {
		if len(docs) != 0 {
			t.Fatal("private bytes reached Provider")
		}
		return []RetrievalMatch{}, nil
	})
	p, e := s.SearchPublic(t.Context(), request(c, "a"))
	if e != nil || p.Total != 0 || len(p.Items) != 0 {
		t.Fatal("stale private bytes treated as current capacity", p, e)
	}
}
