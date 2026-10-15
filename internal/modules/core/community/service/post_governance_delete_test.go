package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

func postGovernanceFixture(t *testing.T, kind string) (*PostService, *ThreadService, *repository.MemoryPostRepository, *reliability.MemoryStore, *identityrepo.MemoryDelegationRepository, identityport.SessionPrincipalResolver) {
	t.Helper()
	s, threads, posts, store := postAuthorFixture(t)
	ctx := context.Background()
	row := &domain.Post{ID: "post.governance", ThreadID: "190101", AuthorID: "190102", Content: "private reply", Status: "published"}
	if err := posts.Create(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := threads.repo.IncrementReplyCount(ctx, "190101", 1); err != nil {
		t.Fatal(err)
	}
	grants := identityrepo.NewMemoryDelegationRepository()
	proof := authorUpdateProof("190104")
	now := time.Now().UTC()
	var admins *identityrepo.MemoryAdminDomainRepository
	var admin *identityservice.AdminSessionService
	if kind == "admin" {
		admins = identityrepo.NewMemoryAdminDomainRepository()
		sessions := identityrepo.NewMemoryAdminSessionRepository()
		id := "190102"
		if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: id, Username: "post-gov-admin", Email: "post-gov-admin@example.test"}); err != nil {
			t.Fatal(err)
		}
		at := now.Add(-time.Minute)
		if err := sessions.CreateSession(ctx, identityrepo.AdminSession{ID: "adms.post-gov", AccountID: id, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: "adms.post-gov", AuthenticationStrength: "mfa", MFAAuthenticatedAt: &at, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "post-gov-native-test-key", Issuer: "post-gov-native", Audience: "admin", AccessTTL: time.Hour})
		admin = identityservice.NewAdminSessionService(admins, sessions, jwt)
		token, err := jwt.GenerateAccessToken(id, "Admin", auth.AccessTokenContext{SessionID: "adms.post-gov", AuthVersion: 1})
		if err != nil {
			t.Fatal(err)
		}
		claims, err := jwt.VerifyAccessToken(token)
		if err != nil {
			t.Fatal(err)
		}
		proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
			return identityport.ResolveAdminSessionPrincipal(ctx, admin, claims)
		}
		if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.post-gov", AccountID: id, RoleID: "security_admin", GrantedBy: id}); err != nil {
			t.Fatal(err)
		}
		if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.post.delete"}, "test"); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := grants.InsertDelegations(ctx, []identityrepo.Delegation{{ID: "grant.post-gov", Kind: "grant", SubjectKind: "user", SubjectID: "190104", Action: "community.post.delete", BoardID: "190103", NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	s.SetGovernanceAuthority(identityservice.NewBoardGovernanceAuthorityService(grants, admins, admin))
	return s, threads, posts, store, grants, proof
}

func TestPostGovernanceDeleteCurrentUserAndAdminDomain(t *testing.T) {
	for _, kind := range []string{"user", "admin"} {
		t.Run(kind, func(t *testing.T) {
			s, threads, posts, store, _, proof := postGovernanceFixture(t, kind)
			ctx := context.Background()
			p, err := proof(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.GovernanceDeleteForPrincipal(ctx, "190101", "post.governance", proof, nil); err != nil {
				t.Fatal(err)
			}
			deleted, err := posts.ReadPostCommandSnapshot(ctx, "post.governance")
			if err != nil || deleted.DeletedAt == nil || deleted.DeletedActorKind != kind || deleted.DeletedActorID != p.Actor.ID || deleted.AuthorID != "190102" || deleted.Content != "private reply" {
				t.Fatal(deleted, err)
			}
			parent, _ := threads.repo.GetByID(ctx, "190101")
			if parent.ReplyCount != 0 {
				t.Fatal(parent)
			}
			audit, total, err := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
			if err != nil || total != 1 || audit[0].ActorType != kind || audit[0].ActorID != p.Actor.ID {
				t.Fatal(audit, total, err)
			}
		})
	}
}

type postGovernanceFailParticipant struct{}

func (postGovernanceFailParticipant) CheckPostGovernance(context.Context) error { return nil }
func (postGovernanceFailParticipant) RecordPostGovernance(context.Context, identityport.PrincipalContext, *domain.Thread, *domain.Post) error {
	return errors.New("mandatory module audit failure")
}

func TestPostGovernanceDeleteRevocationAndRequiredAtomicity(t *testing.T) {
	for _, mode := range []string{"grant_revoke", "credential", "parent_aba", "post_aba", "audit", "participant", "wrong_parent", "owner_without_grant"} {
		t.Run(mode, func(t *testing.T) {
			s, threads, posts, store, grants, proof := postGovernanceFixture(t, "user")
			ctx := context.Background()
			before := posts.Snapshot()
			parentID := "190101"
			var participant interface {
				CheckPostGovernance(context.Context) error
				RecordPostGovernance(context.Context, identityport.PrincipalContext, *domain.Thread, *domain.Post) error
			}
			if mode == "audit" {
				s.SetReliability(reliability.NewService(transaction.NewMemory(posts, threads.repo.(transaction.Snapshotter)), authorFailAudit{Store: store}))
			}
			if mode == "participant" {
				participant = postGovernanceFailParticipant{}
			}
			if mode == "wrong_parent" {
				parentID = "other-thread"
			}
			if mode == "owner_without_grant" {
				proof = authorUpdateProof("190102")
			}
			calls := 0
			changing := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 3 {
					switch mode {
					case "grant_revoke":
						grants.SetStatusCAS(ctx, "grant.post-gov", 1, "revoked", time.Now())
					case "parent_aba":
						row, _ := threads.repo.GetByID(ctx, "190101")
						old := row.Content
						row.Content = "changed"
						threads.repo.Update(ctx, row)
						row.Content = old
						threads.repo.Update(ctx, row)
					case "post_aba":
						row, _ := posts.GetByID(ctx, "post.governance")
						old := row.Content
						row.Content = "changed"
						posts.Update(ctx, row)
						row.Content = old
						posts.Update(ctx, row)
					}
				}
				if mode == "credential" && calls >= 3 {
					return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
				}
				return proof(ctx)
			}
			err := s.GovernanceDeleteForPrincipal(ctx, parentID, "post.governance", changing, participant)
			if err == nil {
				t.Fatal("failed governance command accepted")
			}
			if !reflect.DeepEqual(before, posts.Snapshot()) {
				t.Fatal("failed governance mutated Post")
			}
			parent, _ := threads.repo.GetByID(ctx, "190101")
			if parent.ReplyCount != 1 {
				t.Fatal("failed governance changed counter", parent)
			}
		})
	}
}
