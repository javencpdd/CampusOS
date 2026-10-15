package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type eventMetadataHTTPEnv struct {
	owner     *ownerHTTPEnvironment
	admins    *identityrepo.MemoryAdminDomainRepository
	sessions  *identityrepo.MemoryAdminSessionRepository
	admin     *identityservice.AdminSessionService
	authority identityport.AdminExecutionAuthorityReader
	bus       *eventbus.MemoryEventBus
	h         *EventHandler
	token     string
}

func newEventMetadataHTTP(t *testing.T, mode string) eventMetadataHTTPEnv {
	t.Helper()
	ctx := context.Background()
	e := eventMetadataHTTPEnv{owner: newOwnerHTTPEnvironment(t), admins: identityrepo.NewMemoryAdminDomainRepository(), sessions: identityrepo.NewMemoryAdminSessionRepository(), bus: eventbus.NewMemoryEventBus()}
	if err := e.admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: e.owner.owner.ID, Username: "events-admin", Email: "events-admin@example.test"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	at := now.Add(-time.Minute)
	strength := "mfa"
	if mode == "password" {
		strength = "password"
	}
	if mode == "expired_mfa" {
		at = now.Add(-time.Hour)
	}
	if err := e.sessions.CreateSession(ctx, identityrepo.AdminSession{ID: "adms.events", AccountID: e.owner.owner.ID, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: "adms.events", AuthenticationStrength: strength, MFAAuthenticatedAt: &at, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	e.admin = identityservice.NewAdminSessionService(e.admins, e.sessions, e.owner.jwts.Admin)
	if err := e.admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.events", AccountID: e.owner.owner.ID, RoleID: "security_admin", GrantedBy: e.owner.owner.ID}); err != nil {
		t.Fatal(err)
	}
	codes := []string{"platform.reliability.read"}
	if mode == "no_code" {
		codes = nil
	}
	if mode == "wrong_code" {
		codes = []string{"community.thread.read"}
	}
	if err := e.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", codes, "events-test"); err != nil {
		t.Fatal(err)
	}
	e.authority = identityservice.NewBoardGovernanceAuthorityService(nil, e.admins, e.admin)
	e.h = NewEventHandler(e.bus)
	e.h.SetAdminAuthority(e.admin, e.authority)
	subject := e.owner.owner.ID
	if mode == "forged_subject" {
		subject = "other-admin"
	}
	var err error
	e.token, err = e.owner.jwts.Admin.GenerateAccessToken(subject, "Admin", auth.AccessTokenContext{SessionID: "adms.events", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func (e eventMetadataHTTPEnv) serve(path string) *httptest.ResponseRecorder {
	r := gin.New()
	r.GET("/events", middleware.RequireAdminDomainAccess(e.owner.jwts.Admin, portadapt.NewAdminDomainSessionVerifier(e.admin), nil), e.h.ListEvents)
	req := httptest.NewRequest("GET", path, nil)
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	req.Header.Set("X-Actor-ID", e.owner.owner.ID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
func TestEventMetadataHTTPCurrentAdminAndPrivateFields(t *testing.T) {
	for _, mode := range []string{"current", "password", "expired_mfa", "no_code", "wrong_code", "missing_source", "missing_session", "forged_subject", "user_token", "revoked", "empty", "no_token", "invalid_limit", "invalid_type", "filter"} {
		t.Run(mode, func(t *testing.T) {
			e := newEventMetadataHTTP(t, mode)
			ctx := context.Background()
			expected := 200
			path := "/events?limit=20&actor_kind=user&scope=all"
			if mode != "empty" {
				event := eventbus.NewEvent(eventbus.EventThreadCreated, "private-source", "private-subject", map[string]string{"content": "private-body", "token": "private-token"})
				event.Extensions = map[string]string{"secret": "private-extension"}
				e.bus.Publish(ctx, event)
			}
			switch mode {
			case "no_code", "wrong_code":
				expected = 403
			case "missing_source":
				e.h.SetAdminAuthority(e.admin, nil)
				expected = 503
			case "missing_session":
				e.h.SetAdminAuthority(nil, e.authority)
				expected = 401
			case "forged_subject":
				expected = 401
			case "user_token":
				e.token = e.owner.tokens.AccessToken
				expected = 401
			case "no_token":
				e.token = ""
				expected = 401
			case "revoked":
				e.sessions.RevokeSessionCAS(ctx, "adms.events", 1, "test", time.Now())
				expected = 401
			case "invalid_limit":
				path = "/events?limit=0"
				expected = 400
			case "invalid_type":
				path = "/events?type=private-type"
				expected = 400
			case "filter":
				path = "/events?type=post.created&limit=1"
				e.bus.Publish(ctx, eventbus.NewEvent(eventbus.EventPostCreated, "private", "private", nil))
				e.bus.Publish(ctx, eventbus.NewEvent(eventbus.EventPostCreated, "private", "private", nil))
			}
			rec := e.serve(path)
			if rec.Code != expected {
				t.Fatal(mode, rec.Code, expected, rec.Body.String())
			}
			body := rec.Body.String()
			for _, secret := range []string{"private-", "subject", "extensions", "credential", "scope", "permission_code"} {
				if strings.Contains(body, secret) {
					t.Fatal("private metadata leaked", body)
				}
			}
			if mode == "filter" && (!strings.Contains(body, `"total":2`) || strings.Contains(body, "thread.created")) {
				t.Fatal("filter/count not applied before limit", body)
			}
			if expected == 200 && mode != "empty" && !strings.Contains(body, `"time"`) {
				t.Fatal("metadata missing", body)
			}
		})
	}
}

type eventMetadataAuthorityProbe struct {
	inner  identityport.AdminExecutionAuthorityReader
	calls  int
	before func(int)
}

func (p *eventMetadataAuthorityProbe) LoadAdminExecutionAuthority(ctx context.Context, q identityport.AdminExecutionQuery, proof identityport.SessionPrincipalResolver, lock bool) (identityport.AdminExecutionAuthority, error) {
	p.calls++
	p.before(p.calls)
	return p.inner.LoadAdminExecutionAuthority(ctx, q, proof, lock)
}
func TestEventMetadataHTTPWholePageAndRevocation(t *testing.T) {
	for _, mode := range []string{"permission_aba", "binding_revoke", "session_revoke", "session_version", "metadata", "empty_session", "empty_metadata"} {
		t.Run(mode, func(t *testing.T) {
			e := newEventMetadataHTTP(t, "current")
			ctx := context.Background()
			if !strings.HasPrefix(mode, "empty") {
				e.bus.Publish(ctx, eventbus.NewEvent(eventbus.EventThreadCreated, "private", "private", map[string]string{"content": "private-body"}))
			}
			probe := &eventMetadataAuthorityProbe{inner: e.authority, before: func(call int) {
				if call != 2 {
					return
				}
				switch mode {
				case "permission_aba":
					e.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "events-test")
					e.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"platform.reliability.read"}, "events-test")
				case "binding_revoke":
					e.admins.RevokeBindingCAS(ctx, "binding.events", 1, time.Now())
				case "session_revoke", "empty_session":
					e.sessions.RevokeSessionCAS(ctx, "adms.events", 1, "test", time.Now())
				case "session_version":
					e.sessions.UpgradeSessionStrengthCAS(ctx, "adms.events", 1, "mfa", time.Now(), time.Now())
				case "metadata", "empty_metadata":
					e.bus.Publish(ctx, eventbus.NewEvent(eventbus.EventPostDeleted, "private", "private", nil))
				}
			}}
			e.h.SetAdminAuthority(e.admin, probe)
			rec := e.serve("/events?limit=20")
			want := 409
			if mode == "session_revoke" || mode == "empty_session" {
				want = 401
			}
			if rec.Code != want || strings.Contains(rec.Body.String(), `"items"`) || strings.Contains(rec.Body.String(), `"total"`) || strings.Contains(rec.Body.String(), "private-body") {
				t.Fatal(mode, rec.Code, want, rec.Body.String())
			}
		})
	}
}
