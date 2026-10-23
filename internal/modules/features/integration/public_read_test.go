package integration

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communitysvc "github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/gin-gonic/gin"
)

// Build the real Identity gateway and the real Community public service.
// Only Admin authority is an in-process setup seam; HTTP cannot supply it.
type testIntegrationAuthority struct{}

func (testIntegrationAuthority) LoadAdminExecutionAuthority(ctx context.Context, q port.AdminExecutionQuery, r port.SessionPrincipalResolver, lock bool) (port.AdminExecutionAuthority, error) {
	p, err := r(ctx)
	return port.AdminExecutionAuthority{Principal: p, Query: q, Allowed: true, ExpiresAtMS: 9007199254740991, Binding: "setup-only"}, err
}
func TestIntegrationHTTPRealPublicSourceIntersection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"http_public", "mcp_public", "private", "pending", "trashed", "wrong_resource", "wrong_channel", "unregistered_tool", "bearer", "url_token", "cookie", "origin", "duplicate_auth", "unknown_json", "duplicate_json"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			repo := repository.NewMemoryIntegrationRepository()
			svc := service.NewIntegrationService(repo, testIntegrationAuthority{}, reliability.NewService(transaction.NewMemory(repo), reliability.NewMemoryStore()))
			proof := func(context.Context) (port.PrincipalContext, error) {
				return port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "admin", ID: "admin_setup"}, Audience: "admin", AuthenticationStrength: "mfa", CredentialID: "session_setup"}, nil
			}
			p, err := svc.CreatePrincipal(ctx, "test reader", proof)
			if err != nil {
				t.Fatal(err)
			}
			rules := []port.IntegrationAccessRule{{ID: "http", Channel: "integration_http", Endpoint: "public_thread.read", Action: "community.thread.read", ResourceKind: "community.thread", ResourceIDs: []string{"123"}, Status: "active", NotBeforeMS: 1, ExpiresAtMS: 9007199254740991}}
			if mode == "mcp_public" {
				rules[0].Channel = "integration_mcp"
				rules[0].Endpoint = "public_thread_read"
			}
			// The service bounds expiry to ninety days.
			now := time.Now().UnixMilli()
			rules[0].ExpiresAtMS = now + 3600000
			issued, err := svc.IssueCredential(ctx, p.ID, service.IntegrationIssueRequest{PrincipalVersion: p.Version, NotBeforeMS: 1, ExpiresAtMS: now + 3600000, Rules: rules}, proof)
			if err != nil {
				t.Fatal(err)
			}
			threads := communityrepo.NewMemoryThreadRepository()
			thread := &domain.Thread{ID: "123", AuthorID: "456", CategoryID: "789", Title: "public-title", Content: "sensitive-if-private", ContentFormat: "markdown", PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "active"}
			switch mode {
			case "private":
				thread.PublicationStatus = "private"
			case "pending":
				thread.ModerationStatus = "pending"
			case "trashed":
				thread.DeletionStatus = "trashed"
			}
			if err := threads.Create(ctx, thread); err != nil {
				t.Fatal(err)
			}
			h := &Handler{machine: svc, publicContent: community.NewContentQuery(communitysvc.NewThreadService(threads, nil))}
			router := gin.New()
			router.GET("/integration/threads/:id", h.IntegrationThread)
			router.POST("/integration/mcp/tools/:name/call", h.IntegrationMCP)
			method, path, body := "GET", "/integration/threads/123", ""
			if mode == "mcp_public" || mode == "wrong_channel" || mode == "unregistered_tool" || mode == "unknown_json" || mode == "duplicate_json" {
				method = "POST"
				path = "/integration/mcp/tools/public_thread_read/call"
				body = `{"id":"123"}`
			}
			if mode == "wrong_resource" {
				path = "/integration/threads/124"
			}
			if mode == "unregistered_tool" {
				path = "/integration/mcp/tools/admin_accounts_list/call"
			}
			if mode == "url_token" {
				path += "?token=" + issued.Token
			}
			if mode == "unknown_json" {
				body = `{"id":"123","principal":{"kind":"admin"}}`
			}
			if mode == "duplicate_json" {
				body = `{"id":"123","id":"123"}`
			}
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Authorization", "Integration "+issued.Token)
			switch mode {
			case "bearer":
				req.Header.Set("Authorization", "Bearer "+issued.Token)
			case "cookie":
				req.Header.Set("Cookie", "campusos_admin_refresh=x")
			case "origin":
				req.Header.Set("Origin", "http://localhost:3001")
			case "duplicate_auth":
				req.Header.Add("Authorization", "Integration "+issued.Token)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			want := 200
			switch mode {
			case "private", "pending", "trashed":
				want = 404
			case "wrong_resource", "wrong_channel", "unregistered_tool":
				want = 403
			case "bearer", "url_token", "cookie", "origin", "duplicate_auth":
				want = 401
			case "unknown_json", "duplicate_json":
				want = 400
			}
			if rec.Code != want {
				t.Fatalf("status %d want %d: %s", rec.Code, want, rec.Body.String())
			}
			if want != 200 && strings.Contains(rec.Body.String(), thread.Content) {
				t.Fatal("denial returned private body")
			}
			for _, secret := range []string{issued.Token, "digest", "moderation_reason", "created_by_admin_id", "facts_version"} {
				if strings.Contains(rec.Body.String(), secret) {
					t.Fatal("private field exposed", secret)
				}
			}
		})
	}
}
