package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/gin-gonic/gin"
)

func machineSearchRouter(h *Handler) *gin.Engine {
	r := gin.New()
	r.POST("/collections/:id/search", h.IntegrationSearch)
	return r
}

func machineSearchRequest(t *testing.T, r http.Handler, path, body string, headers http.Header, status int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header = headers
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != status || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("search status/cache %d %s", w.Code, w.Body.String())
	}
	if status != 200 && strings.Contains(w.Body.String(), `"data"`) {
		t.Fatal("error returned buffered search data", w.Body.String())
	}
	return w
}

func TestIntegrationSearchClosedInputAndScopedCurrentResults(t *testing.T) {
	f, _, retrieval, collection := retrievalFixture(t, 3)
	h := NewHandler(f.s, noAdminSession{}, retrieval)
	calls := 0
	h.machine = integrationReadFunc(func(ctx context.Context, token string, target identityport.IntegrationAccessTarget, read func(context.Context) (any, error)) (any, error) {
		calls++
		if token != "test-key" || target.Channel != "integration_http" || target.Endpoint != "knowledge.search" || target.Action != "knowledge.source.read" || target.Resource.Kind != "public_collection" || target.Resource.ID != collection.ID {
			t.Fatal("machine target not bound to actual endpoint/collection", target)
		}
		if d, ok := ctx.Deadline(); !ok || time.Until(d) > 5*time.Second {
			t.Fatal("missing bounded machine request context")
		}
		return read(ctx)
	})
	r := machineSearchRouter(h)
	path := "/collections/" + collection.ID + "/search"
	valid := `{"query":"公开文本","page":1,"page_size":2}`
	auth := http.Header{"Authorization": {"Integration test-key"}}
	w := machineSearchRequest(t, r, path, valid, auth, 200)
	var out struct {
		Data PublicSearchPage `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.Total != 4 || len(out.Data.Items) != 2 || out.Data.PageSize != 2 {
		t.Fatal("machine search changed public corpus or paging", w.Body.String())
	}
	for _, hit := range out.Data.Items {
		if hit.Citation.CollectionID != collection.ID || len(hit.Citation.VisibilityRevision) != 64 {
			t.Fatal("citation escaped the exact collection")
		}
	}
	// Stale projections must not retain hidden rows in totals or page content.
	row, err := f.threads.GetByID(t.Context(), "91001")
	if err != nil {
		t.Fatal(err)
	}
	row.PublicationStatus = "private"
	if err = f.threads.Update(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	w = machineSearchRequest(t, r, path, valid, auth, 200)
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.Total != 3 {
		t.Fatal("private row affected current total", w.Body.String())
	}
	before := calls
	for _, body := range []string{"", `null`, `{}`, `{"query":"ok","page":1}`, `{"query":null,"page":1,"page_size":2}`, `{"query":"ok","page":1.0,"page_size":2}`, `{"query":"ok","page":0,"page_size":2}`, `{"query":"ok","page":100001,"page_size":2}`, `{"query":"ok","page":1,"page_size":51}`, `{"query":"ok","page":1,"page":2,"page_size":2}`, `{"query":"ok","qu\u0065ry":"override","page":1,"page_size":2}`, `{"query":"ok","page":1,"page_size":2,"collection_id":"foreign"}`, `{"query":"ok","page":1,"page_size":2,"provider":"remote"}`, `{"query":"\ud800","page":1,"page_size":2}`, `{"query":"","page":1,"page_size":2}`, `{"query":"` + strings.Repeat("a", 1025) + `","page":1,"page_size":2}`, valid + ` {}`, strings.Repeat(" ", 32769)} {
		machineSearchRequest(t, r, path, body, auth, 400)
	}
	for _, headers := range []http.Header{nil, {"Authorization": {"Bearer user-or-admin"}}, {"Authorization": {"Integration test-key", "Integration test-key"}}, {"Authorization": {"Integration test-key"}, "Cookie": {"session=x"}}, {"Authorization": {"Integration test-key"}, "Origin": {"https://example.test"}}} {
		machineSearchRequest(t, r, path, valid, headers, 401)
	}
	machineSearchRequest(t, r, path+"?query=override", valid, auth, 401)
	if calls != before {
		t.Fatal("invalid input reached the machine/resource callback")
	}
}

func TestIntegrationSearchDiscardsPostReadFailure(t *testing.T) {
	for _, mode := range []string{"revoked", "scope", "facts_changed", "audit", "hot_disable", "cancel", "stop", "wrong_type", "wrong_collection"} {
		t.Run(mode, func(t *testing.T) {
			f, _, retrieval, c := retrievalFixture(t, 1)
			h := NewHandler(f.s, noAdminSession{}, retrieval)
			requestCtx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := 503
			h.machine = integrationReadFunc(func(ctx context.Context, _ string, _ identityport.IntegrationAccessTarget, read func(context.Context) (any, error)) (any, error) {
				data, err := read(ctx)
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "revoked":
					want = 401
					return data, identityport.ErrIntegrationCredentialInvalid
				case "scope":
					want = 403
					return data, identityport.ErrIntegrationDenied
				case "facts_changed":
					want = 409
					return data, identityport.ErrIntegrationChanged
				case "audit":
					return data, errors.New("INTERNAL_SEARCH_SECRET")
				case "hot_disable":
					f.enabled = false
				case "cancel":
					cancel()
				case "stop":
					retrieval.stopped.Store(true)
				case "wrong_type":
					return "untyped private data", nil
				case "wrong_collection":
					p := data.(PublicSearchPage)
					p.Items[0].Citation.CollectionID = "foreign"
					return p, nil
				}
				return data, nil
			})
			r := machineSearchRouter(h)
			req := httptest.NewRequest("POST", "/collections/"+c.ID+"/search", strings.NewReader(`{"query":"公开文本","page":1,"page_size":2}`)).WithContext(requestCtx)
			req.Header.Set("Authorization", "Integration test-key")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != want || strings.Contains(w.Body.String(), `"data"`) || strings.Contains(w.Body.String(), "INTERNAL_SEARCH_SECRET") {
				t.Fatal("buffered search escaped final failure", w.Code, w.Body.String())
			}
		})
	}
}
