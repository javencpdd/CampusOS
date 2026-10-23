package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func retrievalHTTP(h *Handler) *gin.Engine {
	r := gin.New()
	r.GET("/collections/:id/search", h.Search)
	r.GET("/collections/:id/sources/:member_id", h.Source)
	return r
}

func httpSearchResult(t *testing.T, r http.Handler, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, strings.NewReader(body))
	// Public sources cannot be widened by a claimed principal or credential.
	req.Header.Set("Authorization", "Bearer untrusted-admin-claim")
	r.ServeHTTP(w, req)
	if w.Code != status || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("status/cache: %d %s: %s", w.Code, w.Header().Get("Cache-Control"), w.Body.String())
	}
	return w
}

func TestKnowledgeSearchHTTPPagesCurrentCitationAndRevocation(t *testing.T) {
	f, _, s, collection := retrievalFixture(t, 22)
	r := retrievalHTTP(NewHandler(f.s, noAdminSession{}, s))
	base := "/collections/" + collection.ID
	path := base + "/search?query=" + url.QueryEscape("公开文本")
	var first struct {
		Data PublicSearchPage `json:"data"`
	}
	if e := json.Unmarshal(httpSearchResult(t, r, path, "", 200).Body.Bytes(), &first); e != nil {
		t.Fatal(e)
	}
	if first.Data.Total != 23 || first.Data.Page != 1 || first.Data.PageSize != 20 || len(first.Data.Items) != 20 {
		t.Fatal(first.Data)
	}
	var second struct {
		Data PublicSearchPage `json:"data"`
	}
	if e := json.Unmarshal(httpSearchResult(t, r, path+"&page=2", "", 200).Body.Bytes(), &second); e != nil {
		t.Fatal(e)
	}
	if second.Data.Total != 23 || len(second.Data.Items) != 3 {
		t.Fatal(second.Data)
	}
	seen := map[string]bool{}
	for _, hit := range append(first.Data.Items, second.Data.Items...) {
		key := hit.Citation.Source.Kind + ":" + hit.Citation.Source.ID
		if seen[key] {
			t.Fatal("duplicated source", key)
		}
		seen[key] = true
		var source struct {
			Data VisibleSource `json:"data"`
		}
		reopen := base + "/sources/" + hit.Citation.MemberID + "?revision=" + hit.Citation.VisibilityRevision
		if e := json.Unmarshal(httpSearchResult(t, r, reopen, "", 200).Body.Bytes(), &source); e != nil {
			t.Fatal(e)
		}
		if source.Data.Source.Reference != hit.Citation.Source || source.Data.Revision != hit.Citation.VisibilityRevision || !strings.Contains(source.Data.Source.Content, hit.Snippet) {
			t.Fatal("citation did not reopen current original")
		}
	}
	hit := first.Data.Items[0]
	reopen := base + "/sources/" + hit.Citation.MemberID + "?revision=" + hit.Citation.VisibilityRevision
	row, _ := f.threads.GetByID(t.Context(), hit.Citation.Source.ID)
	row.Content = "NEW_REVISION_HTTP_ONLY"
	if e := f.threads.Update(t.Context(), row); e != nil {
		t.Fatal(e)
	}
	httpSearchResult(t, r, reopen, "", 409)
	var stale struct {
		Data PublicSearchPage `json:"data"`
	}
	if e := json.Unmarshal(httpSearchResult(t, r, path, "", 200).Body.Bytes(), &stale); e != nil {
		t.Fatal(e)
	}
	if stale.Data.Total != 22 || len(indexDocs(t, f, collection.ID)) != 23 {
		t.Fatal("stale projection leaked or query mutated index")
	}
	row.PublicationStatus = "private"
	if e := f.threads.Update(t.Context(), row); e != nil {
		t.Fatal(e)
	}
	httpSearchResult(t, r, reopen, "", 404)
	f.enabled = false
	w := httpSearchResult(t, r, path, "", 503)
	if !strings.Contains(w.Body.String(), `"code":"knowledge.disabled"`) {
		t.Fatal(w.Body.String())
	}
}

func TestKnowledgeSearchHTTPRejectsAmbiguousInput(t *testing.T) {
	f, _, s, collection := retrievalFixture(t, 0)
	r := retrievalHTTP(NewHandler(f.s, noAdminSession{}, s))
	base := "/collections/" + collection.ID + "/search?"
	for _, query := range []string{
		"", "query=", "query=%", "query=ok&ignored=%ZZ", "query=ok;page=2",
		"query=ok&query=evil", "query=ok&qu%65ry=evil", "query=ok&principal=admin",
		"query=ok&provider=remote", "query=ok&collection_id=other", "query=ok&acl=public",
		"query=ok&page=1&page=2", "query=ok&page_size=20&page_size=1",
		"query=ok&page=01", "query=ok&page=%2B1", "query=ok&page=-1", "query=ok&page=0",
		"query=ok&page=100001", "query=ok&page=999999999999999999999999", "query=ok&page_size=51",
		"query=ok&page_size=0", "query=ok&page_size=", "query=%FF", "query=ok%00",
		"query=" + strings.Repeat("a", MaxSearchQueryBytes+1), "query=" + strings.Repeat("%61", MaxSearchQueryBytes+100),
	} {
		t.Run(query[:min(50, len(query))], func(t *testing.T) { httpSearchResult(t, r, base+query, "", 400) })
	}
	httpSearchResult(t, r, base+"query=ok", `{"principal":"admin"}`, 400)
	httpSearchResult(t, r, "/collections/NOT_CANONICAL/search?query=ok", "", 400)
	// The citation route must never silently drop a malformed exact proof and
	// fall back to the ordinary, unpinned source read.
	hit, e := s.SearchPublic(t.Context(), request(collection, "公开文本"))
	if e != nil {
		t.Fatal(e)
	}
	member := hit.Items[0].Citation.MemberID
	for _, query := range []string{"revision=", "revision=%ZZ", "revision=" + hit.Items[0].Citation.VisibilityRevision + ";ignored=x", "revision=" + strings.Repeat("a", 64) + "&revision=" + strings.Repeat("b", 64), "principal=admin", "revision=" + strings.Repeat("A", 64)} {
		httpSearchResult(t, r, "/collections/"+collection.ID+"/sources/"+member+"?"+query, "", 400)
	}
}

type httpFailRetrieval struct{ err error }

func (p httpFailRetrieval) SearchPublic(context.Context, PublicSearchRequest) (PublicSearchPage, error) {
	return PublicSearchPage{}, p.err
}

func TestKnowledgeSearchHTTPFailureContractAndCancellation(t *testing.T) {
	f, index, s, c := retrievalFixture(t, 0)
	for _, err := range []error{ErrIndexNotReady, ErrIndexConsumerStopped, ErrRetrievalProvider, context.Canceled, context.DeadlineExceeded, errors.New("PRIVATE_PROVIDER_SECRET http://internal/path")} {
		r := retrievalHTTP(NewHandler(f.s, noAdminSession{}, httpFailRetrieval{err}))
		w := httpSearchResult(t, r, "/collections/"+c.ID+"/search?query=ok", "", 503)
		if !strings.Contains(w.Body.String(), `"code":"knowledge.unavailable"`) || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), `"data"`) {
			t.Fatal(w.Body.String())
		}
	}
	r := retrievalHTTP(NewHandler(f.s, noAdminSession{}, s))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequest("GET", "/collections/"+c.ID+"/search?query=ok", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 503 || strings.Contains(w.Body.String(), `"data"`) {
		t.Fatal(w.Body.String())
	}
	indexWake(t, f, c.ID)
	if e := index.runPage(t.Context()); e != nil {
		t.Fatal(e)
	}
	httpSearchResult(t, r, "/collections/"+c.ID+"/search?query=ok", "", 503)
	indexDrain(t, f, index, c.ID)
	s.stopped.Store(true)
	httpSearchResult(t, r, "/collections/"+c.ID+"/search?query=ok", "", 503)
}
