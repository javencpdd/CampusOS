package richtext

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestRichTextAuthorizedHTTPRealSessionAndSafeBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	s := newAttachmentTestService(t)
	adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	objectsPort, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), MaxArticleAttachmentBytes)
	if err != nil {
		t.Fatal(err)
	}
	s.SetObjectPort(objectsPort)
	draft := createAttachmentDraft(t, s)
	pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	attachment, err := s.UploadAttachment(ctx, "1001", draft.ThreadID, "private.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	s.SetPDFViewerAuthorizer(func(context.Context, PDFViewerAuthorizationInput) error { return nil })
	users := identityrepo.NewMemoryUserRepository()
	sessions := identityrepo.NewMemorySessionRepository()
	jwt := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "rich-read-user-test-secret", Issuer: "rich-read-user", Audience: "user", KeyID: "user-key", AccessTTL: time.Hour, RefreshTTL: time.Hour}, auth.JWTConfig{Secret: "rich-read-admin-test-secret", Issuer: "rich-read-admin", Audience: "admin", KeyID: "admin-key", AccessTTL: time.Hour})
	identity, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "rich-read-test-ip-secret"})
	if err != nil {
		t.Fatal(err)
	}
	var ownerToken, otherToken string
	var ownerSession string
	for _, id := range []string{"1001", "1002"} {
		user := &identitydomain.User{ID: id, Username: "rich-user-" + id, Email: "rich-" + id + "@example.test", Status: identitydomain.UserStatusActive, AuthVersion: 1}
		if err := users.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
		tokens, err := identity.Issue(ctx, user, identityservice.SessionMetadata{DeviceName: "RichText Test"})
		if err != nil {
			t.Fatal(err)
		}
		if id == "1001" {
			ownerToken, ownerSession = tokens.AccessToken, tokens.Session.ID
		} else {
			otherToken = tokens.AccessToken
		}
	}
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, identity, identity, &auth.JWTClaims{UserID: "1001", SessionID: ownerSession, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	invocation, err := s.CreateInvocationForPrincipal(ctx, PluginUIInvocationRequest{PluginKey: PDFViewerPluginKey, SurfaceID: PDFViewerSurfaceID, ResourceType: InvocationContextPersonalAsset, ResourceID: attachment.AssetID, Presentation: "modal"}, proof)
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token, path string, bound bool) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if bound {
			h.SetSessionProof(identity, identity)
		}
		r := gin.New()
		r.GET("/public/:id", h.GetPublished)
		g := r.Group("", middleware.JWTAuth(jwt.User, identity))
		g.GET("/articles/:id/me", h.GetMine)
		g.GET("/articles/:id/attachments", h.ListAttachments)
		g.GET("/articles/:id/attachments/:attachment_id/download", h.DownloadAttachment)
		g.GET("/assets", h.ListUserAssets)
		g.GET("/assets/:id/download", h.DownloadMyUserAsset)
		g.GET("/invocations/:id", h.GetPDFInvocation)
		g.GET("/invocations/:id/download", h.DownloadPDFInvocation)
		g.GET("/invocations/:id/content", h.PDFInvocationContent)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Actor-ID", "1002")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	paths := []string{"/articles/" + draft.ThreadID + "/me?actor_id=1002&authentication_strength=mfa", "/articles/" + draft.ThreadID + "/attachments", "/articles/" + draft.ThreadID + "/attachments/" + attachment.ID + "/download", "/assets", "/assets/" + attachment.AssetID + "/download", "/invocations/" + invocation.ID, "/invocations/" + invocation.ID + "/content", "/invocations/" + invocation.ID + "/download"}
	adminToken, err := jwt.Admin.GenerateAccessToken("1001", "admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if rec := serve(ownerToken, path, true); rec.Code != 200 {
				t.Fatalf("owner %d %s", rec.Code, rec.Body.String())
			}
			if rec := serve(ownerToken, path, false); rec.Code != 401 || bytes.Contains(rec.Body.Bytes(), pdf) {
				t.Fatalf("unbound %d", rec.Code)
			}
			if rec := serve(adminToken, path, true); rec.Code != 401 {
				t.Fatalf("admin %d", rec.Code)
			}
		})
	}
	if rec := serve(otherToken, "/assets/"+attachment.AssetID+"/download", true); rec.Code != 404 || bytes.Contains(rec.Body.Bytes(), pdf) {
		t.Fatalf("other owner %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve("", "/public/"+draft.ThreadID, true); rec.Code != 404 {
		t.Fatalf("anonymous private %d %s", rec.Code, rec.Body.String())
	}
	objects := &richReadObjects{ObjectPort: s.objects}
	objects.afterOpen = func() {
		if _, err := sessions.RevokeByUser(ctx, "1001", "rich-read-test", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	s.objects = objects
	if rec := serve(ownerToken, "/assets/"+attachment.AssetID+"/download", true); rec.Code != 401 || bytes.Contains(rec.Body.Bytes(), pdf) || objects.closes != 1 {
		t.Fatalf("revoked read %d closes=%d %s", rec.Code, objects.closes, rec.Body.String())
	}
	if rec := serve(ownerToken, "/assets", true); rec.Code != 401 || strings.Contains(rec.Body.String(), "private.pdf") {
		t.Fatalf("revoked metadata %d", rec.Code)
	}
}
