package richtext

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

type articleGovernanceFixtureData struct {
	s         *Service
	core      *communityservice.ThreadService
	threads   *communityrepo.MemoryThreadRepository
	gov       *communityrepo.MemoryContentGovernanceRepository
	events    *reliability.MemoryStore
	admins    *identityrepo.MemoryAdminDomainRepository
	sessions  *identityrepo.MemoryAdminSessionRepository
	current   *identityservice.AdminSessionService
	jwt       *auth.JWTManager
	claims    *auth.JWTClaims
	proof     identityport.SessionPrincipalResolver
	id, actor string
}

func articleGovernanceFixture(t *testing.T, actor, operation string) articleGovernanceFixtureData {
	t.Helper()
	ctx := context.Background()
	s, threads, _, _, events, core := articleCreateFixture(t)
	g := articleGovernanceFixtureData{s: s, threads: threads, events: events, core: core, actor: actor}
	g.gov = communityrepo.NewMemoryContentGovernanceRepository()
	core.SetGovernanceRepository(g.gov)
	draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
	if err != nil {
		t.Fatal(err)
	}
	g.id = draft.ThreadID
	if _, err := s.PublishForPrincipal(ctx, g.id, articleCreateProof); err != nil {
		t.Fatal(err)
	}
	if operation == "direct_restore" {
		row, _ := threads.GetByID(ctx, g.id)
		row.ModerationStatus = domain.ModerationStatusTakenDown
		if err := threads.Update(ctx, row); err != nil {
			t.Fatal(err)
		}
		// A Core governance transition may leave the old display mirror stale.
	}
	g.admins = identityrepo.NewMemoryAdminDomainRepository()
	g.sessions = identityrepo.NewMemoryAdminSessionRepository()
	if err := g.admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: actor, Username: "article-admin", Email: "article-admin@example.test"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	at := now.Add(-time.Minute)
	if err := g.sessions.CreateSession(ctx, identityrepo.AdminSession{ID: "adms.richtext", AccountID: actor, RefreshTokenDigest: strings.Repeat("b", 64), TokenFamilyID: "adms.richtext", AuthenticationStrength: "mfa", MFAAuthenticatedAt: &at, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	g.jwt = auth.NewJWTManager(auth.JWTConfig{Secret: "richtext-admin-unit-test-key", Issuer: "richtext-admin-test", Audience: "admin", AccessTTL: time.Hour})
	g.current = identityservice.NewAdminSessionService(g.admins, g.sessions, g.jwt)
	token, err := g.jwt.GenerateAccessToken(actor, "Admin", auth.AccessTokenContext{SessionID: "adms.richtext", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	g.claims, err = g.jwt.VerifyAccessToken(token)
	if err != nil {
		t.Fatal(err)
	}
	g.proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, g.current, g.claims)
	}
	if err := g.admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.article", AccountID: actor, RoleID: "security_admin", GrantedBy: actor}); err != nil {
		t.Fatal(err)
	}
	if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.take_down", "community.thread.direct_restore", "community.thread.trash"}, "article-test"); err != nil {
		t.Fatal(err)
	}
	source := identityservice.NewBoardGovernanceAuthorityService(nil, g.admins, g.current)
	core.SetGovernanceAuthority(source)
	core.SetAdminExecutionAuthority(source)
	return g
}

type articleGovernanceHookStore struct {
	Store
	afterUpdate func(context.Context, *Article) error
	afterLock   func(context.Context, *Article) error
}

func (s *articleGovernanceHookStore) LockArticleAuthority(ctx context.Context, id string) (*Article, error) {
	a, err := s.Store.(ArticleAuthorityStore).LockArticleAuthority(ctx, id)
	if err == nil && s.afterLock != nil {
		err = s.afterLock(ctx, a)
	}
	return a, err
}
func (s *articleGovernanceHookStore) UpdateArticleIfFacts(ctx context.Context, a *Article, version int64) error {
	if err := s.Store.(ArticleAuthorityStore).UpdateArticleIfFacts(ctx, a, version); err != nil {
		return err
	}
	if s.afterUpdate != nil {
		return s.afterUpdate(ctx, a)
	}
	return nil
}

func TestRichTextGovernanceCurrentTypedActorsSafeResultAndPreservedBody(t *testing.T) {
	for _, actor := range []string{"1001", "adm.opaque.article"} {
		for _, op := range []string{"take_down", "direct_restore", "trash"} {
			t.Run(actor+"/"+op, func(t *testing.T) {
				g := articleGovernanceFixture(t, actor, op)
				ctx := context.Background()
				before, _ := g.s.store.GetArticleByThreadID(ctx, g.id)
				threadBefore, _ := g.threads.GetByID(ctx, g.id)
				result, err := g.s.AdminGovernanceForPrincipal(ctx, g.id, op, "actual article governance", g.proof)
				if err != nil || result == nil {
					t.Fatal(result, err)
				}
				after, _ := g.s.store.GetArticleByThreadID(ctx, g.id)
				threadAfter, _ := g.threads.GetByID(ctx, g.id)
				want := map[string]string{"take_down": StatusOffline, "direct_restore": StatusPublished, "trash": StatusTrashed}[op]
				if result.ThreadID != g.id || result.Status != want || after.Status != want || after.FactsVersion <= before.FactsVersion || after.UpdatedBy != "" || after.UpdatedActorKind != "admin" || after.UpdatedActorID != actor {
					t.Fatal(result, after)
				}
				if after.ContentHTML != before.ContentHTML || !reflect.DeepEqual(canonicalAuthorArticle(after).ContentJSON, canonicalAuthorArticle(before).ContentJSON) || after.Title != before.Title || after.CreatedBy != before.CreatedBy || after.Summary != before.Summary || after.CoverURL != before.CoverURL || threadAfter.Content != threadBefore.Content || threadAfter.PublicationStatus != threadBefore.PublicationStatus {
					t.Fatal("immutable facts changed", after, threadAfter)
				}
				raw, _ := json.Marshal(result)
				var fields map[string]any
				json.Unmarshal(raw, &fields)
				if len(fields) != 2 || strings.Contains(string(raw), before.ContentHTML) || strings.Contains(string(raw), before.Title) {
					t.Fatal(string(raw))
				}
				actions, err := g.gov.ListModerationActions(ctx, g.id)
				if err != nil || len(actions) != 1 || actions[0].ActorKind != "admin" || actions[0].ActorRefID != actor || actions[0].ActorID != "" {
					t.Fatal(actions, err)
				}
			})
		}
	}
}

func TestRichTextGovernanceMissingAuthorityFactChangesAndAtomicRollback(t *testing.T) {
	for _, mode := range []string{"missing_proof", "user", "missing_command", "missing_store", "missing_audit", "permission", "password", "disabled", "wrong_creator", "locked_metadata", "locked_aba", "final_metadata", "final_core", "final_permission", "final_disabled", "article_write", "audit"} {
		t.Run(mode, func(t *testing.T) {
			g := articleGovernanceFixture(t, "adm.opaque.article", "take_down")
			ctx := context.Background()
			original := g.s.store
			proof := g.proof
			switch mode {
			case "missing_proof":
				proof = nil
			case "user":
				proof = articleCreateProof
			case "missing_command":
				g.s.community = articleRawGateway{g.s.community}
			case "missing_store":
				g.s.store = struct{ Store }{original}
			case "missing_audit":
				g.s.SetReliability(nil)
			case "permission":
				if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "test"); err != nil {
					t.Fatal(err)
				}
			case "password":
				row, _ := g.sessions.GetSessionByID(ctx, "adms.richtext")
				if _, ok, err := g.sessions.UpgradeSessionStrengthCAS(ctx, row.ID, row.Version, "password", time.Now(), time.Now()); err != nil || !ok {
					t.Fatal(ok, err)
				}
			case "disabled":
				g.s.SetEnabledChecker(func() bool { return false })
			case "wrong_creator":
				a, _ := original.GetArticleByThreadID(ctx, g.id)
				a.CreatedBy = "1002"
				if err := original.UpdateArticle(ctx, a); err != nil {
					t.Fatal(err)
				}
			}
			beforeThread, beforeArticle, beforeEvent := g.threads.Snapshot(), original.(transaction.Snapshotter).Snapshot(), g.events.Snapshot()
			beforeGov := g.gov.Snapshot()
			if mode == "audit" {
				d := reliability.NewService(transaction.NewMemory(g.threads, g.gov, original.(transaction.Snapshotter), g.events), articleCreateFailAudit{g.events})
				g.core.SetReliability(d)
				g.s.SetReliability(d)
			}
			calls := 0
			base := proof
			if mode == "locked_metadata" || mode == "locked_aba" {
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					calls++
					if calls == 2 {
						a, _ := original.GetArticleByThreadID(ctx, g.id)
						old := a.Summary
						a.Summary = "changed"
						if err := original.UpdateArticle(ctx, a); err != nil {
							t.Fatal(err)
						}
						if mode == "locked_aba" {
							a.Summary = old
							if err := original.UpdateArticle(ctx, a); err != nil {
								t.Fatal(err)
							}
						}
					}
					return base(ctx)
				}
			}
			g.s.store = &articleGovernanceHookStore{Store: g.s.store, afterUpdate: func(tx context.Context, a *Article) error {
				switch mode {
				case "article_write":
					return errors.New("article storage unavailable after write")
				case "final_metadata":
					current, _ := original.GetArticleByThreadID(tx, g.id)
					current.Summary = "unexpected"
					return original.UpdateArticle(tx, current)
				case "final_core":
					current, _ := g.threads.GetByID(tx, g.id)
					current.Content = "unexpected"
					return g.threads.Update(tx, current)
				case "final_disabled":
					g.s.SetEnabledChecker(func() bool { return false })
					return nil
				case "final_permission":
					return g.admins.ReplaceAdminRolePermissionFacts(tx, "security_admin", nil, "test")
				}
				return nil
			}}
			// Keep the missing-store test genuinely missing its authority port.
			if mode == "missing_store" {
				g.s.store = struct{ Store }{original}
			}
			result, err := g.s.AdminGovernanceForPrincipal(ctx, g.id, "take_down", "actual governance", proof)
			if err == nil || result != nil || !reflect.DeepEqual(beforeThread, g.threads.Snapshot()) || !reflect.DeepEqual(beforeEvent, g.events.Snapshot()) || !reflect.DeepEqual(beforeGov, g.gov.Snapshot()) {
				t.Fatal("partial aggregate accepted", result, err)
			}
			if mode != "locked_metadata" && mode != "locked_aba" && !reflect.DeepEqual(beforeArticle, original.(transaction.Snapshotter).Snapshot()) {
				t.Fatal("partial article persisted", err)
			}
		})
	}
}

var _ communityport.ThreadGovernanceParticipant = (*articleGovernancePart)(nil)
