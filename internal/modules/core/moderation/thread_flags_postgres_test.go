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

type threadFlagPgHookAudit struct {
	*PgAuditStore
	hook func(context.Context) error
}

func (a threadFlagPgHookAudit) Log(ctx context.Context, record AuditRecord) error {
	if err := a.PgAuditStore.Log(ctx, record); err != nil {
		return err
	}
	return a.hook(ctx)
}

func TestThreadUserFlagModerationPostgreSQLRequiredAuditAndConfigTransaction(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_author_write" {
		t.Fatal("User flag module audit requires owned database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	users := identityrepo.NewPgUserRepository(pool)
	for _, id := range []string{"154201", "154202"} {
		if err := users.Create(ctx, &identitydomain.User{ID: id, Username: "flag-module-" + id, Nickname: "Flag module", Email: "flag-module-" + id + "@example.test", Status: "active", AuthVersion: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(154110,'Flag module group','flag-module-group','group','active');
INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(154111,'Flag module board','flag-module-board',154110,'board','active');
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(154111,'discussion',TRUE);`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO roles(id,name,description,is_system) VALUES(154203,'flag_module','Module User flag',FALSE);
INSERT INTO role_permissions(id,role_id,permission_id,created_by) SELECT 154204,154203,id,'flag-module' FROM permission_definitions WHERE code='community.thread.pin';
INSERT INTO role_permissions(id,role_id,permission_id,created_by) SELECT 154205,154203,id,'flag-module' FROM permission_definitions WHERE code='community.thread.lock';
INSERT INTO user_roles(id,user_id,role_id,scope_type,scope_id) VALUES(154206,154202,154203,'category',154111);`); err != nil {
		t.Fatal(err)
	}
	sessions := identityrepo.NewPgSessionRepository(pool)
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "flag-module-owned-test-key", Issuer: "flag-module", Audience: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour})
	current, err := identityservice.NewSessionService(sessions, users, jwt, identityservice.SessionConfig{IPHashSecret: "flag-module-owned-ip-key"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := users.GetByID(ctx, "154202")
	tokens, err := current.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Module User flag"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, current, current, claims)
	}
	threads := communityrepo.NewPgThreadRepository(pool)
	core := communityservice.NewThreadService(threads, nil)
	core.SetGovernanceRepository(communityrepo.NewPgContentGovernanceRepository(pool))
	core.SetUserScopedExecutionAuthority(identityservice.NewUserScopedExecutionAuthorityService(identityrepo.NewPgRoleRepository(pool), current, identityrepo.NewPgAdminAccountRepository(pool)))
	core.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	module := NewService(nil, community.NewModerationGateway(communityrepo.NewPgCategoryRepository(pool), threads, communityrepo.NewPgPostRepository(pool), core, nil), NewPgAuditStore(pool), Config{AllowPin: true, AllowLock: true})
	for i, operation := range []string{"pin", "unpin", "lock", "unlock"} {
		for j, mode := range []string{"success", "audit_failure", "config_change"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				row := &communitydomain.Thread{ID: fmt.Sprint(154301 + i*10 + j), AuthorID: "154201", CategoryID: "154111", Title: "private module title", Content: "private module body", PublicationStatus: "private", ModerationStatus: "clear", DeletionStatus: "active", IsPinned: operation == "unpin", IsLocked: operation == "unlock"}
				if err := threads.Create(ctx, row); err != nil {
					t.Fatal(err)
				}
				module.config = Config{AllowPin: true, AllowLock: true}
				module.audit = threadFlagPgHookAudit{NewPgAuditStore(pool), func(txCtx context.Context) error {
					tx, ok := transaction.FromContext(txCtx)
					if !ok {
						t.Fatal("module audit escaped command transaction")
					}
					var count int
					if err := tx.QueryRow(txCtx, `SELECT count(*) FROM audit_logs WHERE resource='thread' AND resource_id=$1 AND actor_type='user' AND actor_id=154202`, row.ID).Scan(&count); err != nil || count != 1 {
						t.Fatal("actual module audit was not inserted", count, err)
					}
					if mode == "audit_failure" {
						return errors.New("required PostgreSQL module audit rejected after insert")
					}
					if mode == "config_change" {
						module.config = Config{}
					}
					return nil
				}}
				before, _ := threads.GetByID(ctx, row.ID)
				result, err := module.SetThreadFlagForPrincipal(ctx, row.ID, operation, proof, OperationContext{TraceID: "owned-user-flag"})
				if mode == "success" {
					if err != nil || result == nil {
						t.Fatal(result, err)
					}
				} else if err == nil || result != nil || mode == "config_change" && !errors.Is(err, ErrActionDisabled) {
					t.Fatal("required audit/config failure accepted", result, err)
				}
				actual, _ := threads.GetByID(ctx, row.ID)
				want := 0
				if mode == "success" {
					want = 1
				}
				for _, query := range []string{`SELECT count(*) FROM audit_logs WHERE resource_id=$1`, `SELECT count(*) FROM content_revisions WHERE thread_id=$1`, `SELECT count(*) FROM content_moderation_actions WHERE thread_id=$1`, `SELECT count(*) FROM platform_command_audits WHERE resource_id=$1`, `SELECT count(*) FROM platform_outbox WHERE aggregate_id=$1`} {
					var count int
					if err := pool.QueryRow(ctx, query, row.ID).Scan(&count); err != nil || count != want {
						t.Fatal("partial module/Core command", mode, query, count, want, err)
					}
				}
				if mode != "success" && (actual.FactsVersion != before.FactsVersion || actual.CurrentRevision != before.CurrentRevision || actual.IsPinned != before.IsPinned || actual.IsLocked != before.IsLocked) {
					t.Fatal("failed module audit/config retained resource", actual)
				}
			})
		}
	}
}
