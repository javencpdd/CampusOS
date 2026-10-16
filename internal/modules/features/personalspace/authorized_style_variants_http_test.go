package space

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestSpaceStyleVariantsHTTPActualCurrentSessionStrictInputsAndTempCleanup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	e := newOwnContentsHTTPEnvironment(t)
	root := t.TempDir()
	t.Setenv("RESOURCE_DIR", root)
	writeStyleVariantSource(t, root, styleVariantFiles())
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	s := NewService(e.spaces, e.users)
	s.SetReliability(reliability.NewService(transaction.NewMemory(e.spaces), reliability.NewMemoryStore()))
	admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "Admin", auth.AccessTokenContext{SessionID: "variants-admin", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	data := zipStylePack(t, styleVariantFiles())
	serve := func(token, variant, body, extra string, count int, bound bool) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if bound {
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		r := gin.New()
		r.MaxMultipartMemory = 1
		g := r.Group("/style", middleware.JWTAuth(e.jwts.User, e.identity))
		g.POST("/html", h.ApplyCustomHTML)
		g.POST("/zip", h.ApplyStylePackZip)
		g.POST("/source", h.ApplySourceStylePack)
		var buffer bytes.Buffer
		contentType := "application/json"
		if variant == "zip" {
			writer := multipart.NewWriter(&buffer)
			if extra != "" {
				if err := writer.WriteField(extra, e.other.ID); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < count; i++ {
				part, err := writer.CreateFormFile("file", "style.zip")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := part.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			contentType = writer.FormDataContentType()
		} else {
			buffer.WriteString(body)
		}
		req := httptest.NewRequest("POST", "/style/"+variant+"?owner_id="+e.other.ID+"&scope=admin", &buffer)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		entries, err := os.ReadDir(temp)
		if err != nil || len(entries) != 0 {
			t.Fatal("multipart temporary file was not closed before cleanup", len(entries), err)
		}
		return rec
	}
	check := func(token, variant, body, extra string, count int, bound bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		rec := serve(token, variant, body, extra, count, bound)
		if rec.Code != want {
			t.Fatal(want, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), root) {
			t.Fatal("host path exposed", rec.Body.String())
		}
		return rec
	}
	html := `{"html":"<section>Owned HTML</section>"}`
	source := `{"name":"variant-space"}`
	for _, variant := range []string{"html", "zip", "source"} {
		body := html
		if variant == "source" {
			body = source
		}
		check(e.tokens.AccessToken, variant, body, "", 1, false, 401)
		check(admin, variant, body, "", 1, true, 401)
	}
	for _, body := range []string{`{"html":"<p>Owned</p>","owner_id":"1002"}`, `{"html":"<script>bad()</script>"}`} {
		check(e.tokens.AccessToken, "html", body, "", 0, true, 400)
	}
	check(e.tokens.AccessToken, "source", `{"name":"variant-space","actor":"admin"}`, "", 0, true, 400)
	check(e.tokens.AccessToken, "source", `{"name":"../secret"}`, "", 0, true, 400)
	check(e.tokens.AccessToken, "zip", "", "owner_id", 1, true, 400)
	check(e.tokens.AccessToken, "zip", "", "", 2, true, 400)
	for _, variant := range []string{"html", "zip", "source"} {
		body := html
		if variant == "source" {
			body = source
		}
		rec := check(e.tokens.AccessToken, variant, body, "", 1, true, 200)
		var ack struct{ Data map[string]json.RawMessage }
		if json.Unmarshal(rec.Body.Bytes(), &ack) != nil || len(ack.Data) != 2 || ack.Data["space_id"] == nil || ack.Data["owner_id"] == nil {
			t.Fatal("unsafe variant acknowledgement", rec.Body.String())
		}
	}
	stored, err := e.spaces.GetByUserID(ctx, e.owner.ID)
	if err != nil || stored.StyleName != "variant-space" {
		t.Fatal(stored, err)
	}
	if err := e.identity.RevokeCurrent(ctx, e.owner.ID, e.tokens.Session.ID); err != nil {
		t.Fatal(err)
	}
	check(e.tokens.AccessToken, "html", html, "", 0, true, 401)
}
