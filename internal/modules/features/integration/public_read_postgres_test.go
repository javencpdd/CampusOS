package integration

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communitysvc "github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationPostgreSQLSourceACLAndCommitLock(t *testing.T) {
	url := os.Getenv("CAMPUSOS_INTEGRATION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("owned Integration PG not configured")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Database != "campusos_v12_02c" {
		t.Fatal("owned target required")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES(7250101,'integration_source','Source','integration-source@example.test','active',1) ON CONFLICT(id) DO NOTHING;
INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(7250102,'Integration source','integration-source','board','active') ON CONFLICT(id) DO NOTHING;
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(7250102,'discussion',TRUE) ON CONFLICT(category_id,thread_type) DO UPDATE SET enabled=TRUE;
INSERT INTO threads(id,author_id,author_name,category_id,title,content,content_format,status,thread_type,publication_status,moderation_status,deletion_status) VALUES(7250103,7250101,'Source',7250102,'Source title','Source body','markdown','published','discussion','published','clear','active') ON CONFLICT(id) DO UPDATE SET publication_status='published',moderation_status='clear',deletion_status='active';`); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewPgIntegrationRepository(pool)
	svc := service.NewIntegrationService(repo, testIntegrationAuthority{}, reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	proof := func(context.Context) (port.PrincipalContext, error) {
		return port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "admin", ID: "setup_source"}, Audience: "admin", AuthenticationStrength: "mfa", CredentialID: "setup_session"}, nil
	}
	p, err := svc.CreatePrincipal(ctx, "source proof", proof)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	rules := []port.IntegrationAccessRule{{ID: "http", Channel: "integration_http", Endpoint: "public_thread.read", Action: "community.thread.read", ResourceKind: "community.thread", ResourceIDs: []string{"7250103"}, Status: "active", NotBeforeMS: now - 1, ExpiresAtMS: now + 3600000}}
	issued, err := svc.IssueCredential(ctx, p.ID, service.IntegrationIssueRequest{PrincipalVersion: p.Version, NotBeforeMS: now - 1, ExpiresAtMS: now + 3600000, Rules: rules}, proof)
	if err != nil {
		t.Fatal(err)
	}
	source := community.NewContentQuery(communitysvc.NewThreadService(communityrepo.NewPgThreadRepository(pool), nil))
	target := port.IntegrationAccessTarget{Channel: "integration_http", Endpoint: "public_thread.read", Action: "community.thread.read", Resource: port.PolicyResourceRef{Kind: "community.thread", ID: "7250103"}}
	for _, state := range []struct {
		column, value string
		allow         bool
	}{{"publication_status", "published", true}, {"publication_status", "private", false}, {"moderation_status", "pending", false}, {"deletion_status", "trashed", false}} {
		if _, err := pool.Exec(ctx, `UPDATE threads SET publication_status='published',moderation_status='clear',deletion_status='active' WHERE id=7250103`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE threads SET `+state.column+`=$1 WHERE id=7250103`, state.value); err != nil {
			t.Fatal(err)
		}
		data, err := svc.ReadForIntegration(ctx, issued.Token, target, func(tx context.Context) (any, error) {
			thread, err := source.GetPublicThread(tx, "7250103")
			if err != nil {
				return nil, err
			}
			_, lockErr := pool.Exec(ctx, `SELECT id FROM threads WHERE id=7250103 FOR UPDATE NOWAIT`)
			var pe *pgconn.PgError
			if !errors.As(lockErr, &pe) || pe.Code != "55P03" {
				return nil, errors.New("source row not locked through machine commit")
			}
			return thread.Content, nil
		})
		if state.allow {
			if err != nil || data != "Source body" {
				t.Fatal(data, err)
			}
		} else if err == nil || data != nil {
			t.Fatal("original source ACL bypass", state.value)
		}
	}
}
