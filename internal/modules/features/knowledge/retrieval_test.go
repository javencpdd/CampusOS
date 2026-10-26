package knowledge

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func retrievalFixture(t *testing.T, n int) (*collectionsFixture, *IndexService, *RetrievalService, Collection) {
	t.Helper()
	f, index, c := indexFixture(t, n)
	indexDrain(t, f, index, c.ID)
	s, e := newRetrievalService(index, LexicalProvider{})
	if e != nil {
		t.Fatal(e)
	}
	return f, index, s, c
}
func request(c Collection, query string) PublicSearchRequest {
	return PublicSearchRequest{c.ID, query, 1, 10}
}

func TestRetrievalLexicalTermsRankingAndBoundary(t *testing.T) {
	for _, query := range []string{"", " ", "%_ --", strings.Repeat("a", 1025), "图书馆\n开放", string([]byte{0xff}), "a b c d e f g h i j k l m n o p q"} {
		if _, e := searchTerms(query); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid query", e)
		}
	}
	terms, e := searchTerms("图书馆 OPEN open")
	if e != nil || !reflect.DeepEqual(terms, []string{"图书馆", "open"}) {
		t.Fatal(terms, e)
	}
	docs := []RetrievalDocument{{"b", "图书馆 OPEN", []string{"图书馆 open"}}, {"a", "普通", []string{"图书馆 open open open"}}, {"c", "图书馆", []string{"未包含英语词"}}}
	matches, e := (LexicalProvider{}).Search(t.Context(), terms, docs)
	if e != nil || len(matches) != 2 || matches[0].DocumentID != "b" || matches[0].Score != 10 || matches[1].Score != 2 {
		t.Fatal(matches, e)
	}
	boundary := strings.Repeat("中", 1021) + "图书馆开放时间" + strings.Repeat("文", 400)
	terms, _ = searchTerms("图书馆开放时间")
	d := RetrievalDocument{"boundary", "普通", indexChunks(boundary)}
	m, ok := lexicalMatch(terms, d)
	if !ok || m.ChunkIndex != 0 || m.Score != 1 {
		t.Fatal("chunk boundary lost a term", m)
	}
	window, _ := lexicalWindow(d, m.ChunkIndex, terms)
	snippet := searchSnippet(window, terms)
	if !utf8.ValidString(snippet) || utf8.RuneCountInString(snippet) > SearchSnippetRunes || !strings.Contains(snippet, "图书馆开放时间") || !strings.Contains(boundary, snippet) {
		t.Fatal("invalid source snippet")
	}
}

func TestRetrievalLexicalFoldedChunkBoundary(t *testing.T) {
	// İ folds from two UTF-8 bytes to one. A match starting only in the next
	// chunk must not inflate this chunk's score, even with a long lookahead.
	d := RetrievalDocument{ID: "unicode", Chunks: []string{strings.Repeat("İ", 1024), "abcde"}}
	_, terms := lexicalWindow(d, 0, []string{"i", "abcde"})
	if !reflect.DeepEqual(terms, []string{"i"}) {
		t.Fatal("following chunk scored at the old byte boundary", terms)
	}
	d.Chunks = []string{strings.Repeat("İ", 1023) + "a", "bcde"}
	_, terms = lexicalWindow(d, 0, []string{"abcde"})
	if !reflect.DeepEqual(terms, []string{"abcde"}) {
		t.Fatal("cross-chunk term disappeared after case folding", terms)
	}
	if match, ok := lexicalMatch([]string{"abcde"}, d); !ok || match.ChunkIndex != 0 || match.Score != 1 {
		t.Fatal("ranking lost folded boundary", match, ok)
	}
}

func TestRetrievalChunkTermsAgreeWithCitationWindows(t *testing.T) {
	for _, chunks := range [][]string{
		{strings.Repeat("中", 1021) + "图书馆", "开放时间"},
		{strings.Repeat("İ", 1023) + "a", "bcde"},
		{"校", "园", "服", "务"},
		{strings.Repeat("x", 1024), "图书馆"},
	} {
		d := RetrievalDocument{ID: "boundary", Chunks: chunks}
		for _, terms := range [][]string{{"图书馆开放时间"}, {"abcde"}, {"校园服务"}, {"图书馆"}, {"x", "abcde"}} {
			for i := range chunks {
				_, expected := lexicalWindow(d, i, terms)
				if got := lexicalChunkTerms(d, i, terms); !reflect.DeepEqual(got, expected) {
					t.Fatal("rank and citation window disagree", i, terms, got, expected)
				}
			}
		}
	}
}

func TestRetrievalUncasedAndUnicodeCaseQueries(t *testing.T) {
	for _, tc := range []struct {
		text, query string
		want        bool
	}{
		{"图书馆开放123", "图书馆", true}, {"图书馆开放123", "123", true},
		{"图书馆开放123", "自习室", false}, {"校园 WiFi", "wifi", true},
		{"Kelvin", "kelvin", true}, {"ẞ", "ß", true}, {"İ", "i", true},
		{"Σ", "σ", true}, {"Σ", "ς", false},
	} {
		t.Run(tc.query+tc.text, func(t *testing.T) {
			d := RetrievalDocument{ID: "case", Chunks: []string{tc.text}}
			_, got := lexicalMatch([]string{tc.query}, d)
			if got != tc.want || (len(matchingTerms(tc.text, []string{tc.query})) == 1) != tc.want {
				t.Fatal("Unicode lowercase semantics changed", tc, got)
			}
		})
	}
}

func TestRetrievalPaginationDedupAndCurrentCitations(t *testing.T) {
	f, index, s, c := retrievalFixture(t, 70)
	state, e := f.s.SaveSelection(t.Context(), c.ID, c.Version, Selection{Kind: "thread", SourceID: "91001", Active: true}, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	c = state.Collection
	indexWake(t, f, c.ID)
	indexDrain(t, f, index, c.ID)
	seen := map[string]bool{}
	for page := 1; page <= 11; page++ {
		req := request(c, "公开文本")
		req.Page, req.PageSize = page, 7
		got, e := s.SearchPublic(t.Context(), req)
		if e != nil || got.Total != 71 || got.Provider != BuiltinLexicalProvider || got.Generation < 1 {
			t.Fatal(got, e)
		}
		for _, hit := range got.Items {
			key := hit.Citation.Source.Kind + ":" + hit.Citation.Source.ID
			if seen[key] || hit.Citation.CollectionID != c.ID || hit.Score != 1 || !strings.Contains(hit.Snippet, "公开文本") {
				t.Fatal(hit)
			}
			seen[key] = true
			origin, e := f.s.Source(t.Context(), c.ID, hit.Citation.MemberID, hit.Citation.VisibilityRevision)
			if e != nil || origin.Source.Reference != hit.Citation.Source || origin.Source.Title != hit.Title || !strings.Contains(origin.Source.Content, hit.Snippet) {
				t.Fatal("unverifiable citation", e)
			}
		}
		if page == 11 && len(got.Items) != 1 {
			t.Fatal("last page", len(got.Items))
		}
	}
	if len(seen) != 71 {
		t.Fatal("pagination dropped source", len(seen))
	}
	req := request(c, "公开文本")
	req.Page = 100000
	if got, e := s.SearchPublic(t.Context(), req); e != nil || got.Total != 71 || len(got.Items) != 0 {
		t.Fatal(got, e)
	}
}

type retrievalProviderFunc func(context.Context, []string, []RetrievalDocument) ([]RetrievalMatch, error)

func (retrievalProviderFunc) ID() string { return BuiltinLexicalProvider }
func (f retrievalProviderFunc) Search(ctx context.Context, terms []string, docs []RetrievalDocument) ([]RetrievalMatch, error) {
	return f(ctx, terms, docs)
}

func TestRetrievalFiltersBeforeRankingAndCollectionScope(t *testing.T) {
	f, index, s, c := retrievalFixture(t, 2)
	row, _ := f.threads.GetByID(t.Context(), "91001")
	row.Content = "PRIVATE_REVOKED_MARKER"
	if e := f.threads.Update(t.Context(), row); e != nil {
		t.Fatal(e)
	}
	indexWake(t, f, c.ID)
	indexDrain(t, f, index, c.ID)
	row.PublicationStatus = "private"
	if e := f.threads.Update(t.Context(), row); e != nil {
		t.Fatal(e)
	}
	other := f.collection(t)
	o, e := f.s.SaveOfficial(t.Context(), Official{Title: "Other collection", Content: "OTHER_COLLECTION_MARKER", Format: "text", Status: "published"}, f.resolve)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.SaveSelection(t.Context(), other.ID, other.Version, Selection{Kind: "official", SourceID: o.ID, Active: true}, f.resolve); e != nil {
		t.Fatal(e)
	}
	if e = f.s.reliable.CheckedRead(t.Context(), func(ctx context.Context) error { return f.repo.ensureIndexJob(ctx, other.ID, true) }); e != nil {
		t.Fatal(e)
	}
	indexDrain(t, f, index, other.ID)
	s.provider = retrievalProviderFunc(func(ctx context.Context, terms []string, docs []RetrievalDocument) ([]RetrievalMatch, error) {
		for _, d := range docs {
			if strings.Contains(d.Title+strings.Join(d.Chunks, ""), "MARKER") {
				t.Fatal("revoked or other-collection bytes reached provider")
			}
		}
		return (LexicalProvider{}).Search(ctx, terms, docs)
	})
	for _, query := range []string{"PRIVATE_REVOKED_MARKER", "OTHER_COLLECTION_MARKER"} {
		got, e := s.SearchPublic(t.Context(), request(c, query))
		if e != nil || got.Total != 0 || len(got.Items) != 0 {
			t.Fatal(got, e)
		}
	}
	got, e := s.SearchPublic(t.Context(), request(c, "公开文本"))
	if e != nil || got.Total != 2 {
		t.Fatal("private affected count", got, e)
	}
}

func TestRetrievalFinalRevocationAndProviderIsolation(t *testing.T) {
	for _, mode := range []string{"feature", "source", "board", "rule", "member", "collection", "stop", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f, _, s, c := retrievalFixture(t, 0)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.provider = retrievalProviderFunc(func(ctx context.Context, terms []string, docs []RetrievalDocument) ([]RetrievalMatch, error) {
				matches, e := (LexicalProvider{}).Search(ctx, terms, docs)
				switch mode {
				case "feature":
					f.enabled = false
				case "source":
					row, _ := f.threads.GetByID(ctx, "91001")
					row.PublicationStatus = "private"
					if e = f.threads.Update(ctx, row); e != nil {
						t.Fatal(e)
					}
				case "board":
					f.cat.rows[0].Version++
				case "rule":
					r, _ := load[Selection](ctx, f.repo, selections, indexDocs(t, f, c.ID)[0].SelectionID, true)
					r.Active = false
					_, e = save(ctx, f.repo, selections, r.ID, r.Version, r)
				case "member":
					e = f.repo.deleteMember(ctx, indexDocs(t, f, c.ID)[0].MemberID)
				case "collection":
					v, _ := load[Collection](ctx, f.repo, collections, c.ID, true)
					v.Status = "disabled"
					_, e = save(ctx, f.repo, collections, v.ID, v.Version, v)
				case "stop":
					s.stopped.Store(true)
				case "cancel":
					cancel()
				}
				return matches, e
			})
			out, e := s.SearchPublic(ctx, request(c, "公开文本"))
			if e == nil || !reflect.DeepEqual(out, PublicSearchPage{}) {
				t.Fatal("revoked return leaked results/count", out, e)
			}
		})
	}
	for _, mode := range []string{"foreign", "duplicate", "chunk", "score", "error", "mutate"} {
		t.Run(mode, func(t *testing.T) {
			_, _, s, c := retrievalFixture(t, 0)
			s.provider = retrievalProviderFunc(func(ctx context.Context, terms []string, docs []RetrievalDocument) ([]RetrievalMatch, error) {
				matches, e := (LexicalProvider{}).Search(ctx, terms, docs)
				switch mode {
				case "foreign":
					matches[0].DocumentID = "foreign"
				case "duplicate":
					matches = append(matches, matches[0])
				case "chunk":
					matches[0].ChunkIndex = 100
				case "score":
					matches[0].Score++
				case "error":
					return nil, fmt.Errorf("SECRET_INTERNAL_PROVIDER_ERROR")
				case "mutate":
					docs[0].Chunks[0] = "FORGED_PUBLIC_TEXT"
					terms[0] = "FORGED_QUERY"
				}
				return matches, e
			})
			out, e := s.SearchPublic(t.Context(), request(c, "公开文本"))
			if mode == "mutate" {
				if e != nil || out.Total != 1 || strings.Contains(out.Items[0].Snippet, "FORGED") {
					t.Fatal(out, e)
				}
			} else if !errors.Is(e, ErrRetrievalProvider) || !reflect.DeepEqual(out, PublicSearchPage{}) {
				t.Fatal("untrusted provider accepted", out, e)
			}
		})
	}
}

func TestRetrievalReadinessDisabledAndBudget(t *testing.T) {
	f, index, s, c := retrievalFixture(t, 0)
	f.enabled = false
	if out, e := s.SearchPublic(t.Context(), request(c, "公开")); !errors.Is(e, ErrDisabled) || out.Total != 0 {
		t.Fatal(out, e)
	}
	f.enabled = true
	indexWake(t, f, c.ID)
	if e := index.runPage(t.Context()); e != nil {
		t.Fatal(e)
	}
	if out, e := s.SearchPublic(t.Context(), request(c, "公开")); !errors.Is(e, ErrIndexNotReady) || out.Total != 0 {
		t.Fatal(out, e)
	}
	indexDrain(t, f, index, c.ID)
	for _, req := range []PublicSearchRequest{{CollectionID: c.ID, Query: "公开", Page: 0, PageSize: 10}, {CollectionID: "other", Query: "公开", Page: 1, PageSize: 10}, {CollectionID: c.ID, Query: "公开", Page: 1, PageSize: 51}} {
		if _, e := s.SearchPublic(t.Context(), req); !errors.Is(e, ErrInvalid) {
			t.Fatal(e)
		}
	}
	// Supported sources can individually be64KiB. A query refuses an oversized
	// corpus without leaking a partial count or silently truncating candidates.
	big, indexBig, searchBig, colBig := retrievalFixture(t, 65)
	ids, e := big.threads.SourceThreadIDs(t.Context(), "91003", "", 33)
	if e != nil {
		t.Fatal(e)
	}
	all := append([]string(nil), ids...)
	for len(ids) > 0 {
		ids, e = big.threads.SourceThreadIDs(t.Context(), "91003", ids[len(ids)-1], 33)
		if e != nil {
			t.Fatal(e)
		}
		all = append(all, ids...)
	}
	for _, id := range all {
		r, _ := big.threads.GetByID(t.Context(), id)
		r.Content = strings.Repeat("a", MaxSourceBodyBytes)
		if e = big.threads.Update(t.Context(), r); e != nil {
			t.Fatal(e)
		}
	}
	indexWake(t, big, colBig.ID)
	indexDrain(t, big, indexBig, colBig.ID)
	if out, e := searchBig.SearchPublic(t.Context(), request(colBig, "a")); !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(out, PublicSearchPage{}) {
		t.Fatal("corpus truncated", out, e)
	}
}
