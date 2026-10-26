package knowledge

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	publicRAG      *PublicLocalRAG
	privateRAG     *PrivateLocalRAG
	ragModel       ManagedRAGModel
	privateSources *privateSourcePreflight
	privateIndex   *PrivateIndexService
	userVerifier   identityport.SessionVerifier
	userStrength   identityport.SessionStrengthReader
	service        *CollectionService
	authority      identityport.AdminSessionAuthorityReader
	retrieval      PublicRetrieval
	machine        identityport.IntegrationReadGateway
	mcp            publicMCPState
}

func NewHandler(s *CollectionService, a identityport.AdminSessionAuthorityReader, retrieval PublicRetrieval) *Handler {
	return &Handler{service: s, authority: a, retrieval: retrieval}
}
func (h *Handler) resolver(c *gin.Context) identityport.SessionPrincipalResolver {
	id := c.GetString("admin_account_id")
	claims := &auth.JWTClaims{UserID: id, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		if id == "" || c.GetString("user_id") != id {
			return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
		}
		return identityport.ResolveAdminSessionPrincipal(ctx, h.authority, claims)
	}
}
func knowledgeError(c *gin.Context, e error) {
	c.Header("Cache-Control", "private, no-store")
	switch {
	case errors.Is(e, ErrInvalid), errors.Is(e, ErrInvalidReference), errors.Is(e, ErrRAGStepUnsupported):
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
	case errors.Is(e, ErrConflict), errors.Is(e, ErrSourceChanged), errors.Is(e, identityport.ErrPolicyFactsChanged), errors.Is(e, ErrRAGConsent), errors.Is(e, ErrRAGChanged):
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
	case errors.Is(e, ErrPrivateSourceMissing), errors.Is(e, ErrNotFound), errors.Is(e, ErrSourceUnavailable), errors.Is(e, ErrUnsupportedSource), errors.Is(e, ErrSourceTooLarge):
		response.ErrorDescriptor(c, apperror.ResourceNotFound, nil)
	case errors.Is(e, ErrMFARequired):
		response.ErrorDescriptor(c, apperror.IdentityMFAStepUpRequired, nil)
	case errors.Is(e, identityport.ErrUserSessionPrincipalUnavailable), errors.Is(e, identityport.ErrAdminSessionPrincipalUnavailable):
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
	case errors.Is(e, ErrDenied), errors.Is(e, ErrRAGDenied):
		response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
	case errors.Is(e, ErrDisabled):
		response.ErrorDescriptor(c, apperror.KnowledgeDisabled, nil)
	case errors.Is(e, ErrCapacity):
		response.ErrorDescriptor(c, apperror.KnowledgeCapacity, nil)
	default:
		response.ErrorDescriptor(c, apperror.KnowledgeUnavailable, nil)
	}
}

// DisabledHTTP keeps the Feature path gate consistent with errors from current
// service checks, including a hot disable after a request entered the router.
func DisabledHTTP(c *gin.Context) {
	c.Abort()
	knowledgeError(c, ErrDisabled)
}

func (h *Handler) ready(c *gin.Context) bool {
	c.Header("Cache-Control", "private, no-store")
	if h == nil || h.service == nil {
		knowledgeError(c, ErrSourceUnavailable)
		return false
	}
	return true
}
func (h *Handler) body(c *gin.Context, v any, fields ...string) bool {
	if !h.ready(c) {
		return false
	}
	// Same independent Admin browser double-submit boundary as Identity. Non-
	// browser bearer clients remain possible; browser credentials never bypass it.
	_, refreshCookie := c.Cookie("campusos_admin_refresh")
	if c.GetHeader("X-CampusOS-Client") == "browser" || c.GetHeader("Origin") != "" || c.GetHeader("Sec-Fetch-Site") != "" || refreshCookie == nil {
		cookie, e := c.Cookie("campusos_admin_csrf")
		header := strings.TrimSpace(c.GetHeader("X-Admin-CSRF-Token"))
		if e != nil || cookie == "" || len(cookie) != len(header) || subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) != 1 {
			knowledgeError(c, ErrDenied)
			return false
		}
	}
	const maxBody = 6*(MaxSourceBodyBytes+MaxSourceTitleBytes) + 4096
	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, maxBody+1))
	// The shared Identity closed-object codec rejects unknown/duplicate fields,
	// malformed Unicode, nulls and noncanonical string versions before decoding.
	if e != nil || len(raw) > maxBody || c.Request.URL.RawQuery != "" || identityport.ValidateClosedHTTPObject(raw, fields, maxBody) != nil || json.Unmarshal(raw, v) != nil {
		knowledgeError(c, ErrInvalid)
		return false
	}
	var values map[string]json.RawMessage
	_ = json.Unmarshal(raw, &values)
	for _, field := range []string{"version", "collection_version", "selection_version"} {
		if value, ok := values[field]; ok {
			var text string
			if json.Unmarshal(value, &text) != nil {
				knowledgeError(c, ErrInvalid)
				return false
			}
			n, e := strconv.ParseInt(text, 10, 64)
			if e != nil || n < 1 || strconv.FormatInt(n, 10) != text {
				knowledgeError(c, ErrInvalid)
				return false
			}
		}
	}
	return true
}
func noQuery(c *gin.Context) bool {
	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	if e != nil || len(raw) > 0 || c.Request.URL.RawQuery != "" {
		knowledgeError(c, ErrInvalid)
		return false
	}
	return true
}
func (h *Handler) Inventory(c *gin.Context) {
	if !h.ready(c) || !noQuery(c) {
		return
	}
	kind := "collections"
	if strings.HasSuffix(c.FullPath(), "/officials") {
		kind = "officials"
	}
	out, e := h.service.Inventory(c.Request.Context(), kind, h.resolver(c))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) State(c *gin.Context) {
	if !h.ready(c) || !noQuery(c) {
		return
	}
	out, e := h.service.CollectionState(c.Request.Context(), c.Param("id"), h.resolver(c))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) History(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	before := int64(0)
	for key, values := range c.Request.URL.Query() {
		if key != "before" || len(values) != 1 {
			knowledgeError(c, ErrInvalid)
			return
		}
		n, e := strconv.ParseInt(values[0], 10, 64)
		if e != nil || n < 1 || strconv.FormatInt(n, 10) != values[0] {
			knowledgeError(c, ErrInvalid)
			return
		}
		before = n
	}
	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	if e != nil || len(raw) > 0 {
		knowledgeError(c, ErrInvalid)
		return
	}
	out, e := h.service.OfficialHistory(c.Request.Context(), c.Param("id"), before, h.resolver(c))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) SaveCollection(c *gin.Context) {
	var req struct {
		Title   string `json:"title"`
		Status  string `json:"status"`
		Version int64  `json:"version,string"`
	}
	fields := []string{"title", "status"}
	if c.Param("id") != "" {
		fields = append(fields, "version")
	}
	if !h.body(c, &req, fields...) {
		return
	}
	out, e := h.service.SaveCollection(c.Request.Context(), Collection{ID: c.Param("id"), Title: req.Title, Status: req.Status, Version: req.Version}, h.resolver(c))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) SaveOfficial(c *gin.Context) {
	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Format  string `json:"format"`
		Status  string `json:"status"`
		Version int64  `json:"version,string"`
	}
	fields := []string{"title", "content", "format", "status"}
	if c.Param("id") != "" {
		fields = append(fields, "version")
	}
	if !h.body(c, &req, fields...) {
		return
	}
	out, e := h.service.SaveOfficial(c.Request.Context(), Official{ID: c.Param("id"), Title: req.Title, Content: req.Content, Format: req.Format, Status: req.Status, Version: req.Version}, h.resolver(c))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) SaveSelection(c *gin.Context) {
	var req struct {
		Kind              string `json:"kind"`
		SourceID          string `json:"source_id"`
		Active            *bool  `json:"active"`
		CollectionVersion int64  `json:"collection_version,string"`
		Version           int64  `json:"version,string"`
	}
	fields := []string{"kind", "source_id", "active", "collection_version"}
	if c.Param("selection_id") != "" {
		fields = append(fields, "version")
	}
	if !h.body(c, &req, fields...) {
		return
	}
	if req.Active == nil {
		knowledgeError(c, ErrInvalid)
		return
	}
	out, e := h.service.SaveSelection(c.Request.Context(), c.Param("id"), req.CollectionVersion, Selection{ID: c.Param("selection_id"), Version: req.Version, Kind: req.Kind, SourceID: req.SourceID, Active: *req.Active}, h.resolver(c))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) Refresh(c *gin.Context) {
	var req struct {
		CollectionVersion int64 `json:"collection_version,string"`
		SelectionVersion  int64 `json:"selection_version,string"`
		Page              int   `json:"page"`
	}
	if !h.body(c, &req, "collection_version", "selection_version", "page") {
		return
	}
	out, e := h.service.Refresh(c.Request.Context(), c.Param("id"), c.Param("selection_id"), req.CollectionVersion, req.SelectionVersion, req.Page, h.resolver(c))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) RemoveMember(c *gin.Context) {
	var req struct {
		Version int64 `json:"version,string"`
	}
	if !h.body(c, &req, "version") {
		return
	}
	out, e := h.service.RemoveMember(c.Request.Context(), c.Param("id"), c.Param("member_id"), req.Version, h.resolver(c))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) Collections(c *gin.Context) {
	if !h.ready(c) || !noQuery(c) {
		return
	}
	out, e := h.service.PublicCollections(c.Request.Context())
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func pageQuery(c *gin.Context) (int, int, bool) {
	page, size := 1, 50
	for k, values := range c.Request.URL.Query() {
		if (k != "page" && k != "page_size") || len(values) != 1 {
			knowledgeError(c, ErrInvalid)
			return 0, 0, false
		}
		n, e := strconv.Atoi(values[0])
		if e != nil || strconv.Itoa(n) != values[0] {
			knowledgeError(c, ErrInvalid)
			return 0, 0, false
		}
		if k == "page" {
			page = n
		} else {
			size = n
		}
	}
	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	if e != nil || len(raw) > 0 {
		knowledgeError(c, ErrInvalid)
		return 0, 0, false
	}
	return page, size, true
}
func (h *Handler) Sources(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	page, size, ok := pageQuery(c)
	if !ok {
		return
	}
	out, e := h.service.Sources(c.Request.Context(), c.Param("id"), page, size)
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}

// Search has the same anonymous-current-origin boundary as Source. Credentials
// supplied by a client cannot widen the public corpus or select a Provider.
func (h *Handler) Search(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	if nilSourceAdapter(h.retrieval) {
		knowledgeError(c, ErrRetrievalProvider)
		return
	}
	// Bound the encoded query before decoding; a UTF-8 byte needs at most three
	// bytes in percent encoding. Do not use URL.Query: it discards parse errors.
	if len(c.Request.URL.RawQuery) > 3*MaxSearchQueryBytes+128 {
		knowledgeError(c, ErrInvalid)
		return
	}
	query, e := url.ParseQuery(c.Request.URL.RawQuery)
	if e != nil || len(query["query"]) != 1 {
		knowledgeError(c, ErrInvalid)
		return
	}
	req := PublicSearchRequest{CollectionID: c.Param("id"), Query: query.Get("query"), Page: 1, PageSize: 20}
	for key, values := range query {
		if len(values) != 1 || (key != "query" && key != "page" && key != "page_size") {
			knowledgeError(c, ErrInvalid)
			return
		}
		if key == "query" {
			continue
		}
		n, err := strconv.Atoi(values[0])
		if err != nil || strconv.Itoa(n) != values[0] {
			knowledgeError(c, ErrInvalid)
			return
		}
		if key == "page" {
			req.Page = n
		} else {
			req.PageSize = n
		}
	}
	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	if e != nil || len(raw) > 0 {
		knowledgeError(c, ErrInvalid)
		return
	}
	out, e := h.retrieval.SearchPublic(c.Request.Context(), req)
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}

func (h *Handler) Source(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	if len(c.Request.URL.RawQuery) > 256 {
		knowledgeError(c, ErrInvalid)
		return
	}
	query, e := url.ParseQuery(c.Request.URL.RawQuery)
	if e != nil {
		knowledgeError(c, ErrInvalid)
		return
	}
	for k, v := range query {
		if k != "revision" || len(v) != 1 || v[0] == "" {
			knowledgeError(c, ErrInvalid)
			return
		}
	}
	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	if e != nil || len(raw) > 0 {
		knowledgeError(c, ErrInvalid)
		return
	}
	out, e := h.service.Source(c.Request.Context(), c.Param("id"), c.Param("member_id"), query.Get("revision"))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}

func (h *Handler) IndexDiagnostics(c *gin.Context) {
	if !h.ready(c) || !noQuery(c) {
		return
	}
	out, e := h.service.IndexDiagnostics(c.Request.Context(), c.Param("id"), h.resolver(c))
	if e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *Handler) RetryIndex(c *gin.Context) {
	var req struct {
		CollectionVersion int64 `json:"collection_version,string"`
		Version           int64 `json:"version,string"`
	}
	if !h.body(c, &req, "collection_version", "version") {
		return
	}
	if e := h.service.RetryIndex(c.Request.Context(), c.Param("id"), req.CollectionVersion, req.Version, h.resolver(c)); e != nil {
		knowledgeError(c, e)
		return
	}
	response.Success(c, map[string]bool{"accepted": true})
}
