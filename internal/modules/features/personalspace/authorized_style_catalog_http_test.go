package space

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type legacySpaceStyleCatalog struct{ StyleApplication }

func TestSpaceStyleCatalogHTTPActualSessionNoBodySafeProjectionAndRevocation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	e := newOwnContentsHTTPEnvironment(t)
	root := t.TempDir()
	t.Setenv("RESOURCE_DIR", root)
	writeStyleVariantSource(t, root, styleVariantFiles())
	s := NewService(e.spaces, e.users)
	admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "Admin", auth.AccessTokenContext{SessionID: "catalog-admin", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token, body, mode string) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if mode != "missing_proof" {
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		if mode == "legacy_target" {
			h = NewHandlerWithStyleApplication(s, &legacySpaceStyleCatalog{})
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		r := gin.New()
		g := r.Group("/catalog", middleware.JWTAuth(e.jwts.User, e.identity))
		g.GET("", h.ListSourceStylePacks)
		req := httptest.NewRequest("GET", "/catalog?owner_id="+e.other.ID+"&scope=admin", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Owner-ID", e.other.ID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	before := e.spaces.Snapshot()
	for _, row := range []struct {
		token, body, mode string
		want              int
	}{{e.tokens.AccessToken, "", "current", 200}, {e.tokens.AccessToken, "{}", "current", 400}, {admin, "", "current", 401}, {e.tokens.AccessToken, "", "missing_proof", 401}, {e.tokens.AccessToken, "", "legacy_target", 503}} {
		rec := serve(row.token, row.body, row.mode)
		if rec.Code != row.want || strings.Contains(rec.Body.String(), root) {
			t.Fatal(row.mode, row.want, rec.Code, rec.Body.String())
		}
		if row.want == 200 {
			var wire struct {
				Data struct{ Items []map[string]json.RawMessage }
			}
			if json.Unmarshal(rec.Body.Bytes(), &wire) != nil || len(wire.Data.Items) != 1 || wire.Data.Items[0]["path"] != nil || rec.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("unsafe current catalog", rec.Body.String(), rec.Header())
			}
		}
	}
	if err := e.identity.RevokeCurrent(ctx, e.owner.ID, e.tokens.Session.ID); err != nil {
		t.Fatal(err)
	}
	if rec := serve(e.tokens.AccessToken, "", "current"); rec.Code != 401 {
		t.Fatal("revoked current token accepted", rec.Code, rec.Body.String())
	}
	if !reflect.DeepEqual(before, e.spaces.Snapshot()) {
		t.Fatal("catalog HTTP created or changed a Space")
	}
}
