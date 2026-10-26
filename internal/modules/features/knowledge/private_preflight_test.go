package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/gin-gonic/gin"
)

type privateMetadataTestReader struct {
	metadata PrivateDocumentMetadata
	calls    int
	read     func(context.Context, identityport.SessionPrincipalResolver, string) (PrivateDocumentMetadata, error)
}

func (r *privateMetadataTestReader) GetOwnDocumentMetadata(ctx context.Context, resolve identityport.SessionPrincipalResolver, id string) (PrivateDocumentMetadata, error) {
	r.calls++
	if r.read != nil {
		return r.read(ctx, resolve, id)
	}
	p, err := resolve(ctx)
	if err != nil {
		return PrivateDocumentMetadata{}, err
	}
	if p.Actor.ID != "101" {
		return PrivateDocumentMetadata{}, ErrPrivateSourceMissing
	}
	return r.metadata, nil
}
func privateTestPrincipal() identityport.PrincipalContext {
	return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "101"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "session-101"}
}
func privateTestResolver(context.Context) (identityport.PrincipalContext, error) {
	return privateTestPrincipal(), nil
}
func privateTestService() (*privateSourcePreflight, *privateMetadataTestReader) {
	r := &privateMetadataTestReader{metadata: PrivateDocumentMetadata{DocumentID: "201", VersionID: "301", Format: "text"}}
	return &privateSourcePreflight{reader: r, enabled: func() bool { return true }}, r
}

var privateTestRequest = PrivateSourcePreflightRequest{DocumentID: "201", VersionID: "301"}

func TestPrivatePreflightAlwaysRefusesIndexingWithoutProtectedPath(t *testing.T) {
	for _, format := range []string{"text", "markdown", "campusdoc", "pdf", "docx"} {
		t.Run(format, func(t *testing.T) {
			s, r := privateTestService()
			r.metadata.Format = format
			out, err := s.document(t.Context(), privateTestRequest, privateTestResolver)
			want := "private_storage_unavailable"
			if format == "pdf" || format == "docx" {
				want = "unsupported_format"
			}
			if err != nil || out.CanIndex || out.Reason != want || out.DocumentID != "201" || out.VersionID != "301" || out.Format != format || r.calls != 2 {
				t.Fatalf("unsafe preflight %+v calls=%d err=%v", out, r.calls, err)
			}
			encoded, _ := json.Marshal(out)
			var fields map[string]any
			_ = json.Unmarshal(encoded, &fields)
			if len(fields) != 5 {
				t.Fatal("unbounded private projection", string(encoded))
			}
		})
	}
}
func TestPrivatePreflightRejectsChangedAuthorityAndSource(t *testing.T) {
	for _, which := range []string{"revoked", "owner", "credential", "version", "format", "feature", "stop", "canceled"} {
		t.Run(which, func(t *testing.T) {
			s, r := privateTestService()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			p := privateTestPrincipal()
			revoked := false
			enabled := true
			s.enabled = func() bool { return enabled }
			resolve := func(context.Context) (identityport.PrincipalContext, error) {
				if revoked {
					return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
				}
				return p, nil
			}
			r.read = func(ctx context.Context, bound identityport.SessionPrincipalResolver, _ string) (PrivateDocumentMetadata, error) {
				if _, err := bound(ctx); err != nil {
					return PrivateDocumentMetadata{}, err
				}
				got := r.metadata
				if r.calls == 1 {
					switch which {
					case "revoked":
						revoked = true
					case "owner":
						p.Actor.ID = "102"
					case "credential":
						p.CredentialID = "session-102"
					case "version":
						r.metadata.VersionID = "302"
					case "format":
						r.metadata.Format = "markdown"
					case "feature":
						enabled = false
					case "stop":
						s.stopped.Store(true)
					case "canceled":
						cancel()
					}
				}
				return got, nil
			}
			out, err := s.document(ctx, privateTestRequest, resolve)
			if err == nil || out != (PrivateSourcePreflightResult{}) {
				t.Fatalf("late facts released output %+v err=%v", out, err)
			}
			want := ErrSourceChanged
			switch which {
			case "revoked":
				want = identityport.ErrUserSessionPrincipalUnavailable
			case "owner", "credential":
				want = identityport.ErrPolicyFactsChanged
			case "feature":
				want = ErrDisabled
			case "stop":
				want = ErrPrivateUnavailable
			case "canceled":
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
		})
	}
}
func TestPrivatePreflightRejectsInvalidOrUnavailableBeforeSourceRead(t *testing.T) {
	for _, which := range []string{"nilResolver", "admin", "machine", "audience", "badID", "badVersion", "stale", "missing", "unknownFormat", "nilReader", "typedNilReader", "disabled", "stopped"} {
		t.Run(which, func(t *testing.T) {
			s, r := privateTestService()
			resolve := identityport.SessionPrincipalResolver(privateTestResolver)
			req := privateTestRequest
			switch which {
			case "nilResolver":
				resolve = nil
			case "admin", "machine", "audience":
				resolve = func(context.Context) (identityport.PrincipalContext, error) {
					p := privateTestPrincipal()
					if which == "audience" {
						p.Audience = "admin"
					} else {
						p.Actor.Kind = which
					}
					return p, nil
				}
			case "badID":
				req.DocumentID = "0201"
			case "badVersion":
				req.VersionID = "+301"
			case "stale":
				req.VersionID = "300"
			case "missing":
				r.read = func(context.Context, identityport.SessionPrincipalResolver, string) (PrivateDocumentMetadata, error) {
					return PrivateDocumentMetadata{}, ErrPrivateSourceMissing
				}
			case "unknownFormat":
				r.metadata.Format = "html"
			case "nilReader":
				s.reader = nil
			case "typedNilReader":
				s.reader = (*privateMetadataTestReader)(nil)
			case "disabled":
				s.enabled = func() bool { return false }
			case "stopped":
				s.stopped.Store(true)
			}
			out, err := s.document(t.Context(), req, resolve)
			if err == nil || out != (PrivateSourcePreflightResult{}) {
				t.Fatalf("unsafe admission %+v %v", out, err)
			}
			if which != "stale" && which != "missing" && which != "unknownFormat" && r.calls != 0 {
				t.Fatal("read on rejected ingress")
			}
		})
	}
}

type privateSessionTestProof struct {
	revoked bool
	calls   int
}

func (p *privateSessionTestProof) VerifyAccess(_ context.Context, c *auth.JWTClaims) error {
	p.calls++
	if p.revoked || c.UserID != "101" || c.SessionID != "session-101" || c.AuthVersion != 1 || c.TokenType != auth.AccessTokenType {
		return identityport.ErrUserSessionPrincipalUnavailable
	}
	return nil
}
func (p *privateSessionTestProof) AuthenticationStrengthForSession(context.Context, string) (string, error) {
	return "password", nil
}
func TestPrivatePreflightHTTPClosedRequestAndCurrentUser(t *testing.T) {
	for _, tc := range []struct {
		name, raw, query string
		revoked, admin   bool
		want             int
	}{
		{name: "current", raw: `{"document_id":"201","version_id":"301"}`, want: 200},
		{name: "revoked", raw: `{"document_id":"201","version_id":"301"}`, revoked: true, want: 401},
		{name: "admin", raw: `{"document_id":"201","version_id":"301"}`, admin: true, want: 401},
		{name: "ownerOverride", raw: `{"document_id":"201","version_id":"301","owner_id":"101"}`, want: 400},
		{name: "protectedOverride", raw: `{"document_id":"201","version_id":"301","protected":true}`, want: 400},
		{name: "consentOverride", raw: `{"document_id":"201","version_id":"301","consent":true}`, want: 400},
		{name: "profileOverride", raw: `{"document_id":"201","version_id":"301","profile_id":"1"}`, want: 400},
		{name: "escapedDuplicate", raw: `{"document_id":"201","\u0064ocument_id":"202","version_id":"301"}`, want: 400},
		{name: "queryOverride", raw: `{"document_id":"201","version_id":"301"}`, query: "?owner_id=101", want: 400},
		{name: "null", raw: `{"document_id":null,"version_id":"301"}`, want: 400},
		{name: "number", raw: `{"document_id":201,"version_id":"301"}`, want: 400},
		{name: "unicode", raw: `{"document_id":"\ud800","version_id":"301"}`, want: 400},
		{name: "negative", raw: `{"document_id":"-1","version_id":"301"}`, want: 400},
		{name: "zero", raw: `{"document_id":"0","version_id":"301"}`, want: 400},
		{name: "overflow", raw: `{"document_id":"9223372036854775808","version_id":"301"}`, want: 400},
		{name: "missingField", raw: `{"document_id":"201"}`, want: 400},
		{name: "trailing", raw: `{"document_id":"201","version_id":"301"}{}`, want: 400},
		{name: "tooLarge", raw: strings.Repeat(" ", 1024) + `{}`, want: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := privateTestService()
			proof := &privateSessionTestProof{revoked: tc.revoked}
			h := &Handler{privateSources: s, userVerifier: proof, userStrength: proof}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/v1/knowledge/private/sources/preflight"+tc.query, strings.NewReader(tc.raw))
			c.Set("user_id", "101")
			c.Set("session_id", "session-101")
			c.Set("auth_version", int64(1))
			if tc.admin {
				c.Set("admin_account_id", "101")
			}
			h.PrivateSourcePreflight(c)
			if w.Code != tc.want || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("status=%d want=%d cache=%s body=%s", w.Code, tc.want, w.Header().Get("Cache-Control"), w.Body.String())
			}
			if tc.want != 200 && r.calls != 0 {
				t.Fatal("source loaded on rejected request")
			}
			if tc.want == 200 && (proof.calls < 4 || !strings.Contains(w.Body.String(), `"can_index":false`)) {
				t.Fatal("not current verified user denial", w.Body.String())
			}
		})
	}
}

func TestPrivatePreflightModuleRejectsMissingProofOnRepeatedStart(t *testing.T) {
	for _, which := range []string{"missingVerifier", "wrongVerifier", "missingStrength", "wrongStrength"} {
		t.Run(which, func(t *testing.T) {
			app := platformmodule.NewAppContext()
			proof := &privateSessionTestProof{}
			if which == "wrongVerifier" {
				_ = app.Provide("identity.session-verifier", struct{}{})
			} else if which != "missingVerifier" {
				_ = app.Provide("identity.session-verifier", proof)
			}
			if which == "wrongStrength" {
				_ = app.Provide("identity.session-strength", struct{}{})
			}
			_, reader := privateTestService()
			m := NewModuleWithPrivateDocuments(func() bool { return true }, reader)
			m.app = app
			m.service = &PublicSourceService{}
			m.articles = &PublicArticleSourceService{}
			m.indexRequests = &indexRequestConsumer{}
			m.backfill = &indexBackfiller{}
			for attempt := 0; attempt < 2; attempt++ {
				if err := m.Start(t.Context()); !errors.Is(err, ErrPrivateUnavailable) {
					t.Fatalf("attempt %d admitted incomplete proof: %v", attempt, err)
				}
				if m.handler != nil || m.collections != nil {
					t.Fatal("failed start published partial state")
				}
				if _, ok := app.Lookup(PortCollections); ok {
					t.Fatal("failed start published collections")
				}
				if _, ok := app.Lookup(PortPublicRetrieval); ok {
					t.Fatal("failed start published retrieval")
				}
			}
		})
	}
}
