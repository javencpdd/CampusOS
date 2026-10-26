package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

var ErrPrivateKeyUnavailable = errors.New("private derivative key or envelope unavailable")

type PrivateSource struct {
	Kind     string `json:"kind"`
	ID       string `json:"source_id"`
	Revision string `json:"revision"`
	Format   string `json:"format"`
	Title    string `json:"title,omitempty"`
	Text     string `json:"text,omitempty"`
}
type PrivateSourceReader interface {
	ReadPrivateSource(context.Context, identityport.SessionPrincipalResolver, string, string, bool) (PrivateSource, error)
	// Current returns facts only to reconcile existing consent; never original bytes.
	CurrentPrivateSource(context.Context, string, string, string, string) (bool, error)
}
type privatePayload struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}
type PrivateIndexService struct {
	repo     *PgRepository
	vault    *PrivateVault
	reader   PrivateSourceReader
	reliable *reliability.Service
	enabled  func() bool
	stopped  atomic.Bool
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
}

func (s *PrivateIndexService) available() bool {
	return s != nil && s.repo != nil && s.vault.Available() && !nilSourceAdapter(s.reader) && s.enabled != nil && s.enabled() && !s.stopped.Load()
}
func (s *PrivateIndexService) check(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s != nil && s.enabled != nil && !s.enabled() {
		return ErrDisabled
	}
	if !s.available() {
		return ErrPrivateUnavailable
	}
	return nil
}
func privateKind(k string) bool      { return k == "personal_document" || k == "community_thread" }
func privateSupported(f string) bool { return f == "text" || f == "markdown" || f == "campusdoc" }
func privateAAD(r privateRow) []byte {
	b, _ := json.Marshal([]string{"campusos.private-index/v1", r.ID, r.Owner, r.Kind, r.SourceID, r.Revision, r.Format})
	return b
}
func boundPrivateUser(ctx context.Context, resolve identityport.SessionPrincipalResolver) (identityport.PrincipalContext, identityport.SessionPrincipalResolver, error) {
	p, e := privateUser(ctx, resolve)
	if e != nil {
		return p, nil, e
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		q, e := privateUser(ctx, resolve)
		if e != nil {
			return q, e
		}
		if !reflect.DeepEqual(p, q) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return q, nil
	}
	return p, bound, nil
}
func (s *PrivateIndexService) Preview(ctx context.Context, resolve identityport.SessionPrincipalResolver, kind, id string) (PrivateSource, error) {
	if e := s.check(ctx); e != nil {
		return PrivateSource{}, e
	}
	if !privateKind(kind) || !sourceID(id) {
		return PrivateSource{}, ErrInvalid
	}
	_, bound, e := boundPrivateUser(ctx, resolve)
	if e != nil {
		return PrivateSource{}, e
	}
	src, e := s.reader.ReadPrivateSource(ctx, bound, kind, id, false)
	if e != nil {
		return PrivateSource{}, e
	}
	if _, e = bound(ctx); e != nil {
		return PrivateSource{}, e
	}
	src.Text = ""
	if e = s.check(ctx); e != nil {
		return PrivateSource{}, e
	}
	return src, nil
}
func (s *PrivateIndexService) Select(ctx context.Context, resolve identityport.SessionPrincipalResolver, kind, id, revision string, consent bool) (privateRow, error) {
	var out privateRow
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if e := s.check(ctx); e != nil {
		return out, e
	}
	if !consent || !privateKind(kind) || !sourceID(id) || !validPrivateRevision(revision) {
		return out, ErrInvalid
	}
	p, bound, e := boundPrivateUser(ctx, resolve)
	if e != nil {
		return out, e
	}
	src, e := s.reader.ReadPrivateSource(ctx, bound, kind, id, true)
	if e != nil {
		return out, e
	}
	if src.Kind != kind || src.ID != id || src.Revision != revision {
		return out, ErrSourceChanged
	}
	if !privateSupported(src.Format) {
		return out, ErrUnsupportedSource
	}
	if !utf8.ValidString(src.Text) || len(src.Text) > MaxSourceBodyBytes || len(src.Title) > MaxSourceTitleBytes || strings.TrimSpace(src.Text) == "" {
		return out, ErrSourceTooLarge
	}
	row := privateRow{ID: privateID(), Owner: p.Actor.ID, Kind: kind, SourceID: id, Revision: revision, Format: src.Format}
	plain, e := json.Marshal(privatePayload{Title: src.Title, Text: src.Text})
	if e != nil {
		return out, e
	}
	defer clear(plain)
	row.Envelope, e = s.vault.SealNew(row.ID, plain, privateAAD(row))
	if e != nil {
		return out, e
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.vault.Retire(row.ID)
		}
	}()
	e = s.reliable.Execute(ctx, reliability.Command{Code: "knowledge.private.select", ActorType: "user", ActorID: p.Actor.ID, ResourceType: "knowledge_private_source", ResourceID: row.ID, OperationCode: "knowledge.private.select", EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("knowledge.private.changed.v1", "knowledge_private_source", row.ID, map[string]string{"id": row.ID})
	}}, func(tx context.Context) error {
		if _, e := bound(tx); e != nil {
			return e
		}
		if e := s.check(tx); e != nil {
			return e
		}
		current, e := s.reader.ReadPrivateSource(tx, bound, kind, id, false)
		if e != nil {
			return e
		}
		if current.Revision != revision {
			return ErrSourceChanged
		}
		if !s.vault.Has(row.ID) {
			return ErrPrivateUnavailable
		}
		out, e = s.repo.privateInsert(tx, row)
		return e
	})
	if e != nil {
		return privateRow{}, e
	}
	committed = true
	return out, nil
}
func validPrivateRevision(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (s *PrivateIndexService) List(ctx context.Context, resolve identityport.SessionPrincipalResolver) ([]privateRow, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if e := s.check(ctx); e != nil {
		return nil, e
	}
	p, bound, e := boundPrivateUser(ctx, resolve)
	if e != nil {
		return nil, e
	}
	rows, e := s.repo.privateList(ctx, p.Actor.ID)
	if e != nil {
		return nil, e
	}
	if len(rows) > 100 {
		return nil, ErrCapacity
	}
	for i := range rows {
		current, err := s.reader.ReadPrivateSource(ctx, bound, rows[i].Kind, rows[i].SourceID, false)
		if err != nil || current.Revision != rows[i].Revision {
			rows[i].State = "unavailable"
		} else {
			rows[i].Title = current.Title
		}

		if !s.vault.Has(rows[i].ID) {
			rows[i].State = "unavailable"
		}
	}
	if _, e = bound(ctx); e != nil {
		return nil, e
	}
	return rows, s.check(ctx)
}
func (s *PrivateIndexService) Withdraw(ctx context.Context, resolve identityport.SessionPrincipalResolver, id string) error {
	if e := s.check(ctx); e != nil {
		return e
	}
	if !validPrivateID(id) {
		return ErrInvalid
	}
	p, bound, e := boundPrivateUser(ctx, resolve)
	if e != nil {
		return e
	}
	r, e := s.repo.privateGet(ctx, p.Actor.ID, id, false)
	if e != nil {
		return e
	}
	if _, e = bound(ctx); e != nil {
		return e
	}
	// Retirement commits outside the DB before deletion. A failed audit/COMMIT
	// can retain metadata but must never restore access or a deleted key.
	if e = s.vault.Retire(r.ID); e != nil {
		return e
	}
	return s.reliable.Execute(ctx, reliability.Command{Code: "knowledge.private.withdraw", ActorType: "user", ActorID: p.Actor.ID, ResourceType: "knowledge_private_source", ResourceID: id, OperationCode: "knowledge.private.withdraw", EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("knowledge.private.changed.v1", "knowledge_private_source", id, map[string]string{"id": id})
	}}, func(tx context.Context) error {
		if _, e := bound(tx); e != nil {
			return e
		}
		if _, e := s.repo.privateGet(tx, p.Actor.ID, id, true); e != nil {
			return e
		}
		return s.repo.privateDelete(tx, p.Actor.ID, id)
	})
}
func (s *PrivateIndexService) checked(ctx context.Context, resolve identityport.SessionPrincipalResolver, id, revision string) (privateRow, privatePayload, error) {
	var empty privatePayload
	if e := s.check(ctx); e != nil {
		return privateRow{}, empty, e
	}
	p, bound, e := boundPrivateUser(ctx, resolve)
	if e != nil {
		return privateRow{}, empty, e
	}
	r, e := s.repo.privateGet(ctx, p.Actor.ID, id, false)
	if e != nil {
		return r, empty, e
	}
	if r.Revision != revision {
		return r, empty, ErrSourceChanged
	}
	if r.State != "ready" {
		return r, empty, ErrIndexNotReady
	}
	current, e := s.reader.ReadPrivateSource(ctx, bound, r.Kind, r.SourceID, false)
	if e != nil {
		return r, empty, e
	}
	if current.Revision != r.Revision {
		return r, empty, ErrSourceChanged
	}
	plain, e := s.vault.Open(r.ID, r.Envelope, privateAAD(r))
	if e != nil {
		return r, empty, ErrPrivateKeyUnavailable
	}
	defer clear(plain)
	var payload privatePayload
	if json.Unmarshal(plain, &payload) != nil || len(payload.Text) > MaxSourceBodyBytes || len(payload.Title) > MaxSourceTitleBytes {
		return r, empty, ErrPrivateKeyUnavailable
	}
	if _, e = bound(ctx); e != nil {
		return r, empty, e
	}
	return r, payload, s.check(ctx)
}

type PrivateSearchHit struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	SourceID string `json:"source_id"`
	Revision string `json:"revision"`
	Title    string `json:"title"`
	Snippet  string `json:"snippet"`
	Score    int    `json:"score"`
}
type PrivateSearchPage struct {
	Items []PrivateSearchHit `json:"items"`
	Total int                `json:"total"`
}

func (s *PrivateIndexService) Search(ctx context.Context, resolve identityport.SessionPrincipalResolver, query string) (PrivateSearchPage, error) {
	empty := PrivateSearchPage{}
	if e := s.check(ctx); e != nil {
		return empty, e
	}
	terms, e := searchTerms(query)
	if e != nil {
		return empty, e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, bound, e := boundPrivateUser(ctx, resolve)
	if e != nil {
		return empty, e
	}
	rows, e := s.repo.privateList(ctx, p.Actor.ID)
	if e != nil {
		return empty, e
	}
	if len(rows) > 100 {
		return empty, ErrCapacity
	}
	hits := []PrivateSearchHit{}
	visible := []privateRow{}
	bytes := 0
	for _, row := range rows {
		r, content, e := s.checked(ctx, bound, row.ID, row.Revision)
		if e != nil {
			if errors.Is(e, ErrPrivateSourceMissing) || errors.Is(e, ErrSourceChanged) || errors.Is(e, ErrIndexNotReady) || errors.Is(e, ErrPrivateKeyUnavailable) {
				continue
			}
			return empty, e
		}
		bytes += len(content.Title) + len(content.Text)
		if bytes > 4*1024*1024 {
			return empty, ErrCapacity
		}
		visible = append(visible, r)
		title, body := strings.ToLower(content.Title), strings.ToLower(content.Text)
		score := 0
		matched := true
		for _, term := range terms {
			a, b := strings.Contains(title, term), strings.Contains(body, term)
			if !a && !b {
				matched = false
				break
			}
			if a {
				score += 10
			}
			if b {
				score++
			}
		}
		if matched {
			snippet := []rune(content.Text)
			if len(snippet) > 240 {
				snippet = snippet[:240]
			}
			hits = append(hits, PrivateSearchHit{ID: r.ID, Kind: r.Kind, SourceID: r.SourceID, Revision: r.Revision, Title: content.Title, Snippet: string(snippet), Score: score})
		}
	}
	// Recheck every counted candidate, including those outside a returned page.
	for _, r := range visible {
		current, e := s.repo.privateGet(ctx, p.Actor.ID, r.ID, false)
		if e != nil || current.Version != r.Version || !s.vault.Has(r.ID) {
			return empty, ErrSourceChanged
		}
		src, e := s.reader.ReadPrivateSource(ctx, bound, r.Kind, r.SourceID, false)
		if e != nil {
			return empty, e
		}
		if src.Revision != r.Revision {
			return empty, ErrSourceChanged
		}
	}
	if _, e = bound(ctx); e != nil {
		return empty, e
	}
	if e = s.check(ctx); e != nil {
		return empty, e
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].ID < hits[j].ID
	})
	return PrivateSearchPage{Items: hits, Total: len(hits)}, nil
}
func (s *PrivateIndexService) Source(ctx context.Context, resolve identityport.SessionPrincipalResolver, id, revision string) (PrivateSource, error) {
	_, bound, e := boundPrivateUser(ctx, resolve)
	if e != nil {
		return PrivateSource{}, e
	}
	r, p, e := s.checked(ctx, bound, id, revision)
	if e != nil {
		return PrivateSource{}, e
	}
	current, e := s.repo.privateGet(ctx, r.Owner, r.ID, false)
	if e != nil || current.Version != r.Version || !s.vault.Has(r.ID) {
		return PrivateSource{}, ErrSourceChanged
	}
	src, e := s.reader.ReadPrivateSource(ctx, bound, r.Kind, r.SourceID, false)
	if e != nil {
		return PrivateSource{}, e
	}
	if src.Revision != r.Revision {
		return PrivateSource{}, ErrSourceChanged
	}
	if _, e = bound(ctx); e != nil {
		return PrivateSource{}, e
	}
	return PrivateSource{Kind: r.Kind, ID: r.SourceID, Revision: r.Revision, Format: r.Format, Title: p.Title, Text: p.Text}, s.check(ctx)
}

func (s *PrivateIndexService) tick(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if e := s.check(ctx); e != nil {
		return e
	}
	var r privateRow
	e := s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		var e error
		r, e = s.repo.privateClaim(tx, time.Now().UnixMilli())
		return e
	})
	if e != nil {
		return e
	}
	current, e := s.reader.CurrentPrivateSource(ctx, r.Owner, r.Kind, r.SourceID, r.Revision)
	if e == nil && !current {
		if e = s.vault.Retire(r.ID); e != nil {
			return e
		}
		return s.reliable.CheckedRead(ctx, func(tx context.Context) error {
			if _, e := s.repo.privateGet(tx, r.Owner, r.ID, true); e != nil {
				return e
			}
			return s.repo.privateDelete(tx, r.Owner, r.ID)
		})
	}
	code := ""
	if e != nil {
		code = "source_unavailable"
	} else if !s.vault.Has(r.ID) {
		code = "key_unavailable"
	}
	finishCtx := ctx
	if code != "" && ctx.Err() != nil {
		var cancel context.CancelFunc
		finishCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
		defer cancel()
	}
	return s.reliable.CheckedRead(finishCtx, func(tx context.Context) error { return s.repo.privateFinish(tx, r, time.Now().UnixMilli(), code) })
}
func (s *PrivateIndexService) start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	s.stopped.Store(false)
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.available() {
					_ = s.vault.PruneOrphans(func(id string) (bool, error) {
						var found bool
						e := s.repo.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_private_sources WHERE id=$1)`, id).Scan(&found)
						return found, e
					})
					for i := 0; i < 8; i++ {
						if e := s.tick(ctx); e != nil {
							break
						}
					}
				}
			}
		}
	}()
}
func (s *PrivateIndexService) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped.Store(true)
	if s.cancel != nil {
		s.cancel()
		<-s.done
		s.cancel = nil
	}
	s.vault.closeScan()
}

func (s *PrivateIndexService) Retry(ctx context.Context, resolve identityport.SessionPrincipalResolver, id string) error {
	if e := s.check(ctx); e != nil {
		return e
	}
	if !validPrivateID(id) {
		return ErrInvalid
	}
	p, bound, e := boundPrivateUser(ctx, resolve)
	if e != nil {
		return e
	}
	return s.reliable.Execute(ctx, reliability.Command{Code: "knowledge.private.retry", ActorType: "user", ActorID: p.Actor.ID, ResourceType: "knowledge_private_source", ResourceID: id, OperationCode: "knowledge.private.retry", EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("knowledge.private.changed.v1", "knowledge_private_source", id, map[string]string{"id": id})
	}}, func(tx context.Context) error {
		r, e := s.repo.privateGet(tx, p.Actor.ID, id, true)
		if e != nil {
			return e
		}
		current, e := s.reader.ReadPrivateSource(tx, bound, r.Kind, r.SourceID, false)
		if e != nil {
			return e
		}
		if current.Revision != r.Revision {
			return ErrSourceChanged
		}
		if !s.vault.Has(r.ID) {
			return ErrPrivateUnavailable
		}
		if r.TaskState != "retry" && r.TaskState != "dead" {
			return ErrConflict
		}
		_, e = transaction.ExecutorFor(tx, s.repo.pool).Exec(tx, `UPDATE knowledge_private_tasks SET state='pending',attempts=0,error_code='',lease_token=nextval('knowledge_private_epoch_seq'),lease_until_ms=0,available_at_ms=0 WHERE source_id=$1`, r.ID)
		return e
	})
}
