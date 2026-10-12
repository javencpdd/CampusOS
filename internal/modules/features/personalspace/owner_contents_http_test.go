package space

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type ownContentsHTTPEnvironment struct {
	users    *identityrepo.MemoryUserRepository
	sessions *identityrepo.MemorySessionRepository
	identity *identityservice.SessionService
	jwts     *auth.DomainJWTManagers
	owner    *identitydomain.User
	other    *identitydomain.User
	tokens   *identityservice.SessionTokens
	threads  *communityrepo.MemoryThreadRepository
	spaces   *MemoryRepository
	query    communityport.ContentQuery
}

func newOwnContentsHTTPEnvironment(t *testing.T) *ownContentsHTTPEnvironment {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := &ownContentsHTTPEnvironment{users: identityrepo.NewMemoryUserRepository(), sessions: identityrepo.NewMemorySessionRepository(),
		threads: communityrepo.NewMemoryThreadRepository(), spaces: NewMemoryRepository()}
	e.jwts = auth.NewDomainJWTManagers(
		auth.JWTConfig{Secret: "space-owner-user-test-secret", Issuer: "space-owner-user", Audience: "user", KeyID: "user-test", AccessTTL: time.Hour, RefreshTTL: time.Hour},
		auth.JWTConfig{Secret: "space-owner-admin-test-secret", Issuer: "space-owner-admin", Audience: "admin", KeyID: "admin-test", AccessTTL: time.Hour},
	)
	var err error
	e.identity, err = identityservice.NewSessionService(e.sessions, e.users, e.jwts.User, identityservice.SessionConfig{IPHashSecret: "space-owner-test-ip-secret"})
	if err != nil {
		t.Fatal(err)
	}
	e.owner = &identitydomain.User{ID: "98431", Username: "owner", Email: "space-owner@example.test", Status: identitydomain.UserStatusActive, AuthVersion: 1}
	e.other = &identitydomain.User{ID: "98432", Username: "other", Email: "space-other@example.test", Status: identitydomain.UserStatusActive, AuthVersion: 1}
	for _, user := range []*identitydomain.User{e.owner, e.other} {
		if err := e.users.Create(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	e.tokens, err = e.identity.Issue(context.Background(), e.owner, identityservice.SessionMetadata{DeviceName: "Space Owner Test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, thread := range []*communitydomain.Thread{
		{ID: "98441", AuthorID: e.owner.ID, CategoryID: "98451", Title: "owner private title", Content: "owner-private-body", PublicationStatus: communitydomain.PublicationStatusPrivate, ModerationStatus: communitydomain.ModerationStatusTakenDown, DeletionStatus: communitydomain.DeletionStatusActive, ModerationReason: "owner private reason"},
		{ID: "98442", AuthorID: e.other.ID, CategoryID: "98451", Title: "other private title", Content: "other-private-body", PublicationStatus: communitydomain.PublicationStatusPrivate, ModerationStatus: communitydomain.ModerationStatusClear, DeletionStatus: communitydomain.DeletionStatusActive},
	} {
		if err := e.threads.Create(context.Background(), thread); err != nil {
			t.Fatal(err)
		}
	}
	e.query = community.NewContentQuery(communityservice.NewThreadService(e.threads, nil))
	return e
}

func (e *ownContentsHTTPEnvironment) revoke(ctx context.Context) error {
	_, err := e.sessions.RevokeByUser(ctx, e.owner.ID, "space-owner-test", time.Now())
	return err
}

func (e *ownContentsHTTPEnvironment) serve(token string, verifier identityport.SessionVerifier, reader identityport.SessionStrengthReader, urls ...string) *httptest.ResponseRecorder {
	svc := NewService(e.spaces, newFakeUserLookup(e.owner, e.other))
	svc.SetContentQuery(e.query)
	h := NewHandler(svc)
	h.SetOwnerSessionProof(verifier, reader)
	r := gin.New()
	r.GET("/spaces/me/contents", middleware.JWTAuth(e.jwts.User, e.identity), h.ListOwnContents)
	url := "/spaces/me/contents?author_id=98432&owner_id=98432&actor_id=98432&credential_id=forged&authentication_strength=mfa&publication_status=published&deletion_status=trashed"
	if len(urls) > 0 {
		url = urls[0]
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Actor-ID", "98432")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

type ownContentsHTTPProof struct {
	*identityservice.SessionService
	verifyError   error
	strengthError error
	strength      string
	strengthCalls int
	afterStrength func(context.Context, int) error
}

func (p *ownContentsHTTPProof) VerifyAccess(ctx context.Context, claims *auth.JWTClaims) error {
	if p.verifyError != nil {
		return p.verifyError
	}
	return p.SessionService.VerifyAccess(ctx, claims)
}

func (p *ownContentsHTTPProof) AuthenticationStrengthForSession(ctx context.Context, id string) (string, error) {
	p.strengthCalls++
	if p.strengthError != nil {
		return "", p.strengthError
	}
	strength, err := p.SessionService.AuthenticationStrengthForSession(ctx, id)
	if err != nil {
		return "", err
	}
	if p.afterStrength != nil {
		if err := p.afterStrength(ctx, p.strengthCalls); err != nil {
			return "", err
		}
	}
	if p.strength != "" {
		strength = p.strength
	}
	return strength, nil
}

type ownContentsHTTPQuery struct {
	communityport.ContentQuery
	before func(context.Context) error
	after  func(context.Context) error
	err    error
}

func (q *ownContentsHTTPQuery) ListAuthorThreadsForPrincipal(ctx context.Context, resolve func(context.Context) (identityport.PrincipalContext, error), filter communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error) {
	if q.err != nil {
		return nil, 0, q.err
	}
	if q.before != nil {
		if err := q.before(ctx); err != nil {
			return nil, 0, err
		}
	}
	query, ok := q.ContentQuery.(communityport.AuthenticatedAuthorContentQuery)
	if !ok {
		return nil, 0, ErrAuthenticatedAuthorQueryUnavailable
	}
	rows, total, err := query.ListAuthorThreadsForPrincipal(ctx, resolve, filter)
	if q.after != nil {
		if afterErr := q.after(ctx); afterErr != nil {
			return nil, 0, afterErr
		}
	}
	return rows, total, err
}

func TestOwnContentsHTTPVerifiedSessionAndDomain(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
	}{
		{"owner-forged-fields", 200}, {"other-user", 200}, {"same-id-admin", 401}, {"missing-token", 401},
		{"revoked", 401}, {"account-version-changed", 401}, {"suspended", 401}, {"missing-verifier", 401}, {"missing-strength", 401},
		{"verifier-fault", 401}, {"strength-fault", 401}, {"invalid-strength", 401}, {"revoke-during-strength", 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := newOwnContentsHTTPEnvironment(t)
			token := e.tokens.AccessToken
			var verifier identityport.SessionVerifier = e.identity
			var reader identityport.SessionStrengthReader = e.identity
			proof := &ownContentsHTTPProof{SessionService: e.identity}
			switch test.name {
			case "other-user":
				tokens, err := e.identity.Issue(context.Background(), e.other, identityservice.SessionMetadata{})
				if err != nil {
					t.Fatal(err)
				}
				token = tokens.AccessToken
			case "same-id-admin":
				var err error
				token, err = e.jwts.Admin.GenerateAccessToken(e.owner.ID, "admin", auth.AccessTokenContext{SessionID: e.tokens.Session.ID, AuthVersion: 1})
				if err != nil {
					t.Fatal(err)
				}
			case "missing-token":
				token = ""
			case "revoked":
				if err := e.revoke(context.Background()); err != nil {
					t.Fatal(err)
				}
			case "account-version-changed":
				e.owner.AuthVersion++
				if err := e.users.Update(context.Background(), e.owner); err != nil {
					t.Fatal(err)
				}
			case "suspended":
				e.owner.Status = identitydomain.UserStatusSuspended
				if err := e.users.Update(context.Background(), e.owner); err != nil {
					t.Fatal(err)
				}
			case "missing-verifier":
				verifier = nil
			case "missing-strength":
				reader = nil
			case "verifier-fault":
				proof.verifyError = errors.New("private-verifier-error")
				verifier = proof
			case "strength-fault":
				proof.strengthError = errors.New("private-strength-error")
				reader = proof
			case "invalid-strength":
				proof.strength = "admin"
				reader = proof
			case "revoke-during-strength":
				proof.afterStrength = func(ctx context.Context, _ int) error { return e.revoke(ctx) }
				reader = proof
			}
			rec := e.serve(token, verifier, reader)
			if rec.Code != test.status {
				t.Fatalf("status=%d want=%d: %s", rec.Code, test.status, rec.Body.String())
			}
			body := rec.Body.String()
			for _, hidden := range []string{"facts_version", "credential_id", "private-verifier-error", "private-strength-error"} {
				if strings.Contains(body, hidden) {
					t.Fatalf("leaked %q: %s", hidden, body)
				}
			}
			if test.status != 200 && (strings.Contains(body, "private-body") || strings.Contains(body, "private reason")) {
				t.Fatalf("failure leaked private content: %s", body)
			}
			if test.name == "owner-forged-fields" && (!strings.Contains(body, "owner-private-body") || !strings.Contains(body, "owner private reason") || strings.Contains(body, "other-private-body")) {
				t.Fatalf("client fields selected another owner: %s", body)
			}
			if test.name == "other-user" && (!strings.Contains(body, "other-private-body") || strings.Contains(body, "owner-private-body")) {
				t.Fatalf("current principal did not select its own data: %s", body)
			}
		})
	}
}

func TestOwnContentsHTTPEmptyDisabledAndRevocation(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
	}{{"disabled", 200}, {"disabled-final-revoke", 401}, {"before-query-revoke", 401}, {"after-query-revoke", 401}, {"empty-final-revoke", 401}} {
		t.Run(test.name, func(t *testing.T) {
			e := newOwnContentsHTTPEnvironment(t)
			proof := &ownContentsHTTPProof{SessionService: e.identity}
			if strings.HasPrefix(test.name, "disabled") {
				if err := e.spaces.Upsert(context.Background(), &Space{UserID: e.owner.ID, SyncEnabled: false}); err != nil {
					t.Fatal(err)
				}
			}
			switch test.name {
			case "disabled-final-revoke":
				proof.afterStrength = func(ctx context.Context, calls int) error {
					if calls == 2 {
						return e.revoke(ctx)
					}
					return nil
				}
			case "before-query-revoke":
				e.query = &ownContentsHTTPQuery{ContentQuery: e.query, before: e.revoke}
			case "after-query-revoke":
				e.query = &ownContentsHTTPQuery{ContentQuery: e.query, after: e.revoke}
			case "empty-final-revoke":
				if err := e.threads.Purge(context.Background(), "98441"); err != nil {
					t.Fatal(err)
				}
				e.query = &ownContentsHTTPQuery{ContentQuery: e.query, after: e.revoke}
			}
			rec := e.serve(e.tokens.AccessToken, e.identity, proof)
			if rec.Code != test.status {
				t.Fatalf("status=%d want=%d: %s", rec.Code, test.status, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "private-body") {
				t.Fatalf("revocation/disabled leaked data: %s", rec.Body.String())
			}
			if test.name == "disabled" && (!strings.Contains(rec.Body.String(), `"items":[]`) || !strings.Contains(rec.Body.String(), `"total":0`) || proof.strengthCalls != 2) {
				t.Fatalf("disabled list skipped final proof or changed empty shape: calls=%d %s", proof.strengthCalls, rec.Body.String())
			}
		})
	}
}

func TestOwnContentsHTTPFailureBoundary(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		machine string
	}{{"missing-query", 500, "internal.error"}, {"legacy-query", 500, "internal.error"}, {"query-fault", 500, "internal.error"}, {"conflict", 409, "resource.conflict"}, {"principal-drift-before-query", 409, "resource.conflict"}, {"principal-drift-after-query", 409, "resource.conflict"}} {
		t.Run(test.name, func(t *testing.T) {
			e := newOwnContentsHTTPEnvironment(t)
			proof := &ownContentsHTTPProof{SessionService: e.identity}
			switch test.name {
			case "missing-query":
				e.query = nil
			case "legacy-query":
				e.query = &capturingContentQuery{}
			case "query-fault":
				e.query = &ownContentsHTTPQuery{ContentQuery: e.query, err: errors.New("private-query-dsn-do-not-leak")}
			case "conflict":
				e.query = &ownContentsHTTPQuery{ContentQuery: e.query, err: identityport.ErrPolicyFactsChanged}
			case "principal-drift-before-query":
				proof.afterStrength = func(_ context.Context, calls int) error {
					if calls == 2 {
						proof.strength = "mfa"
					}
					return nil
				}
			case "principal-drift-after-query":
				e.query = &ownContentsHTTPQuery{ContentQuery: e.query, after: func(context.Context) error { proof.strength = "mfa"; return nil }}
			}
			rec := e.serve(e.tokens.AccessToken, e.identity, proof)
			if rec.Code != test.status || !strings.Contains(rec.Body.String(), test.machine) {
				t.Fatalf("safe error contract changed: %d %s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "private-query") || strings.Contains(rec.Body.String(), "private-body") || strings.Contains(rec.Body.String(), "authenticated community") {
				t.Fatalf("internal error leaked: %s", rec.Body.String())
			}
		})
	}
}

func TestOwnContentsHTTPEmptyPageKeepsOwnerCount(t *testing.T) {
	e := newOwnContentsHTTPEnvironment(t)
	proof := &ownContentsHTTPProof{SessionService: e.identity}
	rec := e.serve(e.tokens.AccessToken, e.identity, proof, "/spaces/me/contents?page=2&page_size=1&author_id=98432")
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `"items":[]`) || !strings.Contains(body, `"total":1`) || !strings.Contains(body, `"page":2`) || proof.strengthCalls < 4 {
		t.Fatalf("empty page lost owner count or current proof: status=%d proofs=%d %s", rec.Code, proof.strengthCalls, body)
	}
	if strings.Contains(body, "private-body") || strings.Contains(body, "private reason") {
		t.Fatalf("empty page returned private content: %s", body)
	}
}

func TestOwnContentsHTTPRejectsIncompleteHostProof(t *testing.T) {
	for _, name := range []string{"missing-user", "wrong-user-type", "missing-session", "missing-auth-version", "zero-auth-version"} {
		t.Run(name, func(t *testing.T) {
			e := newOwnContentsHTTPEnvironment(t)
			svc := NewService(e.spaces, newFakeUserLookup(e.owner))
			svc.SetContentQuery(e.query)
			h := NewHandler(svc)
			h.SetOwnerSessionProof(e.identity, e.identity)
			r := gin.New()
			r.GET("/spaces/me/contents", func(c *gin.Context) {
				if name != "missing-user" {
					if name == "wrong-user-type" {
						c.Set("user_id", 98431)
					} else {
						c.Set("user_id", e.owner.ID)
					}
				}
				if name != "missing-session" {
					c.Set("session_id", e.tokens.Session.ID)
				}
				if name != "missing-auth-version" && name != "zero-auth-version" {
					c.Set("auth_version", int64(1))
				}
				h.ListOwnContents(c)
			})
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/spaces/me/contents?session_id=forged&auth_version=1&author_id=98431", nil))
			if rec.Code != 401 || strings.Contains(rec.Body.String(), "private-body") {
				t.Fatalf("incomplete host context accepted query proof: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}
