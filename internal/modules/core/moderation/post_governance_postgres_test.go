package moderation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

type failingPgPostModuleAudit struct{ *PgAuditStore }

func (a failingPgPostModuleAudit) Log(ctx context.Context, r AuditRecord) error {
	if err := a.PgAuditStore.Log(ctx, r); err != nil {
		return err
	}
	return errors.New("mandatory PostgreSQL module audit failure")
}
func TestPostGovernanceModerationPostgreSQLAuditTransaction(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_author_write" {
		t.Fatal("module audit requires owned test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	users := identityrepo.NewPgUserRepository(pool)
	for _, id := range []string{"125201", "125202"} {
		if err := users.Create(ctx, &identitydomain.User{ID: id, Username: "post-mod-" + id, Nickname: "Module user", Email: "post-mod-" + id + "@example.test", Status: "active", AuthVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "post-module-pg-test-key", Issuer: "post-module-pg", Audience: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour})
	sessions, err := identityservice.NewSessionService(identityrepo.NewPgSessionRepository(pool), users, jwt, identityservice.SessionConfig{IPHashSecret: "post-module-pg-ip-key"})
	if err != nil {
		t.Fatal(err)
	}
	moderator, err := users.GetByID(ctx, "125202")
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := sessions.Issue(ctx, moderator, identityservice.SessionMetadata{DeviceName: "Module moderator"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, sessions, sessions, claims)
	}
	grants := identityrepo.NewPgDelegationRepository(pool)
	now := time.Now().UTC()
	if err := grants.InsertDelegations(ctx, []identityrepo.Delegation{{ID: "grant.post-module-pg", Kind: "grant", SubjectKind: "user", SubjectID: claims.UserID, Action: "community.post.delete", BoardID: "119111", NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	threads := communityrepo.NewPgThreadRepository(pool)
	posts := communityrepo.NewPgPostRepository(pool)
	corePosts := communityservice.NewPostService(posts, nil)
	corePosts.SetThreadRepository(threads)
	corePosts.SetGovernanceAuthority(identityservice.NewBoardGovernanceAuthorityService(grants, nil, nil))
	corePosts.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	audit := NewPgAuditStore(pool)
	module := NewService(nil, community.NewModerationGateway(communityrepo.NewPgCategoryRepository(pool), threads, posts, communityservice.NewThreadService(threads, nil), corePosts), audit, Config{AllowDeletePost: true})
	for i, mode := range []string{"success", "audit_failure"} {
		t.Run(mode, func(t *testing.T) {
			thread := &communitydomain.Thread{ID: fmt.Sprint(125301 + i), AuthorID: "125201", CategoryID: "119111", Title: "module private title", Content: "module private body", PublicationStatus: "private", ModerationStatus: "clear", DeletionStatus: "active", ReplyCount: 1, CreatedAt: now, UpdatedAt: now}
			if err := threads.Create(ctx, thread); err != nil {
				t.Fatal(err)
			}
			post := &communitydomain.Post{ID: fmt.Sprint(125401 + i), ThreadID: thread.ID, AuthorID: "125201", Content: "private reply", Status: "published", CreatedAt: now, UpdatedAt: now}
			if err := posts.Create(ctx, post); err != nil {
				t.Fatal(err)
			}
			if err := threads.IncrementReplyCount(ctx, thread.ID, 1); err != nil {
				t.Fatal(err)
			}
			module.audit = audit
			if mode == "audit_failure" {
				module.audit = failingPgPostModuleAudit{audit}
			}
			err := module.DeletePostForPrincipal(ctx, thread.ID, post.ID, proof, OperationContext{TraceID: "post-module-pg"})
			if mode == "success" && err != nil || mode == "audit_failure" && err == nil {
				t.Fatal(mode, err)
			}
			var moduleAudits, platformAudits, outbox, replies int
			for _, q := range []struct {
				sql  string
				dest *int
				id   string
			}{{`SELECT count(*) FROM audit_logs WHERE resource='post' AND resource_id=$1`, &moduleAudits, post.ID}, {`SELECT count(*) FROM platform_command_audits WHERE resource_type='post' AND resource_id=$1`, &platformAudits, post.ID}, {`SELECT count(*) FROM platform_outbox WHERE aggregate_type='post' AND aggregate_id=$1`, &outbox, post.ID}, {`SELECT reply_count FROM threads WHERE id=$1`, &replies, thread.ID}} {
				if err := pool.QueryRow(ctx, q.sql, q.id).Scan(q.dest); err != nil {
					t.Fatal(err)
				}
			}
			actual, err := posts.ReadPostCommandSnapshot(ctx, post.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "success" {
				if moduleAudits != 1 || platformAudits != 1 || outbox != 1 || replies != 0 || actual.DeletedActorKind != "user" || actual.DeletedActorID != claims.UserID || actual.DeletedAt == nil {
					t.Fatal(moduleAudits, platformAudits, outbox, replies, actual)
				}
			} else {
				if moduleAudits != 0 || platformAudits != 0 || outbox != 0 || replies != 1 || actual.DeletedAt != nil {
					t.Fatal("module audit escaped command transaction", moduleAudits, platformAudits, outbox, replies, actual)
				}
			}
		})
	}
}
