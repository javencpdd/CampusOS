package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
)

const PortPrivateLocalRAG = "knowledge.private-local-rag/v1"
const PrivateLocalRAGVersion = "campusos.knowledge-private-local-rag/v1"
const PortPrivateRAG = "knowledge.private-rag/v1"
const PrivateRAGVersion = "campusos.knowledge-private-rag/v1"

// PrivateLocalModelPlan discloses the exact fixed destination and prompt to the
// owner. It carries no provider credential and is never an authorization fact.
type PrivateLocalModelPlan struct {
	ProfileID       string `json:"profile_id"`
	ProfileRevision int64  `json:"profile_revision,string"`
	BindingRevision int64  `json:"binding_revision,string"`
	Definition      string `json:"definition"`
	Model           string `json:"model"`
	Endpoint        string `json:"endpoint"`
	SystemPrompt    string `json:"system_prompt"`
	MaxTokens       int    `json:"max_tokens"`
}

type PrivateLocalAnswerModel interface {
	PlanPrivateLocal(context.Context, identityport.SessionPrincipalResolver, int64) (PrivateLocalModelPlan, error)
	AnswerLocal(context.Context, identityport.SessionPrincipalResolver, string, int64, string, func(context.Context) error) (LocalModelAnswer, error)
}

type PrivateLocalQuestion struct {
	SelectionID     string `json:"selection_id"`
	Revision        string `json:"revision"`
	Question        string `json:"question"`
	RunID           string `json:"run_id"`
	BindingRevision int64  `json:"binding_revision,string"`
}

type PrivateRAGEvidence struct {
	ID                  string `json:"id"`
	SelectionID         string `json:"selection_id"`
	Kind                string `json:"kind"`
	SourceID            string `json:"source_id"`
	Revision            string `json:"revision"`
	Format              string `json:"format"`
	Title               string `json:"title"`
	Excerpt             string `json:"excerpt"`
	IndexConsentVersion int64  `json:"index_consent_version,string"`
}

type PrivateLocalReview struct {
	Contract   string                `json:"contract"`
	Status     string                `json:"status"`
	Reason     string                `json:"reason,omitempty"`
	Request    PrivateLocalQuestion  `json:"request"`
	Step       string                `json:"step"`
	Model      PrivateLocalModelPlan `json:"model"`
	Content    string                `json:"content"`
	Evidence   []PrivateRAGEvidence  `json:"evidence"`
	Digest     string                `json:"digest,omitempty"`
	ApprovalID string                `json:"approval_id,omitempty"`
	ExpiresAt  int64                 `json:"expires_at_ms,omitempty"`
}

// Consent is separate from index consent. A trusted first-party caller must
// present the exact review, then collect an explicit fresh affirmative action.
type PrivateLocalApproval struct {
	ID           string `json:"id"`
	ReviewDigest string `json:"review_digest"`
	Consented    bool   `json:"consented"`
}

type PrivateLocalAnswer struct {
	Contract  string               `json:"contract"`
	Status    string               `json:"status"`
	Reason    string               `json:"reason,omitempty"`
	Answer    string               `json:"answer"`
	RunID     string               `json:"run_id,omitempty"`
	Citations []PrivateRAGEvidence `json:"citations"`
}

type PrivateLocalRAG struct {
	index     *PrivateIndexService
	model     PrivateLocalAnswerModel
	managed   bool
	approvals *ragApprovalLedger
	lifecycle sync.Mutex
	mu        sync.Mutex
	stopped   bool
	lifetime  context.Context
	cancel    context.CancelFunc
	calls     sync.WaitGroup
}

func newPrivateLocalRAG(index *PrivateIndexService, model PrivateLocalAnswerModel) *PrivateLocalRAG {
	s := &PrivateLocalRAG{index: index, model: model, approvals: newRAGApprovalLedger()}
	s.start()
	return s
}
func newPrivateManagedRAG(index *PrivateIndexService, model PrivateLocalAnswerModel) *PrivateLocalRAG {
	s := newPrivateLocalRAG(index, model)
	s.managed = true
	return s
}
func (s *PrivateLocalRAG) contract() string {
	if s.managed {
		return PrivateRAGVersion
	}
	return PrivateLocalRAGVersion
}
func (s *PrivateLocalRAG) start() {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel == nil {
		s.approvals.reset()
		s.lifetime, s.cancel = context.WithCancel(context.Background())
	}
	s.stopped = false
}
func (s *PrivateLocalRAG) stop() {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	s.stopped = true
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.approvals.stop()
	s.calls.Wait()
	s.mu.Lock()
	s.cancel = nil
	s.mu.Unlock()
}
func (s *PrivateLocalRAG) begin(ctx context.Context, timeout time.Duration) (context.Context, func(), error) {
	if s == nil || s.index == nil || nilSourceAdapter(s.model) {
		return ctx, nil, ErrRAGUnavailable
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return ctx, nil, ErrRAGUnavailable
	}
	s.calls.Add(1)
	lifetime := s.lifetime
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	stop := context.AfterFunc(lifetime, cancel)
	return ctx, func() { stop(); cancel(); s.calls.Done() }, nil
}

type privateRAGSnapshot struct {
	row      privateRow
	evidence PrivateRAGEvidence
}

func ragDigest(v any) string {
	b, _ := json.Marshal(v)
	d := sha256.Sum256(b)
	clear(b)
	return hex.EncodeToString(d[:])
}

func (s *PrivateLocalRAG) snapshot(ctx context.Context, resolve identityport.SessionPrincipalResolver, req PrivateLocalQuestion) (privateRAGSnapshot, bool, error) {
	var zero privateRAGSnapshot
	terms, e := searchTerms(req.Question)
	if e != nil || !validPrivateID(req.SelectionID) || !validPrivateRevision(req.Revision) || !opaque("ar_", req.RunID) || req.BindingRevision < 1 {
		return zero, false, ErrInvalid
	}
	r, body, e := s.index.checked(ctx, resolve, req.SelectionID, req.Revision)
	if e != nil {
		return zero, false, ErrRAGChanged
	}
	evidence := PrivateRAGEvidence{"E1", r.ID, r.Kind, r.SourceID, r.Revision, r.Format, body.Title, searchSnippet(body.Text, terms), r.Version}
	snap := privateRAGSnapshot{r, evidence}
	if e = s.currentSource(ctx, resolve, snap); e != nil {
		return zero, false, e
	}
	title, text := strings.ToLower(body.Title), strings.ToLower(body.Text)
	for _, term := range terms {
		if !strings.Contains(title, term) && !strings.Contains(text, term) {
			return snap, false, nil
		}
	}
	return snap, true, nil
}

func (s *PrivateLocalRAG) currentSource(ctx context.Context, resolve identityport.SessionPrincipalResolver, snap privateRAGSnapshot) error {
	if s.index.check(ctx) != nil {
		return ErrRAGUnavailable
	}
	p, bound, e := boundPrivateUser(ctx, resolve)
	if e != nil || p.Actor.ID != snap.row.Owner {
		return ErrRAGDenied
	}
	r, e := s.index.repo.privateGet(ctx, p.Actor.ID, snap.row.ID, false)
	old := snap.row
	if e != nil || r.State != "ready" || r.ID != old.ID || r.Owner != old.Owner || r.Kind != old.Kind || r.SourceID != old.SourceID || r.Revision != old.Revision || r.Format != old.Format || r.Version != old.Version || !reflect.DeepEqual(r.Envelope, old.Envelope) || !s.index.vault.Has(r.ID) {
		return ErrRAGChanged
	}
	// Existence alone cannot prove current protection: replacing a DEK with a
	// different valid-length key must also invalidate an in-flight answer.
	plain, e := s.index.vault.Open(r.ID, r.Envelope, privateAAD(r))
	if e != nil {
		return ErrRAGChanged
	}
	clear(plain)
	src, e := s.index.reader.ReadPrivateSource(ctx, bound, r.Kind, r.SourceID, false)
	if e != nil || src.Kind != r.Kind || src.ID != r.SourceID || src.Revision != r.Revision || src.Format != r.Format || src.Title != snap.evidence.Title {
		return ErrRAGChanged
	}
	if _, e = bound(ctx); e != nil {
		return ErrRAGDenied
	}
	if s.index.check(ctx) != nil {
		return ErrRAGUnavailable
	}
	return nil
}

func (s *PrivateLocalRAG) review(ctx context.Context, bound identityport.SessionPrincipalResolver, req PrivateLocalQuestion, snap privateRAGSnapshot) (PrivateLocalReview, error) {
	p, e := s.model.PlanPrivateLocal(ctx, bound, req.BindingRevision)
	local := p.ProfileID == "local-chat" && p.Definition == "builtin.local-openai.text"
	remote := s.managed && p.ProfileID == "remote-chat" && p.Definition == "builtin.openai-compatible.text"
	if e != nil || (!local && !remote) || p.ProfileRevision < 1 || p.BindingRevision != req.BindingRevision || p.Model == "" || p.Endpoint == "" || p.SystemPrompt == "" || p.MaxTokens < 1 {
		return PrivateLocalReview{}, ErrRAGUnavailable
	}
	content, e := json.Marshal(struct {
		Question string     `json:"question"`
		Evidence []ragInput `json:"evidence"`
	}{req.Question, []ragInput{{"E1", snap.evidence.Title, snap.evidence.Excerpt}}})
	if e != nil || len(content) > 16*1024 {
		return PrivateLocalReview{}, ErrInvalid
	}
	defer clear(content)
	out := PrivateLocalReview{Contract: s.contract(), Status: "ready", Request: req, Step: "chat", Model: p, Content: string(content), Evidence: []PrivateRAGEvidence{snap.evidence}}
	out.Digest = ragDigest(out)
	return out, nil
}

func (s *PrivateLocalRAG) Prepare(ctx context.Context, resolve identityport.SessionPrincipalResolver, req PrivateLocalQuestion) (PrivateLocalReview, error) {
	var zero PrivateLocalReview
	ctx, done, e := s.begin(ctx, 5*time.Second)
	if e != nil {
		return zero, e
	}
	defer done()
	p, bound, e := boundPrivateUser(ctx, resolve)
	if e != nil {
		return zero, ErrRAGDenied
	}
	snap, matched, e := s.snapshot(ctx, bound, req)
	if e != nil {
		return zero, e
	}
	review, e := s.review(ctx, bound, req, snap)
	if e != nil {
		return zero, e
	}
	if !matched {
		return PrivateLocalReview{Contract: s.contract(), Status: "refused", Reason: "no_evidence", Request: req, Step: "chat", Model: review.Model, Evidence: []PrivateRAGEvidence{}}, nil
	}
	if e = s.currentSource(ctx, bound, snap); e != nil {
		return zero, e
	}
	if _, e = bound(ctx); e != nil {
		return zero, ErrRAGDenied
	}
	id, expiry, e := s.approvals.prepare(ragDigest(p), review.Digest)
	if e != nil {
		return zero, e
	}
	review.ApprovalID, review.ExpiresAt = id, expiry.UnixMilli()
	return review, nil
}

func (s *PrivateLocalRAG) Revoke(ctx context.Context, resolve identityport.SessionPrincipalResolver, id string) error {
	ctx, done, e := s.begin(ctx, 5*time.Second)
	if e != nil {
		return e
	}
	defer done()
	p, _, e := boundPrivateUser(ctx, resolve)
	if e != nil {
		return ErrRAGDenied
	}
	return s.approvals.revoke(id, ragDigest(p))
}

func (s *PrivateLocalRAG) Answer(ctx context.Context, resolve identityport.SessionPrincipalResolver, req PrivateLocalQuestion, approval PrivateLocalApproval) (PrivateLocalAnswer, error) {
	var zero PrivateLocalAnswer
	if !approval.Consented || !opaque("kr_", approval.ID) || !validPrivateRevision(approval.ReviewDigest) {
		return zero, ErrRAGConsent
	}
	ctx, done, e := s.begin(ctx, 70*time.Second)
	if e != nil {
		return zero, e
	}
	defer done()
	p, bound, e := boundPrivateUser(ctx, resolve)
	if e != nil {
		return zero, ErrRAGDenied
	}
	snap, matched, e := s.snapshot(ctx, bound, req)
	if e != nil {
		return zero, e
	}
	if !matched {
		return zero, ErrRAGChanged
	}
	review, e := s.review(ctx, bound, req, snap)
	if e != nil {
		return zero, e
	}
	if review.Digest != approval.ReviewDigest {
		return zero, ErrRAGConsent
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	owner := ragDigest(p)
	if e = s.approvals.consume(approval.ID, owner, review.Digest, cancel); e != nil {
		return zero, e
	}
	defer s.approvals.finish(approval.ID)
	guard := func(c context.Context) error {
		if e := s.approvals.current(approval.ID, owner, review.Digest); e != nil {
			return e
		}
		if _, e := bound(c); e != nil {
			return ErrRAGDenied
		}
		if e := s.currentSource(c, bound, snap); e != nil {
			return e
		}
		current, e := s.model.PlanPrivateLocal(c, bound, req.BindingRevision)
		if e != nil || current != review.Model {
			return ErrRAGChanged
		}
		return s.approvals.current(approval.ID, owner, review.Digest)
	}
	if e = guard(ctx); e != nil {
		return zero, e
	}
	// Required metadata-only consent audit precedes any budget or network. Its
	// failure consumes this one-use approval and never dispatches the model.
	e = s.index.reliable.Execute(ctx, reliability.Command{Code: "knowledge.private.rag.approve", ActorType: "user", ActorID: p.Actor.ID, ResourceType: "knowledge.private.rag", ResourceID: req.RunID, OperationCode: "knowledge.private.rag.approve", EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("knowledge.private.rag.approved.v1", "knowledge.private.rag", req.RunID, map[string]string{"id": req.RunID, "step": "chat", "profile": review.Model.ProfileID})
	}}, guard)
	if e != nil {
		return zero, ErrRAGUnavailable
	}
	result, e := s.model.AnswerLocal(ctx, bound, req.RunID, req.BindingRevision, review.Content, guard)
	if e != nil {
		return zero, ErrRAGUnavailable
	}
	if result.RunID != req.RunID || len(result.Content) > 8192 || identityport.ValidateClosedHTTPObject([]byte(result.Content), []string{"answer", "citations"}, 8192) != nil {
		return zero, ErrRAGAnswer
	}
	var generated struct {
		Answer    string   `json:"answer"`
		Citations []string `json:"citations"`
	}
	if json.Unmarshal([]byte(result.Content), &generated) != nil || len(generated.Answer) > 4096 || len(generated.Citations) > 1 {
		return zero, ErrRAGAnswer
	}
	out := PrivateLocalAnswer{Contract: s.contract(), Status: "answered", Answer: generated.Answer, RunID: result.RunID, Citations: []PrivateRAGEvidence{}}
	if generated.Answer == "" && len(generated.Citations) == 0 {
		out.Status, out.Reason = "refused", "model_no_evidence"
	} else if strings.TrimSpace(generated.Answer) == "" || len(generated.Citations) != 1 || generated.Citations[0] != "E1" {
		return zero, ErrRAGAnswer
	} else {
		out.Citations = []PrivateRAGEvidence{snap.evidence}
	}
	if e = guard(ctx); e != nil {
		return zero, e
	}
	return out, nil
}
