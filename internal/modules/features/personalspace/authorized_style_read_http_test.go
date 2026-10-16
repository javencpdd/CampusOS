package space

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func styleReadHTTPMethod(op string) string {
	if op == "html_example" || op == "pack_example" || op == "pack_zip" {
		return "GET"
	}
	return "POST"
}

func registerStyleReadHTTP(g *gin.RouterGroup, h *Handler) {
	g.POST("/validate", h.ValidateStylePackage)
	g.POST("/preview", h.PreviewStylePackage)
	g.POST("/export", h.ExportStylePackage)
	g.POST("/html_validate", h.ValidateCustomHTML)
	g.GET("/html_example", h.CustomHTMLExample)
	g.POST("/zip_validate", h.ValidateStylePackZip)
	g.GET("/pack_example", h.StylePackExample)
	g.GET("/pack_zip", h.StylePackExampleZip)
}

func styleReadHTTPInput(t *testing.T, op, mode string) ([]byte, string) {
	t.Helper()
	contentType := "application/json"
	if op == "zip_validate" {
		var buffer bytes.Buffer
		w := multipart.NewWriter(&buffer)
		if mode == "unknown" {
			if err := w.WriteField("owner_id", "1002"); err != nil {
				t.Fatal(err)
			}
		}
		f, err := w.CreateFormFile("file", "style.zip")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(zipStylePack(t, styleVariantFiles())); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes(), w.FormDataContentType()
	}
	var body []byte
	switch op {
	case "validate", "preview":
		body, _ = json.Marshal(styleReadPackage())
	case "export":
		body = []byte(`{}`)
	case "html_validate":
		body = []byte(`{"html":"<section>Current</section>"}`)
	}
	if mode == "unknown" {
		if styleReadHTTPMethod(op) == "GET" {
			body = []byte(`{}`)
		} else if string(body) == `{}` {
			body = []byte(`{"owner_id":"1002"}`)
		} else {
			body = append([]byte(`{"owner_id":"1002",`), body[1:]...)
		}
	}
	if mode == "oversize" {
		body = []byte(strings.Repeat(" ", styleReadJSONMaxBytes+1) + string(body))
	}
	return body, contentType
}

func TestSpaceStyleReadHTTPActualSessionStrictDTOCurrentProofAndBufferedZIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	e := newOwnContentsHTTPEnvironment(t)
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	s := NewService(e.spaces, e.users)
	admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "Admin", auth.AccessTokenContext{SessionID: "read-admin", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	serve := func(op, token, mode string) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if mode != "missing_proof" {
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		if mode == "legacy_target" {
			h = NewHandlerWithStyleApplication(s, &legacySpaceStyleCatalog{})
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		if mode == "typed_nil" {
			var empty *Service
			h = NewHandlerWithStyleApplication(s, empty)
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		r := gin.New()
		r.MaxMultipartMemory = 1
		g := r.Group("/read", middleware.JWTAuth(e.jwts.User, e.identity))
		registerStyleReadHTTP(g, h)
		body, contentType := styleReadHTTPInput(t, op, mode)
		req := httptest.NewRequest(styleReadHTTPMethod(op), "/read/"+op+"?owner_id="+e.other.ID+"&scope=admin", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		entries, err := os.ReadDir(temp)
		if err != nil || len(entries) != 0 {
			t.Fatal("temporary multipart data retained", entries, err)
		}
		return rec
	}
	before := e.spaces.Snapshot()
	for _, op := range styleReadOperations {
		for _, row := range []struct {
			mode string
			want int
		}{{"current", 200}, {"missing_proof", 401}, {"legacy_target", 503}, {"typed_nil", 503}, {"unknown", 400}} {
			rec := serve(op, e.tokens.AccessToken, row.mode)
			if rec.Code != row.want {
				t.Fatal(op, row.mode, row.want, rec.Code, rec.Body.String())
			}
			if row.want == 200 {
				if rec.Header().Get("Cache-Control") != "private, no-store" {
					t.Fatal(op, rec.Header())
				}
				if op == "pack_zip" {
					if rec.Header().Get("Content-Type") != "application/zip" || !strings.Contains(rec.Header().Get("Content-Disposition"), ".zip") || len(rec.Body.Bytes()) == 0 {
						t.Fatal("not buffered ZIP", rec.Header())
					}
				} else {
					var payload map[string]json.RawMessage
					if json.Unmarshal(rec.Body.Bytes(), &payload) != nil || payload["data"] == nil {
						t.Fatal(op, rec.Body.String())
					}
				}
			}
		}
		if rec := serve(op, admin, "current"); rec.Code != 401 {
			t.Fatal("Admin token crossed domain", op, rec.Code)
		}
		if styleReadHTTPMethod(op) == "POST" && op != "zip_validate" {
			if rec := serve(op, e.tokens.AccessToken, "oversize"); rec.Code != 413 {
				t.Fatal("unbounded style JSON", op, rec.Code, rec.Body.String())
			}
		}
	}
	if err := e.identity.RevokeCurrent(ctx, e.owner.ID, e.tokens.Session.ID); err != nil {
		t.Fatal(err)
	}
	for _, op := range styleReadOperations {
		if rec := serve(op, e.tokens.AccessToken, "current"); rec.Code != 401 {
			t.Fatal("revoked session accepted", op, rec.Code)
		}
	}
	if !reflect.DeepEqual(before, e.spaces.Snapshot()) {
		t.Fatal("style read HTTP wrote Space")
	}
}
