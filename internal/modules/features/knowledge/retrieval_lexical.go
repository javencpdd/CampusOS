package knowledge

import (
	"context"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	BuiltinLexicalProvider = "builtin.lexical/v1"
	MaxSearchQueryBytes    = 1024
	MaxSearchTerms         = 16
	MaxSearchTextBytes     = 4 << 20
	SearchSnippetRunes     = 240
)

// RetrievalProvider is an internal, in-process strategy, not a plugin registry,
// network capability or authority source. Only the built-in strategy is wired.
// Inputs have already passed current public-source checks; outputs remain hints.
type RetrievalProvider interface {
	ID() string
	Search(context.Context, []string, []RetrievalDocument) ([]RetrievalMatch, error)
}
type RetrievalDocument struct {
	ID     string
	Title  string
	Chunks []string
}
type RetrievalMatch struct {
	DocumentID string
	ChunkIndex int
	Score      int
}
type LexicalProvider struct{}

func (LexicalProvider) ID() string { return BuiltinLexicalProvider }

func searchTerms(query string) ([]string, error) {
	if len(query) == 0 || len(query) > MaxSearchQueryBytes || !utf8.ValidString(query) {
		return nil, ErrInvalid
	}
	for _, r := range query {
		if unicode.IsControl(r) {
			return nil, ErrInvalid
		}
	}
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	terms := []string{}
	seen := map[string]bool{}
	for _, word := range words {
		if !seen[word] {
			terms = append(terms, word)
			seen[word] = true
		}
	}
	if len(terms) == 0 || len(terms) > MaxSearchTerms {
		return nil, ErrInvalid
	}
	return terms, nil
}
func matchingTerms(text string, terms []string) []string {
	folded, foldedReady := "", false
	out := []string{}
	for _, term := range terms {
		haystack := text
		if termNeedsFold(term) {
			if !foldedReady {
				folded, foldedReady = strings.ToLower(text), true
			}
			haystack = folded
		}
		if strings.Contains(haystack, term) {
			out = append(out, term)
		}
	}
	return out
}

// Uncased queries (for example Han and digits) cannot gain a match through
// lowercasing another rune. Skip a full body fold for them; retain Go's exact
// Unicode lowercase semantics whenever a term has a case relation.
func termNeedsFold(term string) bool {
	for _, r := range term {
		if unicode.SimpleFold(r) != r || unicode.ToUpper(r) != r || unicode.ToLower(r) != r {
			return true
		}
	}
	return false
}

// Index chunks split at rune boundaries, not word boundaries. Include enough
// following context to match a term starting in this chunk without losing it
// at the1024-rune split. Terms starting wholly in following chunks score there.
func lexicalWindow(d RetrievalDocument, index int, terms []string) (string, []string) {
	need := 0
	for _, term := range terms {
		need = max(need, utf8.RuneCountInString(term)-1)
	}
	window := d.Chunks[index]
	for i := index + 1; i < len(d.Chunks) && need > 0; i++ {
		r := []rune(d.Chunks[i])
		take := min(need, len(r))
		window += string(r[:take])
		need -= take
	}
	folded, foldedReady := "", false
	matched := []string{}
	// Lowercasing preserves rune positions, but may change byte widths (İ→i).
	// Compare byte positions within the folded prefix instead of recounting
	// the entire 1024-rune chunk for each match and verification pass.
	for _, term := range terms {
		haystack, boundary := window, len(d.Chunks[index])
		if termNeedsFold(term) {
			if !foldedReady {
				folded, foldedReady = strings.ToLower(window), true
			}
			haystack, boundary = folded, len(folded)
			if len(window) != len(d.Chunks[index]) {
				boundary = len(strings.ToLower(d.Chunks[index]))
			}
		}
		at := strings.Index(haystack, term)
		if at >= 0 && at < boundary {
			matched = append(matched, term)
		}
	}
	return window, matched
}

// Ranking only needs matching terms, not a copied 1024-rune snippet window.
// Allocate a short boundary window only when a term is absent from this chunk.
func lexicalChunkTerms(d RetrievalDocument, index int, terms []string) []string {
	chunk := d.Chunks[index]
	folded, foldedReady := "", false
	boundary, boundaryFolded, suffix := "", "", ""
	boundaryReady, boundaryFoldedReady := false, false
	matched := []string{}
	for _, term := range terms {
		cased := termNeedsFold(term)
		text := chunk
		if cased {
			if !foldedReady {
				folded, foldedReady = strings.ToLower(chunk), true
			}
			text = folded
		}
		if strings.Contains(text, term) {
			matched = append(matched, term)
			continue
		}
		if index+1 == len(d.Chunks) || utf8.RuneCountInString(term) <= 1 {
			continue
		}
		if !boundaryReady {
			need := 0
			for _, t := range terms {
				need = max(need, utf8.RuneCountInString(t)-1)
			}
			start := len(chunk)
			for n := 0; n < need && start > 0; n++ {
				_, size := utf8.DecodeLastRuneInString(chunk[:start])
				start -= size
			}
			suffix = chunk[start:]
			boundary = suffix
			for i := index + 1; i < len(d.Chunks) && need > 0; i++ {
				next, end := d.Chunks[i], 0
				for need > 0 && end < len(next) {
					_, size := utf8.DecodeRuneInString(next[end:])
					end += size
					need--
				}
				boundary += next[:end]
			}
			boundaryReady = true
		}
		text, limit := boundary, len(suffix)
		if cased {
			if !boundaryFoldedReady {
				boundaryFolded, boundaryFoldedReady = strings.ToLower(boundary), true
			}
			text, limit = boundaryFolded, len(strings.ToLower(suffix))
		}
		if at := strings.Index(text, term); at >= 0 && at < limit {
			matched = append(matched, term)
		}
	}
	return matched
}

// Exact Unicode substrings, AND across terms, no stemming, segmentation, SQL
// wildcard or model. Title presence weighs4; best-chunk presence weighs1.
// Occurrence stuffing does not increase the score. A source may cover different
// terms in different chunks; the citation identifies its best single chunk.
func lexicalMatch(terms []string, d RetrievalDocument) (RetrievalMatch, bool) {
	title := matchingTerms(d.Title, terms)
	covered := map[string]bool{}
	for _, term := range title {
		covered[term] = true
	}
	best, bestCount := 0, -1
	for i := range d.Chunks {
		matched := lexicalChunkTerms(d, i, terms)
		for _, term := range matched {
			covered[term] = true
		}
		if len(matched) > bestCount {
			best, bestCount = i, len(matched)
		}
		if bestCount == len(terms) && len(covered) == len(terms) {
			break
		}
	}
	if len(d.Chunks) == 0 || len(covered) != len(terms) {
		return RetrievalMatch{}, false
	}
	return RetrievalMatch{d.ID, best, 4*len(title) + bestCount}, true
}
func (LexicalProvider) Search(ctx context.Context, terms []string, docs []RetrievalDocument) ([]RetrievalMatch, error) {
	result := []RetrievalMatch{}
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if m, ok := lexicalMatch(terms, d); ok {
			result = append(result, m)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].DocumentID < result[j].DocumentID
	})
	return result, ctx.Err()
}
func searchSnippet(chunk string, terms []string) string {
	runes := []rune(chunk)
	if len(runes) <= SearchSnippetRunes {
		return chunk
	}
	folded := strings.ToLower(chunk)
	first := -1
	for _, term := range terms {
		at := strings.Index(folded, term)
		if at >= 0 && (first < 0 || at < first) {
			first = at
		}
	}
	start := 0
	if first >= 0 {
		start = max(0, utf8.RuneCountInString(folded[:first])-60)
	}
	end := min(len(runes), start+SearchSnippetRunes)
	return string(runes[start:end])
}
