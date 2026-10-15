package richtext

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestAttachmentMetadataHTTPRealCurrentProofAndSafeAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, operation := range []string{"bind", "remove", "rename", "reorder"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			s, _, _, _, _, _ := articleCreateFixture(t)
			adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), MaxArticleAttachmentBytes)
			if err != nil {
				t.Fatal(err)
			}
			s.SetObjectPort(objects)
			draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
			if err != nil {
				t.Fatal(err)
			}
			items := []*ArticleAttachment{}
			pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
			for _, name := range []string{"a.pdf", "b.pdf"} {
				a, err := s.UploadAttachment(ctx, "1001", draft.ThreadID, name, "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
				if err != nil {
					t.Fatal(err)
				}
				items = append(items, a)
			}
			if operation == "bind" {
				if err := s.store.RemoveAttachment(ctx, draft.Article.ID, items[1].ID); err != nil {
					t.Fatal(err)
				}
			}
			users, sessions := identityrepo.NewMemoryUserRepository(), identityrepo.NewMemorySessionRepository()
			jwt := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "attachment-meta-user-test", Issuer: "attachment-meta-user", Audience: "user", KeyID: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour}, auth.JWTConfig{Secret: "attachment-meta-admin-test", Issuer: "attachment-meta-admin", Audience: "admin", KeyID: "admin", AccessTTL: time.Hour})
			identity, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "attachment-meta-ip-test"})
			if err != nil {
				t.Fatal(err)
			}
			tokens := map[string]string{}
			for _, id := range []string{"1001", "1002"} {
				u := &identitydomain.User{ID: id, Username: "attachment-meta-" + id, Email: "attachment-meta-" + id + "@example.test", Status: "active", AuthVersion: 1}
				if err := users.Create(ctx, u); err != nil {
					t.Fatal(err)
				}
				issued, err := identity.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Attachment metadata"})
				if err != nil {
					t.Fatal(err)
				}
				tokens[id] = issued.AccessToken
			}
			path := "/write/" + draft.ThreadID + "/" + items[0].ID
			body := ""
			switch operation {
			case "bind":
				body = fmt.Sprintf("{\"asset_id\":%q}", items[1].AssetID)
			case "rename":
				body = "{\"display_name\":\"renamed.pdf\"}"
			case "reorder":
				body = fmt.Sprintf("{\"attachment_ids\":[%q,%q]}", items[1].ID, items[0].ID)
			}
			serve := func(token string, proof bool, payload string) *httptest.ResponseRecorder {
				h := NewHandler(s)
				if proof {
					h.SetSessionProof(identity, identity)
				}
				handler := h.BindAttachment
				if operation == "remove" {
					handler = h.RemoveAttachment
				}
				if operation == "rename" {
					handler = h.RenameAttachment
				}
				if operation == "reorder" {
					handler = h.ReorderAttachments
				}
				router := gin.New()
				router.POST("/write/:id/:attachment_id", middleware.JWTAuth(jwt.User, identity), handler)
				req := httptest.NewRequest(http.MethodPost, path+"?owner_id=1002&action=admin", strings.NewReader(payload))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Actor-ID", "1002")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				return rec
			}
			if r := serve(tokens["1002"], true, body); r.Code != 403 {
				t.Fatal("other", r.Code, r.Body.String())
			}
			if r := serve(tokens["1001"], false, body); r.Code != 401 {
				t.Fatal("missing proof", r.Code, r.Body.String())
			}
			admin, err := jwt.Admin.GenerateAccessToken("1001", "Admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			if r := serve(admin, true, body); r.Code != 401 {
				t.Fatal("same-ID Admin", r.Code, r.Body.String())
			}
			bad := "{\"actor_id\":\"1002\"}"
			if operation != "remove" {
				bad = strings.TrimSuffix(body, "}") + ",\"actor_id\":\"1002\"}"
			}
			if r := serve(tokens["1001"], true, bad); r.Code != 400 {
				t.Fatal("strict request", r.Code, r.Body.String())
			}
			want := 200
			if operation == "bind" {
				want = 201
			}
			if operation == "remove" {
				want = 204
			}
			r := serve(tokens["1001"], true, body)
			if r.Code != want {
				t.Fatal("owner", r.Code, r.Body.String())
			}
			if want != 204 {
				var envelope struct {
					Data map[string]json.RawMessage `json:"data"`
				}
				if json.Unmarshal(r.Body.Bytes(), &envelope) != nil || len(envelope.Data) < 1 || len(envelope.Data) > 2 {
					t.Fatal("ack", r.Body.String())
				}
				for name := range envelope.Data {
					if name != "thread_id" && name != "attachment_id" {
						t.Fatal("private response field", name)
					}
				}
			}
			if _, err := sessions.RevokeByUser(ctx, "1001", "test revoke", time.Now()); err != nil {
				t.Fatal(err)
			}
			if r := serve(tokens["1001"], true, body); r.Code != 401 {
				t.Fatal("revoked", r.Code, r.Body.String())
			}
		})
	}
}
