package knowledge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/gin-gonic/gin"
)

type integrationReadFunc func(context.Context, string, identityport.IntegrationAccessTarget, func(context.Context) (any, error)) (any, error)

func (f integrationReadFunc) ReadForIntegration(ctx context.Context, token string, target identityport.IntegrationAccessTarget, read func(context.Context) (any, error)) (any, error) {
	return f(ctx, token, target, read)
}

func denyIntegrationRead(context.Context, string, identityport.IntegrationAccessTarget, func(context.Context) (any, error)) (any, error) {
	return nil, identityport.ErrIntegrationDenied
}

func TestIntegrationSourceClosedIngressAndBoundTarget(t *testing.T) {
	f, _, retrieval, collection := retrievalFixture(t, 1)
	page, err := f.s.Sources(t.Context(), collection.ID, 1, 50)
	if err != nil || len(page.Items) == 0 {
		t.Fatal(err)
	}
	source := page.Items[0]
	calls := 0
	h := NewHandler(f.s, noAdminSession{}, retrieval)
	h.machine = integrationReadFunc(func(ctx context.Context, token string, target identityport.IntegrationAccessTarget, read func(context.Context) (any, error)) (any, error) {
		calls++
		if token != "opaque-test" || target.Channel != "integration_http" || target.Endpoint != "knowledge.source.read" || target.Action != "knowledge.source.read" || target.Resource.Kind != "public_collection" || target.Resource.ID != collection.ID {
			t.Fatal("caller controlled target or credential domain", target)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("machine read has no request budget")
		}
		return read(ctx)
	})
	r := gin.New()
	r.GET("/collections/:id/sources/:member_id/revisions/:revision", h.IntegrationSource)
	path := "/collections/" + collection.ID + "/sources/" + source.MemberID + "/revisions/" + source.Revision
	request := func(path, body string, headers http.Header, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, strings.NewReader(body))
		req.Header = headers
		r.ServeHTTP(w, req)
		if w.Code != want || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("status/cache %d %s", w.Code, w.Body.String())
		}
		if want != 200 && strings.Contains(w.Body.String(), source.Source.Content) {
			t.Fatal("error returned source bytes")
		}
		return w
	}
	auth := http.Header{"Authorization": {"Integration opaque-test"}}
	w := request(path, "", auth, 200)
	if calls != 1 || !strings.Contains(w.Body.String(), source.MemberID) {
		t.Fatal("current citation not opened")
	}
	for _, headers := range []http.Header{nil, {"Authorization": {"Bearer user-or-admin"}}, {"Authorization": {"Integration opaque-test", "Integration opaque-test"}}, {"Authorization": {"Integration opaque-test"}, "Cookie": {"session=claim"}}, {"Authorization": {"Integration opaque-test"}, "Origin": {"https://example.test"}}} {
		request(path, "", headers, 401)
	}
	request(path+"?revision="+source.Revision, "", auth, 401)
	request(path, "{}", auth, 400)
	request(strings.Replace(path, source.Revision, strings.Repeat("A", 64), 1), "", auth, 400)
	request(strings.Replace(path, collection.ID, "private_document", 1), "", auth, 400)
	if calls != 1 {
		t.Fatal("invalid ingress reached Identity or original source", calls)
	}
}

func TestIntegrationSourceDiscardsBufferedFailureAndHotDisable(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"revoked", identityport.ErrIntegrationCredentialInvalid, 401},
		{"scope", identityport.ErrIntegrationDenied, 403},
		{"changed", identityport.ErrIntegrationChanged, 409},
		{"storage", identityport.ErrIntegrationUnavailable, 503},
		{"audit", errors.New("INTERNAL_DSN_SECRET_AUDIT"), 503},
		{"hot_disable", nil, 503},
		{"wrong_projection", nil, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _, retrieval, collection := retrievalFixture(t, 1)
			page, err := f.s.Sources(t.Context(), collection.ID, 1, 50)
			if err != nil || len(page.Items) == 0 {
				t.Fatal(err)
			}
			source := page.Items[0]
			h := NewHandler(f.s, noAdminSession{}, retrieval)
			h.machine = integrationReadFunc(func(ctx context.Context, _ string, _ identityport.IntegrationAccessTarget, read func(context.Context) (any, error)) (any, error) {
				out, err := read(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if tc.name == "hot_disable" {
					f.enabled = false
				}
				if tc.name == "wrong_projection" {
					out = "untyped data"
				}
				return out, tc.err
			})
			r := gin.New()
			r.GET("/collections/:id/sources/:member_id/revisions/:revision", h.IntegrationSource)
			req := httptest.NewRequest("GET", "/collections/"+collection.ID+"/sources/"+source.MemberID+"/revisions/"+source.Revision, nil)
			req.Header.Set("Authorization", "Integration opaque-test")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want || strings.Contains(w.Body.String(), source.Source.Content) || strings.Contains(w.Body.String(), "INTERNAL_DSN_SECRET_AUDIT") || strings.Contains(w.Body.String(), `"data"`) {
				t.Fatal("buffered data/internal error escaped", w.Code, w.Body.String())
			}
		})
	}
}
