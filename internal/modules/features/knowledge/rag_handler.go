package knowledge

import (
	"errors"
	"strconv"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type ManagedRAGModel interface {
	LocalAnswerModel
	PrivateLocalAnswerModel
}

var ErrRAGStepUnsupported = errors.New("knowledge.rag_step_unsupported")

// All optional model-assisted stages are explicitly unsupported in this text
// lexical pipeline. Index consent, a chat approval, or plugin declarations
// cannot silently authorize another endpoint, purpose or processing step.
func ValidateRAGStep(step string) error {
	if step != "chat" {
		return ErrRAGStepUnsupported
	}
	return nil
}
func validBindingRevision(text string) (int64, error) {
	n, e := strconv.ParseInt(text, 10, 64)
	if e != nil || n < 1 || strconv.FormatInt(n, 10) != text {
		return 0, ErrInvalid
	}
	return n, nil
}
func (h *Handler) RAGStatus(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if !noQuery(c) {
		return
	}
	if h == nil || h.publicRAG == nil || nilSourceAdapter(h.ragModel) || h.publicRAG.check(c.Request.Context()) != nil {
		knowledgeError(c, ErrRAGUnavailable)
		return
	}
	// The revision is read by the trusted adapter, never inferred from clients.
	p, e := h.ragModel.PlanPrivateLocal(c.Request.Context(), h.privateResolver(c), 0)
	if e != nil {
		knowledgeError(c, ErrRAGUnavailable)
		return
	}
	response.Success(c, gin.H{"model": p, "supported_steps": []string{"chat"}, "unsupported_steps": []string{"embedding", "rerank", "parser", "query_rewrite", "telemetry"}})
}
func (h *Handler) RAGPublicAnswer(c *gin.Context) {
	var req struct {
		CollectionID string `json:"collection_id"`
		Question     string `json:"question"`
		RunID        string `json:"run_id"`
		Revision     string `json:"binding_revision"`
		Step         string `json:"step"`
	}
	if !h.privateBody(c, &req, "collection_id", "question", "run_id", "binding_revision", "step") {
		return
	}
	rev, e := validBindingRevision(req.Revision)
	if e != nil || ValidateRAGStep(req.Step) != nil {
		knowledgeError(c, ErrInvalid)
		return
	}
	if h.publicRAG == nil {
		knowledgeError(c, ErrRAGUnavailable)
		return
	}
	out, e := h.publicRAG.Answer(c.Request.Context(), h.privateResolver(c), PublicLocalQuestion{req.CollectionID, req.Question, req.RunID, rev})
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}

type privateRAGRequest struct {
	SelectionID     string `json:"selection_id"`
	Revision        string `json:"revision"`
	Question        string `json:"question"`
	RunID           string `json:"run_id"`
	BindingRevision string `json:"binding_revision"`
	Step            string `json:"step"`
	ApprovalID      string `json:"approval_id"`
	Digest          string `json:"review_digest"`
	Consented       *bool  `json:"consented"`
}

func (h *Handler) RAGPrivatePrepare(c *gin.Context) { h.ragPrivateRequest(c, false) }
func (h *Handler) RAGPrivateAnswer(c *gin.Context)  { h.ragPrivateRequest(c, true) }
func (h *Handler) ragPrivateRequest(c *gin.Context, answer bool) {
	fields := []string{"selection_id", "revision", "question", "run_id", "binding_revision", "step"}
	if answer {
		fields = append(fields, "approval_id", "review_digest", "consented")
	}
	var req privateRAGRequest
	if !h.privateBody(c, &req, fields...) {
		return
	}
	rev, e := validBindingRevision(req.BindingRevision)
	if e != nil || ValidateRAGStep(req.Step) != nil {
		knowledgeError(c, ErrInvalid)
		return
	}
	if h.privateRAG == nil {
		knowledgeError(c, ErrRAGUnavailable)
		return
	}
	q := PrivateLocalQuestion{req.SelectionID, req.Revision, req.Question, req.RunID, rev}
	if !answer {
		out, e := h.privateRAG.Prepare(c.Request.Context(), h.privateResolver(c), q)
		if e != nil {
			knowledgeError(c, e)
			return
		}
		response.Success(c, out)
		return
	}
	if req.Consented == nil || !*req.Consented {
		knowledgeError(c, ErrRAGConsent)
		return
	}
	out, e := h.privateRAG.Answer(c.Request.Context(), h.privateResolver(c), q, PrivateLocalApproval{req.ApprovalID, req.Digest, *req.Consented})
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) RAGPrivateRevoke(c *gin.Context) {
	var req struct {
		ID string `json:"approval_id"`
	}
	if !h.privateBody(c, &req, "approval_id") {
		return
	}
	if h.privateRAG == nil {
		knowledgeError(c, ErrRAGUnavailable)
		return
	}
	if e := h.privateRAG.Revoke(c.Request.Context(), h.privateResolver(c), req.ID); e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, gin.H{"revoked": true})
}
