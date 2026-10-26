package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/pkg/observability"
)

const (
	indexPageSize = 32
	indexParser   = "public-text/v1"
	indexChunker  = "unicode-1024/v1"
)

var (
	ErrIndexLease    = errors.New("knowledge index lease lost")
	ErrIndexNotReady = errors.New("knowledge current index unavailable")
	indexProfile     = indexHash("campusos.knowledge.lexical/v1")
)

// IndexJob is durable execution metadata, separate from immutable requests.
// Cursors enumerate current facts; none is a commit/event watermark or ACL.
type IndexJob struct {
	ID                string `json:"id"`
	Version           int64  `json:"version,string"`
	State             string `json:"state"`
	Requested         int64  `json:"requested,string"`
	ActiveRequest     int64  `json:"active_request,string"`
	RunGeneration     int64  `json:"run_generation,string"`
	ReadyGeneration   int64  `json:"ready_generation,string"`
	CollectionVersion int64  `json:"collection_version,string"`
	Phase             string `json:"phase"`
	MemberAfter       string `json:"member_after"`
	RuleAfter         string `json:"rule_after"`
	WorkingRule       string `json:"working_rule"`
	SourceAfter       string `json:"source_after"`
	BoardStamp        string `json:"board_stamp"`
	DocumentAfter     string `json:"document_after"`
	Attempts          int    `json:"attempts"`
	AvailableAtMS     int64  `json:"available_at_ms,string"`
	LeaseUntilMS      int64  `json:"lease_until_ms,string"`
	LeaseToken        int64  `json:"lease_token,string"`
	LastError         string `json:"last_error"`
}

// IndexDocument contains only rechecked public text, as untrusted data. It is
// internal: callers must use CurrentDocument, never treat this as read authority.
type IndexDocument struct {
	ID                 string          `json:"id"`
	Version            int64           `json:"version,string"`
	CollectionID       string          `json:"collection_id"`
	CollectionVersion  int64           `json:"collection_version,string"`
	SelectionID        string          `json:"selection_id"`
	SelectionVersion   int64           `json:"selection_version,string"`
	MemberID           string          `json:"member_id"`
	Generation         int64           `json:"generation,string"`
	Source             SourceReference `json:"source"`
	VisibilityRevision string          `json:"visibility_revision"`
	ParserVersion      string          `json:"parser_version"`
	ChunkerVersion     string          `json:"chunker_version"`
	ProfileFingerprint string          `json:"profile_fingerprint"`
	Title              string          `json:"title"`
	URL                string          `json:"url"`
	Chunks             []string        `json:"chunks"`
}

func indexHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func newIndexJob(id string) IndexJob { return IndexJob{ID: id, State: "pending", Requested: 1} }
func (j *IndexJob) begin(token int64) {
	j.RunGeneration, j.ActiveRequest = token, j.Requested
	j.CollectionVersion, j.Phase = 0, "members"
	j.MemberAfter, j.RuleAfter, j.WorkingRule, j.SourceAfter, j.BoardStamp, j.DocumentAfter = "", "", "", "", "", ""
}
func indexChunks(body string) []string {
	runes := []rune(body)
	result := []string{}
	for len(runes) > 0 {
		n := min(len(runes), 1024)
		result = append(result, string(runes[:n]))
		runes = runes[n:]
	}
	return result
}

type IndexService struct {
	meter       observability.Meter
	store       indexStore
	collections *CollectionService
	inventory   communityport.SourceReferenceInventory
	now         func() time.Time
	failed      atomic.Bool
	mu          sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	stopping    bool
}

func newIndexService(store indexStore, s *CollectionService, inventory communityport.SourceReferenceInventory) (*IndexService, error) {
	if nilSourceAdapter(store) || s == nil || nilSourceAdapter(inventory) {
		return nil, ErrSourceUnavailable
	}
	return &IndexService{store: store, collections: s, inventory: inventory, now: time.Now}, nil
}
func (s *IndexService) claim(ctx context.Context) (IndexJob, error) {
	var out IndexJob
	var previousState string
	var due int64
	err := s.collections.reliable.CheckedRead(ctx, func(tx context.Context) error {
		r, err := s.store.selectIndexJob(tx, s.now().UnixMilli())
		if err != nil {
			return err
		}
		j, err := decode[IndexJob](r)
		if err != nil {
			return err
		}
		previousState, due = j.State, j.AvailableAtMS
		if j.State == "processing" {
			due = j.LeaseUntilMS
		}
		token, err := s.store.indexToken(tx)
		if err != nil {
			return err
		}
		if j.State == "dead" || j.State == "idle" {
			j.Attempts = 0
		}
		if j.Attempts >= 8 {
			j.State, j.LastError, j.LeaseUntilMS = "dead", "lease_expired", 0
			j.AvailableAtMS = s.now().Add(time.Minute).UnixMilli()
			_, err = save(tx, s.store, indexJobs, j.ID, j.Version, j)
			return err
		}
		if j.RunGeneration == 0 {
			j.begin(token)
		}
		j.State, j.LeaseToken, j.LeaseUntilMS = "processing", token, s.now().Add(30*time.Second).UnixMilli()
		j.Attempts++
		out, err = save(tx, s.store, indexJobs, j.ID, j.Version, j)
		return err
	})
	if err == nil && out.ID == "" {
		err = ErrNotFound
	}
	if err == nil {
		s.recordScheduleLag(previousState, due)
	}
	return out, err
}

func (s *IndexService) finish(j *IndexJob, ready bool) {
	if ready {
		j.ReadyGeneration = j.RunGeneration
	} else {
		j.ReadyGeneration = 0
	}
	j.RunGeneration, j.Phase, j.LeaseUntilMS = 0, "", 0
	j.State, j.AvailableAtMS = "idle", s.now().Add(time.Minute).UnixMilli()
	if j.Requested > j.ActiveRequest {
		j.State, j.AvailableAtMS = "pending", 0
	}
}

// step atomically commits one bounded page and its checkpoint under a fenced
// lease. Collection-before-job lock order agrees with commands/request intake.
func (s *IndexService) step(ctx context.Context, lease IndexJob) error {
	return s.collections.reliable.CheckedRead(ctx, func(tx context.Context) error {
		var endMemo func()
		tx, endMemo = withRevisionMemo(tx)
		defer endMemo()
		collection, ce := load[Collection](tx, s.store, collections, lease.ID, false)
		if ce != nil && !errors.Is(ce, ErrNotFound) {
			return ce
		}
		j, err := load[IndexJob](tx, s.store, indexJobs, lease.ID, true)
		if err != nil {
			return err
		}
		if j.State != "processing" || j.LeaseToken != lease.LeaseToken || j.LeaseUntilMS <= s.now().UnixMilli() {
			return ErrIndexLease
		}
		enabled := s.collections.enabled()
		purge := !enabled || ce != nil || collection.Status != "active"
		if purge {
			if j.Phase != "purge" {
				j.DocumentAfter = ""
			}
			j.Phase = "purge"
			j.ReadyGeneration = 0
		}
		if !purge {
			if j.Phase == "purge" || j.CollectionVersion != 0 && j.CollectionVersion != collection.Version {
				j.begin(j.LeaseToken)
			}
			j.CollectionVersion = collection.Version
		}
		done := false
		if purge || j.Phase == "documents" {
			done, err = s.documentPage(tx, &j, purge)
		} else {
			state, e := s.collections.state(tx, j.ID, false)
			if e != nil {
				return e
			}
			switch j.Phase {
			case "members":
				err = s.memberPage(tx, &j, state)
			case "sources":
				err = s.sourcePage(tx, &j, &state)
			default:
				return ErrInvalid
			}
		}
		if err != nil {
			return err
		}
		if !purge && !s.collections.enabled() {
			return ErrDisabled
		}
		if j.LeaseUntilMS <= s.now().UnixMilli() {
			return ErrIndexLease
		}
		j.Attempts, j.LastError, j.LeaseUntilMS = 0, "", 0
		if done {
			s.finish(&j, !purge)
		} else {
			j.State, j.AvailableAtMS = "pending", 0
		}
		_, err = save(tx, s.store, indexJobs, j.ID, j.Version, j)
		return err
	})
}

func (s *IndexService) memberPage(ctx context.Context, j *IndexJob, state CollectionState) error {
	rows, err := s.store.indexPage(ctx, members, j.ID, j.MemberAfter, indexPageSize+1)
	if err != nil {
		return err
	}
	page := []Member{}
	ids := []string{}
	for _, row := range rows[:min(len(rows), indexPageSize)] {
		m, e := decode[Member](row)
		if e != nil {
			return e
		}
		page = append(page, m)
		if m.Kind == PublicThreadKind || m.Kind == PublicArticleKind {
			ids = append(ids, m.SourceID)
		}
	}
	return s.withPageSourceRead(ctx, ids, func(ctx context.Context) error {
		for _, m := range page {
			var e error
			if _, e = s.collections.visible(ctx, state, m); e != nil {
				if !hiddenOrigin(e) {
					return e
				}
				if e = s.store.deleteMember(ctx, m.ID); e != nil {
					return e
				}
			}
			j.MemberAfter = m.ID
		}
		if len(rows) <= indexPageSize {
			j.Phase = "sources"
		}
		return ctx.Err()
	})
}
func (s *IndexService) sourcePage(ctx context.Context, j *IndexJob, state *CollectionState) error {
	sort.Slice(state.Selections, func(i, k int) bool { return state.Selections[i].ID < state.Selections[k].ID })
	var rule Selection
	for _, r := range state.Selections {
		if r.Active && r.ID > j.RuleAfter {
			rule = r
			break
		}
	}
	if rule.ID == "" {
		j.Phase = "documents"
		return nil
	}
	if j.WorkingRule != rule.ID {
		j.WorkingRule, j.SourceAfter, j.BoardStamp = rule.ID, "", ""
	}
	ids := []string{rule.SourceID}
	more := false
	if rule.Kind == "board" {
		stamp, e := s.collections.board(ctx, rule.SourceID)
		if e != nil {
			if !errors.Is(e, ErrNotFound) {
				return e
			}
			j.RuleAfter, j.WorkingRule = rule.ID, ""
			return nil
		}
		if j.BoardStamp != "" && j.BoardStamp != stamp {
			j.begin(j.LeaseToken)
			return nil
		}
		j.BoardStamp = stamp
		ids, e = s.inventory.SourceThreadIDs(ctx, rule.SourceID, j.SourceAfter, indexPageSize+1)
		if e != nil {
			return e
		}
		more = len(ids) > indexPageSize
		ids = ids[:min(len(ids), indexPageSize)]
	}
	read := func(ctx context.Context) error {
		for _, id := range ids {
			var candidate PublicThreadCandidate
			var err error
			if rule.Kind == "official" {
				candidate, err = s.collections.origin(ctx, OfficialKind, id)
			} else {
				candidate, err = s.collections.threadOrigin(ctx, id)
			}
			if err != nil {
				if !hiddenOrigin(err) {
					return err
				}
			} else if rule.Kind != "board" || candidate.CategoryID == rule.SourceID {
				if err = s.project(ctx, *j, state, rule, candidate); err != nil {
					return err
				}
			}
			j.SourceAfter = id
		}
		if rule.Kind == "board" {
			stamp, err := s.collections.board(ctx, rule.SourceID)
			if err != nil {
				return err
			}
			if stamp != j.BoardStamp {
				return ErrSourceChanged
			}
		}
		if !more {
			j.RuleAfter, j.WorkingRule, j.SourceAfter, j.BoardStamp = rule.ID, "", "", ""
		}
		return ctx.Err()
	}
	if rule.Kind == "official" {
		return read(ctx)
	}
	return s.withPageSourceRead(ctx, ids, read)
}

func (s *IndexService) project(ctx context.Context, j IndexJob, state *CollectionState, rule Selection, source PublicThreadCandidate) error {
	var m Member
	for _, v := range state.Members {
		if v.SelectionID == rule.ID && v.Kind == source.Reference.Kind && v.SourceID == source.Reference.ID {
			m = v
			break
		}
	}
	if m.ID == "" {
		if len(state.Members) >= MaxMembers {
			return ErrCapacity
		}
		m = Member{ID: "km_" + indexHash("member/v1\x00" + rule.ID + "\x00" + source.Reference.Kind + "\x00" + source.Reference.ID)[:32], CollectionID: j.ID, SelectionID: rule.ID, Kind: source.Reference.Kind, SourceID: source.Reference.ID}
	}
	if m.SelectionVersion != rule.Version || m.SourceRevision != source.Reference.Revision {
		m.SelectionVersion, m.SourceRevision = rule.Version, source.Reference.Revision
		var err error
		m, err = save(ctx, s.store, members, m.ID, m.Version, m)
		if err != nil {
			return err
		}
		found := false
		for i, v := range state.Members {
			if v.ID == m.ID {
				state.Members[i] = m
				found = true
				break
			}
		}
		if !found {
			state.Members = append(state.Members, m)
		}
	}
	visible, err := s.collections.visible(ctx, *state, m)
	if err != nil {
		return err
	}
	if visible.Source.Reference != source.Reference {
		return ErrSourceChanged
	}
	d := documentFor(j, rule, m, visible.Source, visible.Revision)
	old, err := load[IndexDocument](ctx, s.store, indexDocuments, d.ID, true)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = save(ctx, s.store, indexDocuments, d.ID, old.Version, d)
	return err
}
func documentFor(j IndexJob, r Selection, m Member, source PublicThreadCandidate, visibilityRevision string) IndexDocument {
	return IndexDocument{ID: indexHash("document/v1\x00" + j.ID + "\x00" + r.ID + "\x00" + source.Reference.Kind + "\x00" + source.Reference.ID), CollectionID: j.ID, CollectionVersion: j.CollectionVersion, SelectionID: r.ID, SelectionVersion: r.Version, MemberID: m.ID, Generation: j.RunGeneration, Source: source.Reference, VisibilityRevision: visibilityRevision, ParserVersion: indexParser, ChunkerVersion: indexChunker, ProfileFingerprint: indexProfile, Title: source.Title, URL: source.URL, Chunks: indexChunks(source.Content)}
}
func (s *IndexService) documentPage(ctx context.Context, j *IndexJob, purge bool) (bool, error) {
	rows, err := s.store.indexPage(ctx, indexDocuments, j.ID, j.DocumentAfter, indexPageSize+1)
	if err != nil {
		return false, err
	}
	var state CollectionState
	if !purge {
		state, err = s.collections.state(ctx, j.ID, false)
		if err != nil {
			return false, err
		}
	}
	page := []IndexDocument{}
	ids := []string{}
	for _, row := range rows[:min(len(rows), indexPageSize)] {
		d, e := decode[IndexDocument](row)
		if e != nil {
			return false, e
		}
		page = append(page, d)
		if d.Source.Kind == PublicThreadKind || d.Source.Kind == PublicArticleKind {
			ids = append(ids, d.Source.ID)
		}
	}
	read := func(ctx context.Context) error {
		for _, d := range page {
			var e error
			remove := purge || d.Generation != j.RunGeneration
			if !remove {
				_, e = s.checkDocument(ctx, *j, state, d, false)
				if e != nil {
					if !hiddenOrigin(e) && !errors.Is(e, ErrIndexNotReady) {
						return e
					}
					remove = true
					j.Requested++
				}
			}
			if remove {
				if e = s.store.deleteIndexDocument(ctx, d.ID); e != nil {
					return e
				}
			}
			j.DocumentAfter = d.ID
		}
		return ctx.Err()
	}
	if purge {
		err = read(ctx)
	} else {
		err = s.withPageSourceRead(ctx, ids, read)
	}
	return len(rows) <= indexPageSize, err
}

func (s *IndexService) checkDocument(ctx context.Context, j IndexJob, state CollectionState, d IndexDocument, ready bool) (IndexDocument, error) {
	var m Member
	for _, v := range state.Members {
		if v.ID == d.MemberID {
			m = v
			break
		}
	}
	return s.checkDocumentMember(ctx, j, state, d, ready, m)
}

// The retrieval caller builds a per-read member lookup from the exact locked
// state. This only replaces repeated linear scans; all current checks follow.
func (s *IndexService) checkDocumentMember(ctx context.Context, j IndexJob, state CollectionState, d IndexDocument, ready bool, m Member) (IndexDocument, error) {
	if ready && (j.ReadyGeneration == 0 || d.Generation != j.ReadyGeneration) || d.CollectionVersion != state.Collection.Version || d.CollectionID != state.Collection.ID {
		return IndexDocument{}, ErrIndexNotReady
	}
	var rule Selection
	for _, v := range state.Selections {
		if v.ID == d.SelectionID {
			rule = v
			break
		}
	}
	if m.ID == "" || m.ID != d.MemberID || rule.ID == "" || m.SelectionID != d.SelectionID || rule.Version != d.SelectionVersion || m.Kind != d.Source.Kind || m.SourceID != d.Source.ID {
		return IndexDocument{}, ErrIndexNotReady
	}
	v, err := s.collections.visible(ctx, state, m)
	if err != nil {
		return IndexDocument{}, err
	}
	if !indexChunksMatch(v.Source.Content, d.Chunks) {
		return IndexDocument{}, ErrIndexNotReady
	}
	source := v.Source
	source.Content = ""
	expected := documentFor(IndexJob{ID: j.ID, RunGeneration: d.Generation, CollectionVersion: state.Collection.Version}, rule, m, source, v.Revision)
	expected.Chunks = d.Chunks
	expected.Version = d.Version
	if !reflect.DeepEqual(expected, d) {
		return IndexDocument{}, ErrIndexNotReady
	}
	return d, s.collections.check(ctx)
}

// Verify the exact deterministic 1024-rune partition without allocating a
// second rune array and string slice for every current-fact recheck.
func indexChunksMatch(body string, chunks []string) bool {
	if body == "" {
		return len(chunks) == 0
	}
	after := 0
	for i, c := range chunks {
		if !utf8.ValidString(c) {
			return false
		}
		n := utf8.RuneCountInString(c)
		if n < 1 || n > 1024 || i < len(chunks)-1 && n != 1024 || after+len(c) > len(body) || body[after:after+len(c)] != c {
			return false
		}
		after += len(c)
	}
	return after == len(body)
}

// CurrentDocument rechecks live G1, source/rule revisions and the completed
// generation before releasing any bytes. There is deliberately no HTTP/search
// endpoint here; ranking, citations and retrieval are V12-10.
func (s *IndexService) CurrentDocument(ctx context.Context, id string) (IndexDocument, error) {
	var out IndexDocument
	if s == nil || len(id) != 64 {
		return out, ErrIndexNotReady
	}
	err := s.collections.reliable.CheckedRead(ctx, func(tx context.Context) error {
		if err := s.collections.check(tx); err != nil {
			return err
		}
		collection, err := s.store.documentCollection(tx, id)
		if err != nil {
			return err
		}
		if _, err = load[Collection](tx, s.store, collections, collection, false); err != nil {
			return err
		}
		j, err := load[IndexJob](tx, s.store, indexJobs, collection, false)
		if err != nil {
			return err
		}
		state, err := s.collections.state(tx, collection, false)
		if err != nil {
			return err
		}
		d, err := load[IndexDocument](tx, s.store, indexDocuments, id, false)
		if err != nil {
			return err
		}
		out, err = s.checkDocument(tx, j, state, d, true)
		return err
	})
	if err != nil {
		return IndexDocument{}, err
	}
	return out, nil
}

func (s *IndexService) fail(ctx context.Context, lease IndexJob) error {
	return s.collections.reliable.CheckedRead(ctx, func(tx context.Context) error {
		j, err := load[IndexJob](tx, s.store, indexJobs, lease.ID, true)
		if err != nil {
			return err
		}
		if j.LeaseToken != lease.LeaseToken || j.State != "processing" {
			return ErrIndexLease
		}
		j.State, j.LastError, j.LeaseUntilMS = "retry", "page_failed", 0
		j.AvailableAtMS = s.now().Add(time.Second * time.Duration(1<<min(j.Attempts, 5))).UnixMilli()
		if j.Attempts >= 8 {
			j.State, j.AvailableAtMS = "dead", s.now().Add(time.Minute).UnixMilli()
		}
		_, err = save(tx, s.store, indexJobs, j.ID, j.Version, j)
		return err
	})
}
func (s *IndexService) runPage(ctx context.Context) (err error) {
	start := time.Now()
	j, err := s.claim(ctx)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	defer func() { s.recordPage(start, err) }()
	if err != nil {
		s.failed.Store(true)
		return err
	}
	if err = s.step(ctx, j); err != nil && ctx.Err() == nil {
		_ = s.fail(ctx, j)
	}
	// An idle poll during backoff is not a recovered page. Preserve the latest
	// failure until an actual page succeeds; status details remain in each job.
	s.failed.Store(err != nil)
	return err
}
func (s *IndexService) start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		select {
		case <-s.done:
		default:
			if s.stopping {
				return ErrIndexConsumerStopped
			}
			return nil
		}
	}
	worker, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel, s.done, s.stopping = cancel, done, false
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for worker.Err() == nil {
			page, stop := context.WithTimeout(worker, 10*time.Second)
			_ = s.runPage(page)
			stop()
			select {
			case <-worker.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}
func (s *IndexService) stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel == nil {
		return nil
	}
	s.stopping = true
	s.cancel()
	select {
	case <-s.done:
		s.cancel = nil
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
