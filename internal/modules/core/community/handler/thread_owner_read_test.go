package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type ownerHTTPEnvironment struct {
	users    *identityrepo.MemoryUserRepository
	sessions *identityrepo.MemorySessionRepository
	identity *identityservice.SessionService
	jwts     *auth.DomainJWTManagers
	owner    *identitydomain.User
	tokens   *identityservice.SessionTokens
	thread   *domain.Thread
	repo     *ownerHTTPRepository
}

func newOwnerHTTPEnvironment(t *testing.T) *ownerHTTPEnvironment {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	env := &ownerHTTPEnvironment{users: identityrepo.NewMemoryUserRepository(), sessions: identityrepo.NewMemorySessionRepository()}
	env.jwts = auth.NewDomainJWTManagers(
		auth.JWTConfig{Secret: "owner-http-user-test-secret", Issuer: "owner-http-user", Audience: "user", KeyID: "user-test-key", AccessTTL: time.Hour, RefreshTTL: time.Hour},
		auth.JWTConfig{Secret: "owner-http-admin-test-secret", Issuer: "owner-http-admin", Audience: "admin", KeyID: "admin-test-key", AccessTTL: time.Hour},
	)
	var err error
	env.identity, err = identityservice.NewSessionService(env.sessions, env.users, env.jwts.User, identityservice.SessionConfig{IPHashSecret: "owner-http-ip-test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	env.owner = &identitydomain.User{ID: "98301", Username: "owner", Email: "owner@example.test", Status: identitydomain.UserStatusActive, AuthVersion: 1}
	if err := env.users.Create(ctx, env.owner); err != nil {
		t.Fatal(err)
	}
	env.tokens, err = env.identity.Issue(ctx, env.owner, identityservice.SessionMetadata{DeviceName: "Owner HTTP Test"})
	if err != nil {
		t.Fatal(err)
	}
	env.repo = &ownerHTTPRepository{MemoryThreadRepository: repository.NewMemoryThreadRepository()}
	env.thread = &domain.Thread{ID: "98311", AuthorID: env.owner.ID, CategoryID: "98321", Title: "private title", Content: "private-owner-body",
		PublicationStatus: domain.PublicationStatusPrivate, ModerationStatus: domain.ModerationStatusClear, DeletionStatus: domain.DeletionStatusActive}
	if err := env.repo.Create(ctx, env.thread); err != nil {
		t.Fatal(err)
	}
	return env
}

func (e *ownerHTTPEnvironment) serve(token string, verifier identityport.SessionVerifier, reader identityport.SessionStrengthReader) *httptest.ResponseRecorder {
	h := NewThreadHandler(service.NewThreadService(e.repo, nil))
	h.SetOwnerSessionProof(verifier, reader)
	router := gin.New()
	router.GET("/threads/:id/me", middleware.JWTAuth(e.jwts.User, e.identity), h.GetThreadForCurrentUser)
	req := httptest.NewRequest(http.MethodGet, "/threads/98311/me?author_id=98301&actor_kind=user&actor_id=98301&credential_id=forged&authentication_strength=mfa&facts_version=1", nil)
	req.Header.Set("X-Actor-ID", e.owner.ID)
	req.Header.Set("X-Authentication-Strength", "mfa")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func (e *ownerHTTPEnvironment) revoke(ctx context.Context) error {
	_, err := e.sessions.RevokeByUser(ctx, e.owner.ID, "owner-http-test", time.Now())
	return err
}

type ownerHTTPRepository struct {
	*repository.MemoryThreadRepository
	readError  error
	finalError error
	initial    func(*domain.Thread)
	before     func(context.Context, string, int) error
	rechecks   int
}

func (r *ownerHTTPRepository) GetByID(ctx context.Context, id string) (*domain.Thread, error) {
	if r.readError != nil {
		return nil, r.readError
	}
	thread, err := r.MemoryThreadRepository.GetByID(ctx, id)
	if err == nil && r.initial != nil {
		r.initial(thread)
	}
	return thread, err
}

func (r *ownerHTTPRepository) RecheckOwnerThread(ctx context.Context, id, ownerID string, version int64) (*domain.Thread, error) {
	r.rechecks++
	if r.finalError != nil {
		return nil, r.finalError
	}
	if r.before != nil {
		if err := r.before(ctx, id, r.rechecks); err != nil {
			return nil, err
		}
	}
	return r.MemoryThreadRepository.RecheckOwnerThread(ctx, id, ownerID, version)
}

type ownerHTTPProof struct {
	*identityservice.SessionService
	verifyError   error
	strengthError error
	strength      string
	afterStrength func(context.Context) error
}

func (p *ownerHTTPProof) VerifyAccess(ctx context.Context, claims *auth.JWTClaims) error {
	if p.verifyError != nil {
		return p.verifyError
	}
	return p.SessionService.VerifyAccess(ctx, claims)
}

func (p *ownerHTTPProof) AuthenticationStrengthForSession(ctx context.Context, id string) (string, error) {
	if p.strengthError != nil {
		return "", p.strengthError
	}
	strength, err := p.SessionService.AuthenticationStrengthForSession(ctx, id)
	if err != nil {
		return "", err
	}
	if p.afterStrength != nil {
		if err := p.afterStrength(ctx); err != nil {
			return "", err
		}
	}
	if p.strength != "" {
		strength = p.strength
	}
	return strength, nil
}

func TestThreadOwnerHTTPVerifiedSessionAndDomain(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
	}{
		{"private", 200}, {"draft", 200}, {"trash", 200}, {"taken-down", 200}, {"purged", 404},
		{"public-other-user", 200}, {"other-owner", 404}, {"other-user-token", 404}, {"missing-token", 401}, {"same-id-admin", 401},
		{"revoked", 401}, {"auth-version-changed", 401}, {"suspended", 401}, {"expired-session", 401}, {"different-user-session", 401},
		{"verifier-fault", 401}, {"strength-fault", 401}, {"invalid-strength", 401}, {"no-verifier", 401}, {"no-strength", 401}, {"revoke-during-strength", 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := newOwnerHTTPEnvironment(t)
			ctx := context.Background()
			token := env.tokens.AccessToken
			var verifier identityport.SessionVerifier = env.identity
			var reader identityport.SessionStrengthReader = env.identity
			proof := &ownerHTTPProof{SessionService: env.identity}
			switch test.name {
			case "draft":
				env.thread.PublicationStatus = domain.PublicationStatusDraft
			case "trash":
				env.thread.DeletionStatus = domain.DeletionStatusTrashed
			case "taken-down":
				env.thread.ModerationStatus = domain.ModerationStatusTakenDown
			case "purged":
				env.thread.DeletionStatus = domain.DeletionStatusPurged
			case "other-owner", "public-other-user":
				env.thread.AuthorID = "98302"
				if test.name == "public-other-user" {
					env.thread.PublicationStatus = domain.PublicationStatusPublished
				}
			case "missing-token":
				token = ""
			case "same-id-admin":
				var err error
				token, err = env.jwts.Admin.GenerateAccessToken(env.owner.ID, "admin", auth.AccessTokenContext{SessionID: env.tokens.Session.ID, AuthVersion: 1})
				if err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if err := env.revoke(ctx); err != nil {
					t.Fatal(err)
				}
			case "auth-version-changed":
				if _, err := env.users.BumpAuthVersion(ctx, env.owner.ID); err != nil {
					t.Fatal(err)
				}
			case "suspended":
				env.owner.Status = identitydomain.UserStatusSuspended
				if err := env.users.Update(ctx, env.owner); err != nil {
					t.Fatal(err)
				}
			case "expired-session":
				env.tokens.Session.ExpiresAt = time.Now().Add(-time.Minute)
				if err := env.sessions.Update(ctx, env.tokens.Session); err != nil {
					t.Fatal(err)
				}
			case "different-user-session", "other-user-token":
				other := &identitydomain.User{ID: "98302", Username: "other", Email: "other@example.test", Status: identitydomain.UserStatusActive, AuthVersion: 1}
				if err := env.users.Create(ctx, other); err != nil {
					t.Fatal(err)
				}
				otherTokens, err := env.identity.Issue(ctx, other, identityservice.SessionMetadata{})
				if err != nil {
					t.Fatal(err)
				}
				if test.name == "other-user-token" {
					token = otherTokens.AccessToken
				} else {
					token, err = env.jwts.User.GenerateAccessToken(env.owner.ID, env.owner.Username, auth.AccessTokenContext{SessionID: otherTokens.Session.ID, AuthVersion: 1})
					if err != nil {
						t.Fatal(err)
					}
				}
			case "verifier-fault":
				proof.verifyError = errors.New("secret-session-error")
				verifier = proof
			case "strength-fault":
				proof.strengthError = errors.New("secret-session-error")
				reader = proof
			case "invalid-strength":
				proof.strength = "workload"
				reader = proof
			case "no-verifier":
				verifier = nil
			case "no-strength":
				reader = nil
			case "revoke-during-strength":
				proof.afterStrength = env.revoke
				reader = proof
			}
			if err := env.repo.Update(ctx, env.thread); err != nil {
				t.Fatal(err)
			}
			rec := env.serve(token, verifier, reader)
			assertOwnerHTTPResponse(t, rec, test.status)
			if test.status == http.StatusOK && !strings.Contains(rec.Body.String(), "private-owner-body") {
				t.Fatalf("allowed response lost its content: %s", rec.Body.String())
			}
			stored, err := env.repo.MemoryThreadRepository.GetByID(ctx, env.thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantViews := int64(0)
			if test.name == "public-other-user" {
				wantViews = 1
			}
			if stored.ViewCount != wantViews {
				t.Fatalf("views: got %d want %d", stored.ViewCount, wantViews)
			}
		})
	}
}

func assertOwnerHTTPResponse(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status: got %d want %d: %s", rec.Code, status, rec.Body.String())
	}
	body := rec.Body.String()
	for _, hidden := range []string{"facts_version", "secret-session-error", "secret-database-error", "/private/database/path"} {
		if strings.Contains(body, hidden) {
			t.Fatalf("response leaked %s: %s", hidden, body)
		}
	}
	if status != http.StatusOK && (strings.Contains(body, "private-owner-body") || strings.Contains(body, "fresh-owner-body")) {
		t.Fatalf("denied response returned private content: %s", body)
	}
	if status == http.StatusInternalServerError && !strings.Contains(body, `"code":"internal.error"`) {
		t.Fatalf("unsafe internal error contract: %s", body)
	}
	if status == http.StatusConflict && !strings.Contains(body, `"code":"resource.conflict"`) {
		t.Fatalf("missing stable conflict contract: %s", body)
	}
}

func TestThreadOwnerHTTPRejectsIncompleteHostProof(t *testing.T) {
	for _, test := range []struct {
		name        string
		sessionID   any
		authVersion any
	}{
		{"only-user-id", nil, nil}, {"missing-version", "session", nil}, {"missing-session", nil, int64(1)},
		{"wrong-version-type", "session", "1"}, {"wrong-session-type", int64(1), int64(1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := newOwnerHTTPEnvironment(t)
			h := NewThreadHandler(service.NewThreadService(env.repo, nil))
			h.SetOwnerSessionProof(env.identity, env.identity)
			router := gin.New()
			router.GET("/threads/:id/me", func(c *gin.Context) {
				c.Set("user_id", env.owner.ID)
				if test.sessionID != nil {
					c.Set("session_id", test.sessionID)
				}
				if test.authVersion != nil {
					c.Set("auth_version", test.authVersion)
				}
				c.Next()
			}, h.GetThreadForCurrentUser)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/threads/98311/me?session_id=forged&auth_version=1&authentication_strength=mfa", nil))
			assertOwnerHTTPResponse(t, rec, http.StatusUnauthorized)
		})
	}
}

func TestThreadOwnerHTTPRechecksContentAndSession(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
	}{
		{"aba", 200}, {"owner-transfer", 404}, {"purge", 404}, {"busy", 409}, {"revoke-after-snapshot", 401},
		{"read-fault", 500}, {"recheck-fault", 500}, {"missing-state", 500}, {"missing-board", 500}, {"missing-version", 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := newOwnerHTTPEnvironment(t)
			env.repo.before = func(ctx context.Context, id string, call int) error {
				if call != 1 && test.name != "busy" {
					return nil
				}
				if test.name == "purge" {
					return env.repo.MemoryThreadRepository.Purge(ctx, id)
				}
				if test.name == "revoke-after-snapshot" {
					return env.revoke(ctx)
				}
				thread, err := env.repo.MemoryThreadRepository.GetByID(ctx, id)
				if err != nil {
					return err
				}
				switch test.name {
				case "aba":
					thread.PublicationStatus = domain.PublicationStatusPublished
					if err := env.repo.MemoryThreadRepository.Update(ctx, thread); err != nil {
						return err
					}
					thread.PublicationStatus = domain.PublicationStatusPrivate
					thread.Content = "fresh-owner-body"
				case "owner-transfer":
					thread.AuthorID = "98302"
					thread.Content = "fresh-owner-body"
				case "busy":
					thread.Title += " changed"
				default:
					return nil
				}
				return env.repo.MemoryThreadRepository.Update(ctx, thread)
			}
			switch test.name {
			case "read-fault":
				env.repo.readError = errors.New("secret-database-error /private/database/path")
			case "recheck-fault":
				env.repo.finalError = errors.New("secret-database-error /private/database/path")
			case "missing-state":
				env.repo.initial = func(thread *domain.Thread) { thread.ModerationStatus = "" }
			case "missing-board":
				env.repo.initial = func(thread *domain.Thread) { thread.CategoryID = "" }
			case "missing-version":
				env.repo.initial = func(thread *domain.Thread) { thread.FactsVersion = 0 }
			}
			rec := env.serve(env.tokens.AccessToken, env.identity, env.identity)
			assertOwnerHTTPResponse(t, rec, test.status)
			if test.name == "aba" && (!strings.Contains(rec.Body.String(), "fresh-owner-body") || strings.Contains(rec.Body.String(), "private-owner-body") || env.repo.rechecks != 2) {
				t.Fatalf("ABA did not reload/recheck current content: calls=%d body=%s", env.repo.rechecks, rec.Body.String())
			}
			if test.name == "busy" && env.repo.rechecks != 3 {
				t.Fatalf("busy owner read retries=%d want 3", env.repo.rechecks)
			}
			if stored, err := env.repo.MemoryThreadRepository.GetByID(context.Background(), env.thread.ID); err == nil && stored.ViewCount != 0 {
				t.Fatalf("owner read changed view counter: %d", stored.ViewCount)
			}
		})
	}
}
