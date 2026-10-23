package knowledge

import (
	"context"
	"encoding/json"
	"io"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// IntegrationSearch reads one exact public collection. POST carries the closed
// search DTO, keeping machine credentials and query text out of URL parameters.
// It neither changes source state nor grants the separate citation-read endpoint.
func (h *Handler) IntegrationSearch(c *gin.Context) {
	token, ok := middleware.IntegrationToken(c)
	if !ok {
		return
	}
	var body struct {
		Query    string `json:"query"`
		Page     int    `json:"page"`
		PageSize int    `json:"page_size"`
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 32769))
	if err != nil || identityport.ValidateIntegrationHTTPJSON(raw, []string{"query", "page", "page_size"}) != nil || json.Unmarshal(raw, &body) != nil {
		knowledgeError(c, ErrInvalid)
		return
	}
	id := c.Param("id")
	if !opaque("kc_", id) || body.Page < 1 || body.Page > 100000 || body.PageSize < 1 || body.PageSize > 50 {
		knowledgeError(c, ErrInvalid)
		return
	}
	if _, err = searchTerms(body.Query); err != nil {
		knowledgeError(c, err)
		return
	}
	if !h.ready(c) {
		return
	}
	if nilSourceAdapter(h.machine) {
		response.ErrorDescriptor(c, apperror.IdentityIntegrationUnavailable, nil)
		return
	}
	if nilSourceAdapter(h.retrieval) {
		knowledgeError(c, ErrRetrievalProvider)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	req := PublicSearchRequest{CollectionID: id, Query: body.Query, Page: body.Page, PageSize: body.PageSize}
	target := identityport.IntegrationAccessTarget{Channel: "integration_http", Endpoint: "knowledge.search", Action: "knowledge.source.read", Resource: identityport.PolicyResourceRef{Kind: "public_collection", ID: id}}
	data, err := h.machine.ReadForIntegration(ctx, token, target, func(tx context.Context) (any, error) {
		// The normal Port performs all current source checks before totals and
		// paging. Its CheckedRead joins the machine transaction; owner row locks
		// survive through the final credential proof, audit/outbox and commit.
		return h.retrieval.SearchPublic(tx, req)
	})
	if err != nil {
		integrationSourceError(c, err)
		return
	}
	out, ok := data.(PublicSearchPage)
	if !ok || out.Page != req.Page || out.PageSize != req.PageSize || out.Provider != BuiltinLexicalProvider {
		knowledgeError(c, ErrRetrievalProvider)
		return
	}
	for _, hit := range out.Items {
		if hit.Citation.CollectionID != id {
			knowledgeError(c, ErrRetrievalProvider)
			return
		}
	}
	if err = h.service.check(ctx); err != nil {
		knowledgeError(c, err)
		return
	}
	if runtime, ok := h.retrieval.(*RetrievalService); ok && runtime.stopped.Load() {
		knowledgeError(c, ErrIndexConsumerStopped)
		return
	}
	response.Success(c, out)
}
