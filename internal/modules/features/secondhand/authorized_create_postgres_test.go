package secondhand

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	repo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	core "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func typedCreatePostgres(t *testing.T, owner string) (*Service, *core.ThreadService, *pgxpool.Pool, identityport.SessionPrincipalResolver, *auth.JWTClaims) {
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
		t.Fatal("typed author writes require owned campusos_v12_02b_author_write database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	users := identityrepo.NewPgUserRepository(pool)
	u := &identitydomain.User{ID: owner, Username: "typed-writer-" + owner, Email: "typed-writer-" + owner + "@example.test", Status: "active", AuthVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(114110,'Typed writer group','typed-writer-group','group','active') ON CONFLICT(id) DO NOTHING;
 INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(114111,'Typed writer board','typed-writer-board',114110,'board','active') ON CONFLICT(id) DO NOTHING;
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(114111,'mutual_aid',TRUE),(114111,'secondhand',TRUE) ON CONFLICT(category_id,thread_type) DO NOTHING;`)
	if err != nil {
		t.Fatal(err)
	}
	threads := repo.NewPgThreadRepository(pool)
	threadService := core.NewThreadService(threads, nil)
	threadService.SetCategoryRepository(repo.NewPgCategoryRepository(pool))
	threadService.SetThreadTypePolicyRepository(repo.NewPgThreadTypePolicyRepository(pool))
	threadService.SetGovernanceRepository(repo.NewPgContentGovernanceRepository(pool))
	durable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	threadService.SetReliability(durable)
	s := NewService(NewPgStore(pool), community.NewContentGateway(threads, threadService), community.NewContentQuery(threadService))
	s.SetReliability(durable)
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "typed-create-pg-test-secret", Issuer: "typed-create-pg", Audience: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour})
	session, err := identityservice.NewSessionService(identityrepo.NewPgSessionRepository(pool), users, jwt, identityservice.SessionConfig{IPHashSecret: "typed-create-pg-test-ip-secret"})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := session.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Typed Writer"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, session, session, claims)
	}
	return s, threadService, pool, proof, claims
}
func typedCreateCounts(t *testing.T, pool *pgxpool.Pool, owner string) []int {
	t.Helper()
	ctx := context.Background()
	out := make([]int, 5)
	for i, q := range []string{`SELECT count(*) FROM threads WHERE author_id=$1`, `SELECT count(*) FROM secondhand_details WHERE created_by=$1`, `SELECT count(*) FROM content_revisions WHERE created_by=$1`, `SELECT count(*) FROM platform_command_audits WHERE actor_id=$1`, `SELECT count(*) FROM platform_outbox WHERE aggregate_id IN(SELECT id::text FROM threads WHERE author_id=$1)`} {
		if err := pool.QueryRow(ctx, q, owner).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}
func TestSecondhandAuthorCreatePostgreSQLAtomicAndLocks(t *testing.T) {
	ctx := context.Background()
	owner := "114401"
	s, threadService, pool, proof, claims := typedCreatePostgres(t, owner)
	s.store = &createDriftStore{Store: s.store, afterCreate: func(ctx context.Context, d *Detail) {
		for _, q := range []struct {
			sql string
			id  string
		}{{`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, owner}, {`SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT`, claims.SessionID}, {`SELECT id FROM categories WHERE id=$1 FOR UPDATE NOWAIT`, "114111"}, {`SELECT id FROM categories WHERE id=$1 FOR UPDATE NOWAIT`, "114110"}, {`SELECT category_id FROM category_thread_type_policies WHERE category_id=$1 AND thread_type='secondhand' FOR UPDATE NOWAIT`, "114111"}} {
			_, err := pool.Exec(context.Background(), q.sql, q.id)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "55P03" {
				t.Fatal("authority row not held in transaction", err)
			}
		}
	}}
	req := validCreateRequest()
	req.CategoryID = "114111"
	req.ContentFormat = ContentFormatSafeHTML
	req.Content = `<p>typed body</p><script>bad()</script>`
	result, err := s.CreateForPrincipal(ctx, "Writer", req, proof)
	if err != nil || result == nil || result.Detail.CreatedBy != owner || result.Detail.Version != 1 || strings.Contains(result.Thread.Content, "script") {
		t.Fatal(result, err)
	}
	var actor, kind, code string
	if err := pool.QueryRow(ctx, `SELECT actor_id,actor_type,command_code FROM platform_command_audits WHERE resource_id=$1`, result.Thread.ID).Scan(&actor, &kind, &code); err != nil || actor != owner || kind != "user" || code != "feature.secondhand.create" {
		t.Fatal(actor, kind, code, err)
	}
	before := typedCreateCounts(t, pool, owner)
	durable := reliability.NewService(transaction.NewPostgreSQL(pool), createFailAudit{reliability.NewPostgreSQLStore(pool)})
	threadService.SetReliability(durable)
	s.SetReliability(durable)
	if result, err := s.CreateForPrincipal(ctx, "Writer", req, proof); err == nil || result != nil {
		t.Fatal("required audit failure accepted")
	}
	after := typedCreateCounts(t, pool, owner)
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("partial aggregate/audit/outbox survived", before, after)
		}
	}
	// Actual typed detail drift within the command must also roll back the base.
	durable = reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	threadService.SetReliability(durable)
	s.SetReliability(durable)
	s.store = &createDriftStore{Store: NewPgStore(pool), afterCreate: func(ctx context.Context, d *Detail) {
		_, err := transaction.ExecutorFor(ctx, pool).Exec(ctx, `UPDATE secondhand_details SET location_scope='changed',version=version+1 WHERE thread_id=$1`, d.ThreadID)
		if err != nil {
			t.Fatal(err)
		}
	}}
	if result, err := s.CreateForPrincipal(ctx, "Writer", req, proof); err == nil || result != nil {
		t.Fatal("typed drift accepted")
	}
	after = typedCreateCounts(t, pool, owner)
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("typed drift persisted", before, after)
		}
	}
}
func TestSecondhandAuthorCreatePostgreSQLCommittedChanges(t *testing.T) {
	for i, mode := range []string{"board", "group", "type", "session", "auth-version"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			owner := fmt.Sprint(114411 + i)
			s, _, pool, base, claims := typedCreatePostgres(t, owner)
			before := typedCreateCounts(t, pool, owner)
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 3 {
					var err error
					switch mode {
					case "board":
						_, err = pool.Exec(ctx, `UPDATE categories SET is_closed=TRUE,version=version+1 WHERE id=114111`)
					case "group":
						_, err = pool.Exec(ctx, `UPDATE categories SET version=version+1 WHERE id=114110`)
					case "type":
						_, err = pool.Exec(ctx, `UPDATE category_thread_type_policies SET enabled=FALSE,updated_at=now() WHERE category_id=114111 AND thread_type='secondhand'`)
					case "session":
						_, err = pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, claims.SessionID)
					case "auth-version":
						_, err = pool.Exec(ctx, `UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, owner)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				return base(ctx)
			}
			req := validCreateRequest()
			req.CategoryID = "114111"
			result, err := s.CreateForPrincipal(ctx, "Writer", req, proof)
			if err == nil || result != nil {
				t.Fatal("committed stale authority accepted", result, err)
			}
			after := typedCreateCounts(t, pool, owner)
			for i := range before {
				if before[i] != after[i] {
					t.Fatal("partial create survived", before, after)
				}
			}
			if _, err := pool.Exec(ctx, `UPDATE categories SET is_closed=FALSE,version=version+1 WHERE id=114111; UPDATE category_thread_type_policies SET enabled=TRUE,updated_at=now() WHERE category_id=114111 AND thread_type='secondhand'`); err != nil {
				t.Fatal(err)
			}
		})
	}
}
