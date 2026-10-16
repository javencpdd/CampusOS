package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func authorPostgres(t *testing.T, owner string) (*ThreadService, *repository.PgThreadRepository, *pgxpool.Pool, *domain.Thread, *identityservice.SessionService, *auth.JWTClaims) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_author_write" {
		t.Fatal("author write tests require owned campusos_v12_02b_author_write")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	users := identityrepo.NewPgUserRepository(pool)
	user := &identitydomain.User{ID: owner, Username: "writer-" + owner, Email: "writer-" + owner + "@example.test", Status: "active", AuthVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(113110,'Writer group','writer-group','group','active') ON CONFLICT(id) DO NOTHING;
 INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(113111,'Writer board','writer-board',113110,'board','active'),(113112,'Other board','writer-other',113110,'board','active') ON CONFLICT(id) DO NOTHING;
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(113111,'discussion',TRUE),(113112,'discussion',TRUE) ON CONFLICT(category_id,thread_type) DO NOTHING;`)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewPgThreadRepository(pool)
	row := &domain.Thread{ID: fmt.Sprint(idgen.New()), AuthorID: owner, AuthorName: "Writer", CategoryID: "113111", Title: "before", Content: "body", PublicationStatus: "private", ModerationStatus: "clear", DeletionStatus: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatal(err)
	}
	store := reliability.NewPostgreSQLStore(pool)
	durable := reliability.NewService(transaction.NewPostgreSQL(pool), store)
	s := NewThreadService(repo, nil)
	s.SetGovernanceRepository(repository.NewPgContentGovernanceRepository(pool))
	s.SetReliability(durable)
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "author-write-pg-test-secret", Issuer: "author-write", Audience: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour})
	session, err := identityservice.NewSessionService(identityrepo.NewPgSessionRepository(pool), users, jwt, identityservice.SessionConfig{IPHashSecret: "author-write-pg-ip-secret"})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := session.Issue(ctx, user, identityservice.SessionMetadata{DeviceName: "Writer"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return s, repo, pool, row, session, claims
}
func authorPgProof(session *identityservice.SessionService, claims *auth.JWTClaims) identityport.SessionPrincipalResolver {
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, session, session, claims)
	}
}

func TestAuthorUpdatePostgreSQLCASAndAuditRollback(t *testing.T) {
	ctx := context.Background()
	s, repo, pool, row, session, claims := authorPostgres(t, "113201")
	resolve := authorPgProof(session, claims)
	title := "committed author edit"
	result, err := s.UpdateThreadForPrincipal(ctx, row.ID, domain.UpdateThreadRequest{Title: &title}, resolve)
	if err != nil || result.Title != title {
		t.Fatal(result, err)
	}
	var actor, kind string
	var revisions int
	if err := pool.QueryRow(ctx, `SELECT actor_id,actor_type FROM platform_command_audits WHERE resource_id=$1 AND command_code='community.thread.author_update'`, row.ID).Scan(&actor, &kind); err != nil || actor != claims.UserID || kind != "user" {
		t.Fatal(actor, kind, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM content_revisions WHERE thread_id=$1`, row.ID).Scan(&revisions); err != nil || revisions != 1 {
		t.Fatal(revisions, err)
	}
	base, _ := repo.GetByID(ctx, row.ID)
	failStore := authorFailAudit{Store: reliability.NewPostgreSQLStore(pool)}
	s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), failStore))
	next := "failed audit edit"
	if result, err := s.UpdateThreadForPrincipal(ctx, row.ID, domain.UpdateThreadRequest{Title: &next}, resolve); err == nil || result != nil {
		t.Fatal("missing required audit accepted")
	}
	actual, _ := repo.GetByID(ctx, row.ID)
	if actual.Title != base.Title || actual.FactsVersion != base.FactsVersion || actual.CurrentRevision != base.CurrentRevision {
		t.Fatal("partial PG write survived audit failure")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM content_revisions WHERE thread_id=$1`, row.ID).Scan(&revisions); err != nil || revisions != 1 {
		t.Fatal("partial revision", revisions, err)
	}
}

func TestAuthorUpdatePostgreSQLCommittedAuthorityChanges(t *testing.T) {
	for i, mode := range []string{"board", "state", "body-ABA", "session", "auth-version", "counters"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, repo, pool, row, session, claims := authorPostgres(t, fmt.Sprint(113210+i))
			calls := 0
			base := authorPgProof(session, claims)
			resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 2 {
					var err error
					switch mode {
					case "board":
						_, err = pool.Exec(ctx, `UPDATE threads SET category_id=113112 WHERE id=$1`, row.ID)
					case "state":
						_, err = pool.Exec(ctx, `UPDATE threads SET deletion_status='trashed' WHERE id=$1`, row.ID)
					case "body-ABA":
						tx, e := pool.Begin(ctx)
						if e != nil {
							t.Fatal(e)
						}
						_, err = tx.Exec(ctx, `UPDATE threads SET content='changed' WHERE id=$1`, row.ID)
						if err == nil {
							_, err = tx.Exec(ctx, `UPDATE threads SET content='body' WHERE id=$1`, row.ID)
						}
						if err == nil {
							err = tx.Commit(ctx)
						} else {
							tx.Rollback(ctx)
						}
					case "session":
						_, err = pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, claims.SessionID)
					case "auth-version":
						_, err = pool.Exec(ctx, `UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, claims.UserID)
					case "counters":
						_, err = pool.Exec(ctx, `UPDATE threads SET view_count=17,reply_count=3,like_count=2 WHERE id=$1`, row.ID)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				return base(ctx)
			}
			title := "new title"
			result, err := s.UpdateThreadForPrincipal(ctx, row.ID, domain.UpdateThreadRequest{Title: &title}, resolve)
			if mode == "counters" {
				if err != nil || result.ViewCount != 17 || result.ReplyCount != 3 || result.LikeCount != 2 {
					t.Fatal(result, err)
				}
				return
			}
			if err == nil || result != nil {
				t.Fatalf("stale %s accepted", mode)
			}
			actual, _ := repo.GetByID(ctx, row.ID)
			if actual.Title != "before" {
				t.Fatal("stale write persisted")
			}
		})
	}
}

func TestAuthorUpdatePostgreSQLCredentialLocks(t *testing.T) {
	ctx := context.Background()
	_, _, pool, _, session, claims := authorPostgres(t, "113220")
	if err := transaction.NewPostgreSQL(pool).Within(ctx, func(command context.Context) error {
		if err := session.VerifyAccess(command, claims); err != nil {
			return err
		}
		for _, test := range []struct{ query, id string }{{`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, claims.UserID}, {`SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT`, claims.SessionID}} {
			var id string
			err := pool.QueryRow(ctx, test.query, test.id).Scan(&id)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
				t.Fatalf("credential authority not locked: %s %v", test.query, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
