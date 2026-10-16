package richtext

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestRichTextGovernanceHTTPCurrentAdminProofSafeAckAndFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, op := range []string{"take_down", "direct_restore", "trash"} {
		for _, mode := range []string{"bound_opaque", "bound_numeric", "empty_body", "user_same_id", "missing_proof", "missing_command", "missing_store", "missing_authority", "permission", "password", "revoked", "suspended", "unknown_body", "unknown_chunked", "blank_reason", "final_article", "final_core", "final_permission", "disabled"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				actor := "adm.opaque.article"
				if mode == "bound_numeric" || mode == "user_same_id" {
					actor = "1001"
				}
				g := articleGovernanceFixture(t, actor, op)
				ctx := context.Background()
				original := g.s.store
				h := NewHandler(g.s)
				if mode != "missing_proof" {
					h.SetAdminSessionAuthority(g.current)
				}
				token, err := g.jwt.GenerateAccessToken(actor, "Admin", auth.AccessTokenContext{SessionID: g.claims.SessionID, AuthVersion: 1})
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "user_same_id":
					user := auth.NewJWTManager(auth.JWTConfig{Secret: "article-gov-http-user-key", Issuer: "article-gov-user", Audience: "user", AccessTTL: time.Hour})
					token, err = user.GenerateAccessToken(actor, "Owner", auth.AccessTokenContext{SessionID: "user-session", AuthVersion: 1})
					if err != nil {
						t.Fatal(err)
					}
				case "missing_command":
					g.s.community = articleRawGateway{g.s.community}
				case "missing_store":
					g.s.store = struct{ Store }{original}
				case "missing_authority":
					if op == "take_down" {
						g.core.SetGovernanceAuthority(nil)
					} else {
						g.core.SetAdminExecutionAuthority(nil)
					}
				case "permission":
					if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "test"); err != nil {
						t.Fatal(err)
					}
				case "password":
					row, _ := g.sessions.GetSessionByID(ctx, g.claims.SessionID)
					if _, ok, err := g.sessions.UpgradeSessionStrengthCAS(ctx, row.ID, row.Version, "password", time.Now(), time.Now()); err != nil || !ok {
						t.Fatal(ok, err)
					}
				case "revoked":
					row, _ := g.sessions.GetSessionByID(ctx, g.claims.SessionID)
					if _, ok, err := g.sessions.RevokeSessionCAS(ctx, row.ID, row.Version, "test", time.Now()); err != nil || !ok {
						t.Fatal(ok, err)
					}
				case "suspended":
					row, _ := g.admins.GetAccountByID(ctx, actor)
					if _, ok, err := g.admins.SetAccountStatusCAS(ctx, actor, row.Version, "suspended", "test", actor, time.Now()); err != nil || !ok {
						t.Fatal(ok, err)
					}
				case "disabled":
					g.s.SetEnabledChecker(func() bool { return false })
				}
				if strings.HasPrefix(mode, "final_") {
					g.s.store = &articleGovernanceHookStore{Store: original, afterUpdate: func(tx context.Context, a *Article) error {
						switch mode {
						case "final_article":
							row, _ := original.GetArticleByThreadID(tx, g.id)
							row.Summary = "unexpected"
							return original.UpdateArticle(tx, row)
						case "final_core":
							row, _ := g.threads.GetByID(tx, g.id)
							row.Content = "unexpected"
							return g.threads.Update(tx, row)
						case "final_permission":
							return g.admins.ReplaceAdminRolePermissionFacts(tx, "security_admin", nil, "test")
						}
						return nil
					}}
				}
				body := `{"reason":"actual HTTP governance"}`
				if mode == "unknown_body" || mode == "unknown_chunked" {
					body = `{"reason":"valid","actor_id":"1001","scope":{"id":"other"}}`
				}
				if mode == "empty_body" {
					body = ""
				}
				if mode == "blank_reason" {
					body = `{"reason":" "}`
				}
				router := gin.New()
				handler := h.AdminOffline
				if op == "direct_restore" {
					handler = h.AdminRestore
				}
				if op == "trash" {
					handler = h.AdminDelete
				}
				method, suffix := http.MethodPost, "/admin/offline"
				if op == "direct_restore" {
					suffix = "/admin/restore"
				}
				if op == "trash" {
					method, suffix = http.MethodDelete, "/admin"
				}
				router.Handle(method, "/api/v1/richtext/articles/:id"+suffix, middleware.RequireAdminDomainAccess(g.jwt, portadapt.NewAdminDomainSessionVerifier(g.current), nil), handler)
				request := httptest.NewRequest(method, "/api/v1/richtext/articles/"+g.id+suffix+"?actor_id=1001&scope_id=other", strings.NewReader(body))
				if mode == "unknown_chunked" {
					request.ContentLength = -1
					request.TransferEncoding = []string{"chunked"}
				}
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("X-Actor-ID", "1001")
				beforeThread, beforeArticle, beforeGov, beforeEvents := g.threads.Snapshot(), original.(transaction.Snapshotter).Snapshot(), g.gov.Snapshot(), g.events.Snapshot()
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, request)
				want := 200
				switch mode {
				case "bound_opaque", "bound_numeric", "empty_body":
					if op == "trash" {
						want = 204
					}
				case "user_same_id", "missing_proof", "revoked", "suspended":
					want = 401
				case "missing_command", "missing_store", "missing_authority":
					want = 503
				case "permission", "password", "disabled":
					want = 403
				case "unknown_body", "unknown_chunked", "blank_reason":
					want = 400
				default:
					want = 409
				}
				if rec.Code != want || strings.Contains(rec.Body.String(), "Author draft") || strings.Contains(rec.Body.String(), "<p>body</p>") {
					t.Fatal(mode, rec.Code, rec.Body.String())
				}
				if want == 200 {
					var payload struct {
						Data map[string]any `json:"data"`
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || len(payload.Data) != 2 || payload.Data["thread_id"] != g.id {
						t.Fatal(rec.Body.String(), err)
					}
				}
				if want >= 400 && (!reflect.DeepEqual(beforeThread, g.threads.Snapshot()) || !reflect.DeepEqual(beforeArticle, original.(transaction.Snapshotter).Snapshot()) || !reflect.DeepEqual(beforeGov, g.gov.Snapshot()) || !reflect.DeepEqual(beforeEvents, g.events.Snapshot())) {
					t.Fatal("denied HTTP left partial data")
				}
			})
		}
	}
}
