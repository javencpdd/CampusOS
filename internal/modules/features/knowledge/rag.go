package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

const PortPublicLocalRAG = "knowledge.public-local-rag/v1"
const PublicLocalRAGVersion = "campusos.knowledge-public-local-rag/v1"
const PortPublicRAG = "knowledge.public-rag/v1"
const PublicRAGVersion = "campusos.knowledge-public-rag/v1"
const maxRAGEvidence = 3

var (
	ErrRAGDenied      = errors.New("knowledge.rag_denied")
	ErrRAGChanged     = errors.New("knowledge.rag_changed")
	ErrRAGUnavailable = errors.New("knowledge.rag_unavailable")
	ErrRAGAnswer      = errors.New("knowledge.rag_invalid_answer")
)

type LocalModelPlan struct {
	ProfileID                        string
	ProfileRevision, BindingRevision int64
}
type LocalModelAnswer struct{ Content, RunID string }

// LocalAnswerModel is a late-bound composition port, independent of AI's
// implementation. No plugin, endpoint, Secret or private source reader enters it.
type LocalAnswerModel interface {
	PlanLocal(context.Context, identityport.SessionPrincipalResolver, int64) (LocalModelPlan, error)
	AnswerLocal(context.Context, identityport.SessionPrincipalResolver, string, int64, string, func(context.Context) error) (LocalModelAnswer, error)
}
type PublicLocalQuestion struct {
	CollectionID    string `json:"collection_id"`
	Question        string `json:"question"`
	RunID           string `json:"run_id"`
	BindingRevision int64  `json:"binding_revision,string"`
}
type RAGEvidence struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Excerpt  string         `json:"excerpt"`
	Citation SearchCitation `json:"citation"`
}
type PublicLocalAnswer struct {
	Contract  string        `json:"contract"`
	Status    string        `json:"status"`
	Reason    string        `json:"reason,omitempty"`
	Answer    string        `json:"answer"`
	RunID     string        `json:"run_id,omitempty"`
	Citations []RAGEvidence `json:"citations"`
}
type PublicLocalRAG struct {
	retrieval   *RetrievalService
	collections *CollectionService
	indexer     *IndexService
	model       LocalAnswerModel
	managed     bool
	lifecycle   sync.Mutex
	mu          sync.Mutex
	stopped     bool
	lifetime    context.Context
	cancel      context.CancelFunc
	calls       sync.WaitGroup
}

func newPublicLocalRAG(r *RetrievalService, c *CollectionService, i *IndexService, m LocalAnswerModel) *PublicLocalRAG {
	s := &PublicLocalRAG{retrieval: r, collections: c, indexer: i, model: m}
	s.start()
	return s
}
func newPublicManagedRAG(r *RetrievalService, c *CollectionService, i *IndexService, m LocalAnswerModel) *PublicLocalRAG {
	s := newPublicLocalRAG(r, c, i, m)
	s.managed = true
	return s
}
func (s *PublicLocalRAG) contract() string {
	if s.managed {
		return PublicRAGVersion
	}
	return PublicLocalRAGVersion
}
func (s *PublicLocalRAG) start() {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel == nil {
		s.lifetime, s.cancel = context.WithCancel(context.Background())
	}
	s.stopped = false
}
func (s *PublicLocalRAG) stop() {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	s.stopped = true
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.calls.Wait()
	s.mu.Lock()
	s.cancel = nil
	s.mu.Unlock()
}
func (s *PublicLocalRAG) check(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return ErrRAGUnavailable
	}
	if s.collections == nil || s.indexer == nil || s.retrieval == nil || nilSourceAdapter(s.model) {
		return ErrRAGUnavailable
	}
	if s.collections.check(ctx) != nil || s.retrieval.stopped.Load() {
		return ErrRAGUnavailable
	}
	return nil
}

type ragInput struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
}
type ragSnapshot struct {
	evidence RAGEvidence
	document IndexDocument
}

func (s *PublicLocalRAG) capture(ctx context.Context, hit PublicSearchHit, terms []string) (ragSnapshot, error) {
	var out ragSnapshot
	e := s.collections.reliable.CheckedRead(ctx, func(tx context.Context) error {
		c := hit.Citation
		state, e := s.collections.state(tx, c.CollectionID, false)
		if e != nil {
			return e
		}
		var member Member
		for _, m := range state.Members {
			if m.ID == c.MemberID {
				member = m
				break
			}
		}
		if member.ID == "" || member.Kind != c.Source.Kind || member.SourceID != c.Source.ID {
			return ErrRAGChanged
		}
		id := indexHash("document/v1\x00" + c.CollectionID + "\x00" + member.SelectionID + "\x00" + c.Source.Kind + "\x00" + c.Source.ID)
		d, e := s.indexer.CurrentDocument(tx, id)
		if e != nil {
			return e
		}
		if d.CollectionID != c.CollectionID || d.MemberID != c.MemberID || d.Source != c.Source || d.VisibilityRevision != c.VisibilityRevision || d.Generation != c.Generation || d.Title != hit.Title || d.URL != c.URL || c.ChunkIndex < 0 || c.ChunkIndex >= len(d.Chunks) {
			return ErrRAGChanged
		}
		window, _ := lexicalWindow(RetrievalDocument{d.ID, d.Title, d.Chunks}, c.ChunkIndex, terms)
		if searchSnippet(window, terms) != hit.Snippet {
			return ErrRAGChanged
		}
		d.Chunks = append([]string(nil), d.Chunks...)
		out = ragSnapshot{RAGEvidence{Title: hit.Title, Excerpt: hit.Snippet, Citation: c}, d}
		return nil
	})
	if e != nil {
		return ragSnapshot{}, ErrRAGChanged
	}
	return out, nil
}
func (s *PublicLocalRAG) recheck(ctx context.Context, items []ragSnapshot) error {
	return s.collections.reliable.CheckedRead(ctx, func(tx context.Context) error {
		for _, item := range items {
			d, e := s.indexer.CurrentDocument(tx, item.document.ID)
			if e != nil {
				return fmt.Errorf("%w: index_read context=%v", ErrRAGChanged, tx.Err())
			}
			if !reflect.DeepEqual(d, item.document) {
				return fmt.Errorf("%w: index_revision context=%v", ErrRAGChanged, tx.Err())
			}
			// Reopen the exact source revision as well; neither the projection nor the
			// search snippet is an independent resource/visibility authority.
			c := item.evidence.Citation
			v, e := s.collections.Source(tx, c.CollectionID, c.MemberID, c.VisibilityRevision)
			if e != nil {
				return fmt.Errorf("%w: source_read context=%v", ErrRAGChanged, tx.Err())
			}
			if v.Source.Reference != c.Source || v.Source.Title != item.evidence.Title || v.Source.URL != c.URL {
				return fmt.Errorf("%w: source_revision context=%v", ErrRAGChanged, tx.Err())
			}
		}
		return s.check(tx)
	})
}

func (s *PublicLocalRAG) Answer(ctx context.Context, resolve identityport.SessionPrincipalResolver, req PublicLocalQuestion) (PublicLocalAnswer, error) {
	return s.answer(ctx, resolve, req, nil)
}

// AnswerChecked joins the compiled caller's current plugin grant/profile proof
// to the ordinary source guard, including the model reservation transaction.
func (s *PublicLocalRAG) AnswerChecked(ctx context.Context, resolve identityport.SessionPrincipalResolver, req PublicLocalQuestion, guard func(context.Context) error) (PublicLocalAnswer, error) {
	if guard == nil {
		return PublicLocalAnswer{}, ErrRAGDenied
	}
	return s.answer(ctx, resolve, req, guard)
}
func (s *PublicLocalRAG) answer(ctx context.Context, resolve identityport.SessionPrincipalResolver, req PublicLocalQuestion, external func(context.Context) error) (PublicLocalAnswer, error) {
	var zero PublicLocalAnswer
	if s == nil || !opaque("kc_", req.CollectionID) || !opaque("ar_", req.RunID) || req.BindingRevision < 1 {
		return zero, ErrInvalid
	}
	terms, e := searchTerms(req.Question)
	if e != nil {
		return zero, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 70*time.Second)
	defer cancel()
	if e = s.check(ctx); e != nil {
		return zero, e
	}
	if external != nil {
		if e = external(ctx); e != nil {
			return zero, ErrRAGDenied
		}
	}
	if resolve == nil {
		return zero, ErrRAGDenied
	}
	principal, e := resolve(ctx)
	if e != nil || principal.Validate() != nil || principal.Actor.Kind != "user" || principal.Audience != "user" {
		return zero, ErrRAGDenied
	}
	bound := func(c context.Context) (identityport.PrincipalContext, error) {
		p, e := resolve(c)
		if e != nil || p.Validate() != nil || !reflect.DeepEqual(p, principal) {
			return identityport.PrincipalContext{}, ErrRAGDenied
		}
		return p, nil
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return zero, ErrRAGUnavailable
	}
	s.calls.Add(1)
	lifetime := s.lifetime
	s.mu.Unlock()
	defer s.calls.Done()
	stop := context.AfterFunc(lifetime, cancel)
	defer stop()
	plan, e := s.model.PlanLocal(ctx, bound, req.BindingRevision)
	if e != nil || (plan.ProfileID != "local-chat" && (!s.managed || plan.ProfileID != "remote-chat")) || plan.ProfileRevision < 1 || plan.BindingRevision != req.BindingRevision {
		return zero, ErrRAGUnavailable
	}
	current := func(c context.Context) error {
		if external != nil {
			if e := external(c); e != nil {
				return ErrRAGDenied
			}
		}
		if e := s.check(c); e != nil {
			return e
		}
		if _, e := bound(c); e != nil {
			return ErrRAGDenied
		}
		p, e := s.model.PlanLocal(c, bound, req.BindingRevision)
		if e != nil {
			return fmt.Errorf("%w: model_plan_read context=%v", ErrRAGChanged, c.Err())
		}
		if p != plan {
			return fmt.Errorf("%w: model_plan_revision context=%v", ErrRAGChanged, c.Err())
		}
		return nil
	}
	page, e := s.retrieval.SearchPublic(ctx, PublicSearchRequest{req.CollectionID, req.Question, 1, maxRAGEvidence})
	if e != nil {
		return zero, ErrRAGUnavailable
	}
	if len(page.Items) == 0 {
		if e = current(ctx); e != nil {
			return zero, e
		}
		return PublicLocalAnswer{Contract: s.contract(), Status: "refused", Reason: "no_evidence", Citations: []RAGEvidence{}}, nil
	}
	if len(page.Items) > maxRAGEvidence {
		return zero, ErrRAGUnavailable
	}
	snapshots := make([]ragSnapshot, 0, len(page.Items))
	inputs := make([]ragInput, 0, len(page.Items))
	for i, hit := range page.Items {
		if hit.Citation.CollectionID != req.CollectionID || hit.Citation.Generation != page.Generation {
			return zero, ErrRAGChanged
		}
		snap, e := s.capture(ctx, hit, terms)
		if e != nil {
			return zero, e
		}
		snap.evidence.ID = fmt.Sprintf("E%d", i+1)
		snapshots = append(snapshots, snap)
		inputs = append(inputs, ragInput{snap.evidence.ID, snap.evidence.Title, snap.evidence.Excerpt})
	}
	guard := func(c context.Context) error {
		if e := current(c); e != nil {
			return e
		}
		if e := s.recheck(c, snapshots); e != nil {
			return e
		}
		return current(c)
	}
	if e = guard(ctx); e != nil {
		return zero, e
	}
	payload, e := json.Marshal(struct {
		Question string `json:"question"`
		Evidence any    `json:"evidence"`
	}{req.Question, inputs})
	if e != nil || len(payload) > 16*1024 {
		return zero, ErrInvalid
	}
	result, e := s.model.AnswerLocal(ctx, bound, req.RunID, req.BindingRevision, string(payload), guard)
	if e != nil {
		if err := guard(ctx); err != nil {
			return zero, err
		}
		return zero, ErrRAGUnavailable
	}
	if result.RunID != req.RunID {
		return zero, ErrRAGAnswer
	}
	// The model returns untrusted buffered data. Only host-created evidence IDs
	// select host-created current citations; no model URL or scope is followed.
	if len(result.Content) > 8192 || identityport.ValidateClosedHTTPObject([]byte(result.Content), []string{"answer", "citations"}, 8192) != nil {
		return zero, ErrRAGAnswer
	}
	var generated struct {
		Answer    string   `json:"answer"`
		Citations []string `json:"citations"`
	}
	if json.Unmarshal([]byte(result.Content), &generated) != nil || len(generated.Answer) > 4096 || len(generated.Citations) > maxRAGEvidence {
		return zero, ErrRAGAnswer
	}
	out := PublicLocalAnswer{Contract: s.contract(), Status: "answered", Answer: generated.Answer, RunID: result.RunID, Citations: []RAGEvidence{}}
	seen := map[string]bool{}
	for _, id := range generated.Citations {
		if seen[id] {
			return zero, ErrRAGAnswer
		}
		seen[id] = true
		found := false
		for _, snap := range snapshots {
			if snap.evidence.ID == id {
				out.Citations = append(out.Citations, snap.evidence)
				found = true
				break
			}
		}
		if !found {
			return zero, ErrRAGAnswer
		}
	}
	if strings.TrimSpace(out.Answer) == "" {
		if len(out.Citations) != 0 || out.Answer != "" {
			return zero, ErrRAGAnswer
		}
		out.Status, out.Reason = "refused", "model_no_evidence"
	} else if len(out.Citations) == 0 {
		return zero, ErrRAGAnswer
	}
	if e = guard(ctx); e != nil {
		return zero, e
	}
	return out, nil
}
