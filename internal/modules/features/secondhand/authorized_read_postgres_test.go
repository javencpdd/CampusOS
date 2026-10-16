package secondhand

import (
	"context"
	"fmt"
	community "github.com/campusos/CampusOS/internal/modules/core/community"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func typedReadPostgres(t *testing.T, owner string) (*Service, *communityrepo.PgThreadRepository, *pgxpool.Pool) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_TYPED_FEATURE_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_TYPED_FEATURE_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_typed_read" {
		t.Fatal("typed Feature tests require their owned campusos_v12_02b_typed_read database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES($1::bigint,$2,$2,$3) ON CONFLICT(id) DO NOTHING`, owner, "typed-"+owner, "typed-"+owner+"@example.test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(110110,'Typed group','typed-group','group','active') ON CONFLICT(id) DO NOTHING;
 INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(110111,'Typed board','typed-board',110110,'board','active') ON CONFLICT(id) DO NOTHING;
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(110111,'secondhand',TRUE) ON CONFLICT(category_id,thread_type) DO NOTHING;`)
	if err != nil {
		t.Fatal(err)
	}
	repo := communityrepo.NewPgThreadRepository(pool)
	threads := communityservice.NewThreadService(repo, nil)
	durable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	threads.SetReliability(durable)
	threads.SetGovernanceRepository(communityrepo.NewPgContentGovernanceRepository(pool))
	threads.SetCategoryRepository(communityrepo.NewPgCategoryRepository(pool))
	threads.SetThreadTypePolicyRepository(communityrepo.NewPgThreadTypePolicyRepository(pool))
	s := NewService(NewPgStore(pool), community.NewContentGateway(repo, threads), community.NewContentQuery(threads))
	s.SetReliability(durable)
	return s, repo, pool
}
func typedReadCreate(t *testing.T, s *Service, owner string) *Result {
	t.Helper()
	request := validCreateRequest()
	request.CategoryID = "110111"
	created, err := s.Create(context.Background(), owner, "Target Owner", request)
	if err != nil {
		t.Fatal(err)
	}
	return created
}
func typedReadCommit(t *testing.T, pool *pgxpool.Pool, write func(pgx.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := write(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestSecondhandAuthorizedPostgreSQLStateMatrix(t *testing.T) {
	ctx := context.Background()
	owner := "111101"
	s, repo, _ := typedReadPostgres(t, owner)
	for _, pub := range []domain.PublicationStatus{domain.PublicationStatusPublished, domain.PublicationStatusPrivate, domain.PublicationStatusDraft} {
		for _, mod := range []domain.ModerationStatus{domain.ModerationStatusClear, domain.ModerationStatusPending, domain.ModerationStatusRejected, domain.ModerationStatusTakenDown} {
			for _, del := range []domain.DeletionStatus{domain.DeletionStatusActive, domain.DeletionStatusTrashed, domain.DeletionStatusPurged} {
				t.Run(string(pub)+"/"+string(mod)+"/"+string(del), func(t *testing.T) {
					created := typedReadCreate(t, s, owner)
					thread, _ := repo.GetByID(ctx, created.Thread.ID)
					thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus = pub, mod, del
					if err := repo.Update(ctx, thread); err != nil {
						t.Fatal(err)
					}
					result, err := s.GetForPrincipal(ctx, thread.ID, typedReadProof(owner))
					if (err == nil) != (del == domain.DeletionStatusActive) || (err != nil && result != nil) {
						t.Fatalf("owner %v %v", result, err)
					}
					result, err = s.GetForPrincipal(ctx, thread.ID, nil)
					public := pub == domain.PublicationStatusPublished && mod == domain.ModerationStatusClear && del == domain.DeletionStatusActive
					if (err == nil) != public || (err != nil && result != nil) {
						t.Fatalf("public %v %v", result, err)
					}
					stored, err := repo.GetByID(ctx, thread.ID)
					if err != nil || stored.ViewCount != 0 {
						t.Fatalf("views %v", err)
					}
				})
			}
		}
	}
}
func TestSecondhandAuthorizedPostgreSQLCommittedRaces(t *testing.T) {
	for i, mode := range []string{"detail", "publication", "moderation", "trash-aba", "owner-aba"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			owner := fmt.Sprint(111200 + i)
			s, _, pool := typedReadPostgres(t, owner)
			created := typedReadCreate(t, s, owner)
			store := &typedReadStore{Store: s.store}
			store.afterGet = func(n int) {
				if n != 1 {
					return
				}
				typedReadCommit(t, pool, func(tx pgx.Tx) error {
					var err error
					switch mode {
					case "detail":
						_, err = tx.Exec(ctx, `UPDATE secondhand_details SET location_scope='changed',version=version+1,updated_at=NOW() WHERE thread_id=$1`, created.Thread.ID)
					case "publication":
						_, err = tx.Exec(ctx, `UPDATE threads SET publication_status='private' WHERE id=$1`, created.Thread.ID)
					case "moderation":
						_, err = tx.Exec(ctx, `UPDATE threads SET moderation_status='taken_down' WHERE id=$1`, created.Thread.ID)
					case "trash-aba":
						_, err = tx.Exec(ctx, `UPDATE threads SET deletion_status='trashed' WHERE id=$1`, created.Thread.ID)
						if err == nil {
							_, err = tx.Exec(ctx, `UPDATE threads SET deletion_status='active' WHERE id=$1`, created.Thread.ID)
						}
					case "owner-aba":
						_, err = tx.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES(111999,'typed-other-secondhand','Other','typed-other-secondhand@example.test') ON CONFLICT(id) DO NOTHING`)
						if err == nil {
							_, err = tx.Exec(ctx, `UPDATE threads SET author_id=111999 WHERE id=$1`, created.Thread.ID)
						}
						if err == nil {
							_, err = tx.Exec(ctx, `UPDATE threads SET author_id=$2::bigint WHERE id=$1`, created.Thread.ID, owner)
						}
					}
					return err
				})
			}
			s.store = store
			if result, err := s.GetForPrincipal(ctx, created.Thread.ID, typedReadProof(owner)); err == nil || result != nil {
				t.Fatalf("stale result %v %v", result, err)
			}
		})
	}
}
func TestSecondhandAuthorizedPostgreSQLWholePageAndCount(t *testing.T) {
	ctx := context.Background()
	owner := "111301"
	s, repo, pool := typedReadPostgres(t, owner)
	var ids []string
	for i := 0; i < 3; i++ {
		ids = append(ids, typedReadCreate(t, s, owner).Thread.ID)
	}
	rows, total, err := s.ListPublicChecked(ctx, domain.ThreadListFilter{AuthorID: owner, Page: 1, PageSize: 1})
	if err != nil || total != 3 || len(rows) != 1 {
		t.Fatalf("page %v %d %v", rows, total, err)
	}
	rows, total, err = s.ListPublicChecked(ctx, domain.ThreadListFilter{AuthorID: owner, Page: 9, PageSize: 1})
	if err != nil || total != 3 || len(rows) != 0 {
		t.Fatalf("empty page %v %d %v", rows, total, err)
	}
	base := s.store
	store := &typedReadStore{Store: base}
	store.afterMany = func(n int) {
		if n != 1 {
			return
		}
		typedReadCommit(t, pool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE threads SET publication_status='private' WHERE id=$1`, ids[0])
			return err
		})
	}
	s.store = store
	if rows, total, err := s.ListPublicChecked(ctx, domain.ThreadListFilter{AuthorID: owner, Page: 1, PageSize: 1}); err == nil || rows != nil || total != 0 {
		t.Fatalf("stale count %v %d %v", rows, total, err)
	}
	s.store = base
	thread, err := repo.GetByID(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if thread.PublicationStatus != domain.PublicationStatusPrivate {
		t.Fatal("race did not commit")
	}
}
