package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type checkedThreadFlagFixture struct {
	checkedPostModerationFixture
	roles *identityrepo.MemoryRoleRepository
	core  *communityservice.ThreadService
	gov   *communityrepo.MemoryContentGovernanceRepository
}

func newCheckedThreadFlag(t *testing.T) checkedThreadFlagFixture {
	t.Helper()
	e := newCheckedPostModeration(t)
	ctx := context.Background()
	roles := identityrepo.NewMemoryRoleRepository()
	board := int64(130103)
	if _, err := roles.AssignRole(ctx, "130104", 2, "category", &board); err != nil {
		t.Fatal(err)
	}
	if err := roles.ReplaceRolePermissions(ctx, 2, []string{"community.thread.pin", "community.thread.lock"}, "test"); err != nil {
		t.Fatal(err)
	}
	gov := communityrepo.NewMemoryContentGovernanceRepository()
	core := communityservice.NewThreadService(e.threads, nil)
	core.SetGovernanceRepository(gov)
	core.SetUserScopedExecutionAuthority(identityservice.NewUserScopedExecutionAuthorityService(roles, e.identity, identityrepo.NewMemoryAdminAccountRepository()))
	core.SetReliability(reliability.NewService(transaction.NewMemory(e.threads, gov), e.store))
	e.service = NewService(nil, community.NewModerationGateway(communityrepo.NewMemoryCategoryRepository(), e.threads, e.posts, core, communityservice.NewPostService(e.posts, nil)), e.audit, Config{AllowPin: true, AllowLock: true})
	return checkedThreadFlagFixture{e, roles, core, gov}
}

func TestThreadUserFlagModerationHTTPCurrentJWTAndSafeResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, operation := range []string{"pin", "unpin", "lock", "unlock"} {
		for _, mode := range []string{"current", "global", "missing_verifier", "missing_strength", "no_token", "revoked", "no_role", "cross_scope", "no_code", "disabled", "missing_source", "module_audit", "trashed", "body", "chunked_body", "admin_token"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				e := newCheckedThreadFlag(t)
				ctx := context.Background()
				h := NewHandler(e.service)
				if mode != "missing_verifier" {
					h.SetUserSessionVerifier(e.identity)
				}
				if mode != "missing_strength" {
					h.SetSessionStrengthReader(e.identity)
				}
				want := 200
				switch mode {
				case "global":
					e.roles.RevokeRole(ctx, "130104", 2)
					e.roles.AssignRole(ctx, "130104", 2, "global", nil)
				case "missing_verifier", "missing_strength", "no_token", "admin_token":
					want = 401
				case "revoked":
					e.sessions.RevokeByUser(ctx, "130104", "test", time.Now())
					want = 401
				case "no_role":
					e.roles.RevokeRole(ctx, "130104", 2)
					want = 403
				case "cross_scope":
					e.roles.ReplaceRoleScopes(ctx, "130104", 2, "category", []int64{130106})
					want = 403
				case "no_code":
					e.roles.ReplaceRolePermissions(ctx, 2, nil, "test")
					want = 403
				case "disabled":
					e.service.config = Config{}
					want = 403
				case "missing_source":
					e.core.SetUserScopedExecutionAuthority(nil)
					want = 503
				case "module_audit":
					e.service.audit = failingPostModuleAudit{e.audit}
					want = 503
				case "trashed":
					row, _ := e.threads.GetByID(ctx, "130101")
					row.DeletionStatus = "trashed"
					e.threads.Update(ctx, row)
					want = 403
				case "body", "chunked_body":
					want = 400
				}
				r := gin.New()
				fn := h.Pin
				switch operation {
				case "unpin":
					fn = h.Unpin
				case "lock":
					fn = h.Lock
				case "unlock":
					fn = h.Unlock
				}
				r.POST("/moderation/threads/:id/"+operation, middleware.JWTAuth(e.jwt, e.identity), fn)
				body := ""
				if mode == "body" || mode == "chunked_body" {
					body = `{"actor_id":"130102","scope_id":"130106"}`
				}
				req := httptest.NewRequest("POST", "/moderation/threads/130101/"+operation+"?actor_id=130102&actor_kind=admin&scope_id=130106", strings.NewReader(body))
				if mode == "chunked_body" {
					req.ContentLength = -1
				}
				token := e.token
				if mode == "admin_token" {
					adminJWT := auth.NewJWTManager(auth.JWTConfig{Secret: "checked-post-moderation-test-key", Issuer: "checked-post-moderation", Audience: "admin", AccessTTL: time.Hour})
					token, _ = adminJWT.GenerateAccessToken("130104", "Admin", auth.AccessTokenContext{SessionID: "admin.session", AuthVersion: 1})
				}
				if mode != "no_token" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				req.Header.Set("X-Actor-ID", "130102")
				before, _ := e.threads.GetByID(ctx, "130101")
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				if rec.Code != want || strings.Contains(rec.Body.String(), "private") {
					t.Fatal(rec.Code, want, rec.Body.String())
				}
				after, _ := e.threads.GetByID(ctx, "130101")
				if want == 200 {
					var envelope struct{ Data map[string]json.RawMessage }
					if json.Unmarshal(rec.Body.Bytes(), &envelope) != nil || len(envelope.Data) != 3 || len(e.audit.Records()) != 1 || e.audit.Records()[0].ActorID != "130104" || after.CurrentRevision != before.CurrentRevision+1 {
						t.Fatal("missing current User audit or unsafe ack", rec.Body.String(), e.audit.Records(), after)
					}
				} else if after.CurrentRevision != before.CurrentRevision || after.FactsVersion != before.FactsVersion || len(e.audit.Records()) != 0 {
					t.Fatal("denied HTTP wrote resource or audit", before, after, e.audit.Records())
				}
			})
		}
	}
}

type threadFlagHookAudit struct {
	*MemoryAuditStore
	hook func(context.Context) error
}

func (a threadFlagHookAudit) Log(ctx context.Context, record AuditRecord) error {
	if err := a.MemoryAuditStore.Log(ctx, record); err != nil {
		return err
	}
	return a.hook(ctx)
}

func TestThreadUserFlagModerationRequiredAuditAndCurrentConfigRollback(t *testing.T) {
	for _, operation := range []string{"pin", "unpin", "lock", "unlock"} {
		for _, mode := range []string{"audit", "config", "permission"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				e := newCheckedThreadFlag(t)
				ctx := context.Background()
				claims, _ := e.jwt.VerifyAccessToken(e.token)
				proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
					return identityport.ResolveUserSessionPrincipal(ctx, e.identity, e.identity, claims)
				}
				e.service.audit = threadFlagHookAudit{e.audit, func(ctx context.Context) error {
					switch mode {
					case "audit":
						return errors.New("required audit rejected after insert")
					case "config":
						e.service.config = Config{}
					case "permission":
						return e.roles.ReplaceRolePermissions(ctx, 2, nil, "test")
					}
					return nil
				}}
				before, _ := e.threads.GetByID(ctx, "130101")
				result, err := e.service.SetThreadFlagForPrincipal(ctx, before.ID, operation, proof, OperationContext{})
				if err == nil || result != nil || mode == "config" && !errors.Is(err, ErrActionDisabled) {
					t.Fatal("mandatory/current module facts failure accepted", result, err)
				}
				after, _ := e.threads.GetByID(ctx, before.ID)
				actions, _ := e.gov.ListModerationActions(ctx, before.ID)
				if before.CurrentRevision != after.CurrentRevision || before.FactsVersion != after.FactsVersion || len(actions) != 0 || len(e.audit.Records()) != 0 {
					t.Fatal("failed module participant retained write", after, actions, e.audit.Records())
				}
			})
		}
	}
}
