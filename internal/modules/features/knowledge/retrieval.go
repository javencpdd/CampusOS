package knowledge

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync/atomic"
	"time"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
)

const PortPublicRetrieval = "knowledge.public-retrieval"

var ErrRetrievalProvider = errors.New("knowledge retrieval provider contract failed")

// This Port has anonymous-public semantics, matching09a's source Port. No
// caller identity, claimed ACL, index-consent or external endpoint Scope can
// expand it. Future HTTP/Host/MCP adapters need their own ingress guards.
type PublicRetrieval interface {
	SearchPublic(context.Context, PublicSearchRequest) (PublicSearchPage, error)
}
type PublicSearchRequest struct {
	CollectionID string `json:"collection_id"`
	Query        string `json:"query"`
	Page         int    `json:"page"`
	PageSize     int    `json:"page_size"`
}
type SearchExplanation struct {
	Terms       []string `json:"terms"`
	TitleTerms  []string `json:"title_terms"`
	ChunkTerms  []string `json:"chunk_terms"`
	TitleWeight int      `json:"title_weight"`
	ChunkWeight int      `json:"chunk_weight"`
}
type SearchCitation struct {
	CollectionID       string          `json:"collection_id"`
	MemberID           string          `json:"member_id"`
	VisibilityRevision string          `json:"visibility_revision"`
	Source             SourceReference `json:"source"`
	Generation         int64           `json:"generation,string"`
	ChunkIndex         int             `json:"chunk_index"`
	URL                string          `json:"url"`
}
type PublicSearchHit struct {
	Title       string            `json:"title"`
	Snippet     string            `json:"snippet"`
	Score       int               `json:"score"`
	Explanation SearchExplanation `json:"explanation"`
	Citation    SearchCitation    `json:"citation"`
}
type PublicSearchPage struct {
	Items      []PublicSearchHit `json:"items"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	Provider   string            `json:"provider"`
	Generation int64             `json:"generation,string"`
}
type RetrievalService struct {
	indexer  *IndexService
	provider RetrievalProvider
	stopped  atomic.Bool
}

func newRetrievalService(indexer *IndexService, provider RetrievalProvider) (*RetrievalService, error) {
	if indexer == nil || indexer.collections == nil || nilSourceAdapter(provider) || provider.ID() != BuiltinLexicalProvider {
		return nil, ErrSourceUnavailable
	}
	return &RetrievalService{indexer: indexer, provider: provider}, nil
}
func retrievalHidden(err error) bool { return hiddenOrigin(err) || errors.Is(err, ErrIndexNotReady) }

func (s *RetrievalService) SearchPublic(ctx context.Context, req PublicSearchRequest) (PublicSearchPage, error) {
	var out PublicSearchPage
	if s == nil || s.indexer == nil {
		return out, ErrSourceUnavailable
	}
	if s.stopped.Load() {
		return out, ErrIndexConsumerStopped
	}
	if !opaque("kc_", req.CollectionID) || req.Page < 1 || req.Page > 100000 || req.PageSize < 1 || req.PageSize > 50 {
		return out, ErrInvalid
	}
	terms, err := searchTerms(req.Query)
	if err != nil {
		return out, err
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	index := s.indexer
	err = index.collections.reliable.CheckedRead(bounded, func(tx context.Context) error {
		var endMemo func()
		tx, endMemo = withRevisionMemo(tx)
		defer endMemo()
		if e := index.collections.check(tx); e != nil {
			return e
		}
		// Match the executor lock order. Holding the job prevents mixed projected
		// generations; source row locks and final checks guard authority changes.
		collection, e := load[Collection](tx, index.store, collections, req.CollectionID, false)
		if e != nil {
			return e
		}
		if collection.Status != "active" {
			return ErrNotFound
		}
		job, e := load[IndexJob](tx, index.store, indexJobs, req.CollectionID, false)
		if e != nil {
			return e
		}
		if job.ReadyGeneration < 1 || job.RunGeneration != 0 || job.CollectionVersion != collection.Version {
			return ErrIndexNotReady
		}
		state, e := index.collections.state(tx, req.CollectionID, false)
		if e != nil {
			return e
		}
		membersByID := make(map[string]Member, len(state.Members))
		for _, m := range state.Members {
			if _, exists := membersByID[m.ID]; !exists {
				membersByID[m.ID] = m
			}
		}
		projected := make([]IndexDocument, 0, MaxMembers)
		projectedBytes, validBytes := 0, 0
		fullAccounting := false
		account := func(candidate IndexDocument) error {
			d, e := index.checkDocumentMember(tx, job, state, candidate, true, membersByID[candidate.MemberID])
			if e != nil {
				if retrievalHidden(e) {
					return nil
				}
				return e
			}
			validBytes += searchDocumentBytes(d)
			if validBytes > MaxSearchTextBytes {
				return ErrCapacity
			}
			projected = append(projected, d)
			return nil
		}
		after := ""
		examined := 0
		for {
			rows, e := index.store.retrievalPage(tx, req.CollectionID, after, 128)
			if e != nil {
				return e
			}
			for _, d := range rows {
				examined++
				if examined > MaxMembers {
					return ErrCapacity
				}
				after = d.ID
				projectedBytes += searchDocumentBytes(d)
				if !fullAccounting && projectedBytes > MaxSearchTextBytes {
					// Bound retained text even for a 1000x64KiB stale projection.
					// Stream the original current-G1 accounting instead of retaining
					// hidden text or treating its byte count as current capacity.
					buffered := projected
					projected = []IndexDocument{}
					fullAccounting = true
					for _, v := range buffered {
						if e = account(v); e != nil {
							return e
						}
					}
				}
				if fullAccounting {
					if e = account(d); e != nil {
						return e
					}
				} else {
					projected = append(projected, d)
				}
			}
			if len(rows) < 128 {
				break
			}
			if e = tx.Err(); e != nil {
				return e
			}
		}
		// A literal projection match is only a host-side hint, never authority.
		// If the entire scanned text fits, every valid subset also fits the same
		// corpus budget. For larger projections retain full current-G1 accounting
		// so stale/private bytes cannot change capacity or leak partial totals.
		bytes := 0
		for _, d := range projected {
			bytes += len(d.Title)
			for _, c := range d.Chunks {
				bytes += len(c)
			}
		}
		expectedMatches := map[string]RetrievalMatch{}
		if bytes <= MaxSearchTextBytes {
			hints := []IndexDocument{}
			for _, d := range projected {
				if match, ok := lexicalMatch(terms, RetrievalDocument{d.ID, d.Title, d.Chunks}); ok {
					expectedMatches[d.ID] = match
					hints = append(hints, d)
				}
			}
			projected = hints
		}
		// Domain-owned rows stay locked for this callback. Public G1, source
		// revisions and final rechecks still run; no allow/result survives it.
		read := func(tx context.Context) error {
			docs := make([]IndexDocument, 0, len(projected))
			totalBytes := 0
			for _, candidate := range projected {
				d, e := index.checkDocumentMember(tx, job, state, candidate, true, membersByID[candidate.MemberID])
				if e != nil {
					if retrievalHidden(e) {
						continue
					}
					return e
				}
				totalBytes += len(d.Title)
				for _, chunk := range d.Chunks {
					totalBytes += len(chunk)
				}
				if totalBytes > MaxSearchTextBytes {
					return ErrCapacity
				}
				docs = append(docs, d)
			}
			// Providers receive detached public text, not canonical citations/ACLs or
			// repository handles. Their slice mutations cannot change trusted data.
			_, builtin := s.provider.(LexicalProvider)
			var inputs []RetrievalDocument
			if !builtin {
				inputs = make([]RetrievalDocument, 0, len(docs))
			}
			byID := make(map[string]int, len(docs))
			for i, d := range docs {
				if !builtin {
					inputs = append(inputs, RetrievalDocument{d.ID, d.Title, append([]string(nil), d.Chunks...)})
				}
				byID[d.ID] = i
			}
			var matches []RetrievalMatch
			if builtin {
				// The concrete built-in strategy is the same pure function used
				// for hints. All input bytes have now passed current origin checks;
				// reuse that calculation without repeating Unicode folding.
				for _, d := range docs {
					if match, ok := expectedMatches[d.ID]; ok {
						matches = append(matches, match)
					}
				}
				e = tx.Err()
			} else {
				matches, e = s.provider.Search(tx, append([]string(nil), terms...), inputs)
			}
			if e != nil {
				if tx.Err() != nil {
					return tx.Err()
				}
				return ErrRetrievalProvider
			}
			if len(matches) > len(docs) {
				return ErrRetrievalProvider
			}
			seen := make(map[string]bool, len(matches))
			valid := make([]RetrievalMatch, 0, len(matches))
			for _, m := range matches {
				at, ok := byID[m.DocumentID]
				if !ok || seen[m.DocumentID] {
					return ErrRetrievalProvider
				}
				d := docs[at]
				seen[m.DocumentID] = true
				// Current origin validation covered every hint byte. The pure
				// lexical result is exact; provider inputs were detached copies.
				expected, ok := expectedMatches[d.ID]
				if !ok || expected != m {
					return ErrRetrievalProvider
				}
				valid = append(valid, m)
			}
			// Recheck ALL candidates that could contribute to count, not merely the
			// returned page. Memory adapters also re-read current relation facts here.
			current := state
			if repo, ok := index.store.(*PgRepository); ok {
				matched, err := repo.matchesRetrievalState(tx, state)
				if err != nil {
					return err
				}
				if !matched {
					return ErrConflict
				}
			} else {
				var err error
				current, err = index.collections.state(tx, req.CollectionID, false)
				if err != nil {
					return err
				}
			}
			currentJob, e := load[IndexJob](tx, index.store, indexJobs, req.CollectionID, false)
			if e != nil {
				return e
			}
			if !reflect.DeepEqual(current, state) || currentJob.ReadyGeneration != job.ReadyGeneration {
				return ErrConflict
			}
			for _, d := range docs {
				// The host projection was fully validated above and providers only
				// received detached copies. Reopen ALL current authority/origin facts
				// and exact body now, without hashing the same immutable header twice.
				v, err := index.collections.visible(tx, current, membersByID[d.MemberID])
				if err != nil {
					return err
				}
				if v.Revision != d.VisibilityRevision || v.Source.Reference != d.Source || v.Source.Title != d.Title || v.Source.URL != d.URL || !indexChunksMatch(v.Source.Content, d.Chunks) || d.ParserVersion != indexParser || d.ChunkerVersion != indexChunker || d.ProfileFingerprint != indexProfile {
					return ErrIndexNotReady
				}
				if e = index.collections.check(tx); e != nil {
					return e
				}
			}
			sort.Slice(valid, func(i, j int) bool {
				if valid[i].Score != valid[j].Score {
					return valid[i].Score > valid[j].Score
				}
				return valid[i].DocumentID < valid[j].DocumentID
			})
			// Overlapping board/single rules yield one source, with a stable member
			// citation. Provider results cannot expand the chosen collection scope.
			unique := make([]RetrievalMatch, 0, len(valid))
			type sourceKey struct{ kind, id string }
			sources := make(map[sourceKey]bool, len(valid))
			for _, m := range valid {
				d := docs[byID[m.DocumentID]]
				key := sourceKey{d.Source.Kind, d.Source.ID}
				if sources[key] {
					continue
				}
				sources[key] = true
				unique = append(unique, m)
			}
			out = PublicSearchPage{Items: []PublicSearchHit{}, Total: len(unique), Page: req.Page, PageSize: req.PageSize, Provider: BuiltinLexicalProvider, Generation: job.ReadyGeneration}
			start := (req.Page - 1) * req.PageSize
			if start < len(unique) {
				for _, m := range unique[start:min(len(unique), start+req.PageSize)] {
					d := docs[byID[m.DocumentID]]
					window, chunkTerms := lexicalWindow(RetrievalDocument{d.ID, d.Title, d.Chunks}, m.ChunkIndex, terms)
					out.Items = append(out.Items, PublicSearchHit{Title: d.Title, Snippet: searchSnippet(window, terms), Score: m.Score, Explanation: SearchExplanation{append([]string(nil), terms...), matchingTerms(d.Title, terms), chunkTerms, 4, 1}, Citation: SearchCitation{d.CollectionID, d.MemberID, d.VisibilityRevision, d.Source, d.Generation, m.ChunkIndex, d.URL}})
				}
			}
			if s.stopped.Load() {
				return ErrIndexConsumerStopped
			}
			return index.collections.check(tx)
		}
		threads, articles, officials := []string{}, []string{}, []string{}
		for _, d := range projected {
			switch d.Source.Kind {
			case PublicThreadKind:
				threads = append(threads, d.Source.ID)
			case PublicArticleKind:
				threads = append(threads, d.Source.ID)
				articles = append(articles, d.Source.ID)
			case OfficialKind:
				officials = append(officials, d.Source.ID)
			}
		}
		withOfficial := func(c context.Context) error {
			if repo, ok := index.store.(*PgRepository); ok {
				return repo.withOfficialReadSnapshot(c, officials, read)
			}
			return read(c)
		}
		withArticles := func(c context.Context) error {
			if source, ok := index.collections.articles.(*PublicArticleSourceService); ok {
				return source.withPublicTextSnapshot(c, articles, withOfficial)
			}
			return withOfficial(c)
		}
		if snapshot, ok := index.collections.query.(communityport.SourceReadSnapshot); ok {
			return snapshot.WithSourceReadSnapshot(tx, threads, withArticles)
		}
		return withArticles(tx)

	})
	if err != nil {
		return PublicSearchPage{}, err
	}
	return out, nil
}
