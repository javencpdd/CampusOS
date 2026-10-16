package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

func publicListPostgres(t *testing.T) (*repository.PgThreadRepository, *pgxpool.Pool, context.Context) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("CAMPUSOS_COMMUNITY_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("CAMPUSOS_COMMUNITY_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_list" {
		t.Fatal("public list tests require the owned campusos_v12_02b_list drill database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
DELETE FROM threads WHERE id BETWEEN 98200 AND 98299;
INSERT INTO users (id,username,nickname,email) VALUES
 (98101,'public-list-owner','Owner','public-list-owner@test.local'),
 (98102,'public-list-other','Other','public-list-other@test.local') ON CONFLICT (id) DO NOTHING;
INSERT INTO categories (id,name,slug,node_kind,lifecycle_status) VALUES
 (98110,'Public list group','public-list-group','group','active') ON CONFLICT (id) DO NOTHING;
INSERT INTO categories (id,name,slug,parent_id,node_kind,lifecycle_status) VALUES
 (98111,'Public list board','public-list-board',98110,'board','active'),
 (98112,'Public list other board','public-list-other-board',98110,'board','active') ON CONFLICT (id) DO NOTHING;
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES
 (98111,'discussion',TRUE),(98112,'discussion',TRUE) ON CONFLICT (category_id,thread_type) DO NOTHING;`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), `
DELETE FROM threads WHERE id BETWEEN 98200 AND 98299;
DELETE FROM category_thread_type_policies WHERE category_id IN (98111,98112);
DELETE FROM categories WHERE id IN (98111,98112);
DELETE FROM categories WHERE id=98110;
DELETE FROM users WHERE id IN (98101,98102);`)
		if err != nil {
			t.Errorf("clean public list fixtures: %v", err)
		}
	})
	return repository.NewPgThreadRepository(pool), pool, ctx
}

func publicListPostgresFixture(t *testing.T, repo repository.ThreadRepository, id string) *domain.Thread {
	t.Helper()
	timestamp := time.Date(2026, 10, 11, 1, 2, 3, 0, time.UTC)
	thread := &domain.Thread{ID: id, AuthorID: "98101", CategoryID: "98111", Title: "public list title",
		ThreadType: domain.ThreadTypeDiscussion, Content: "public list body", ContentFormat: "markdown",
		Status: domain.ThreadStatusPublished, PublicationStatus: domain.PublicationStatusPublished,
		ModerationStatus: domain.ModerationStatusClear, DeletionStatus: domain.DeletionStatusActive,
		CurrentRevision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := repo.Create(context.Background(), thread); err != nil {
		t.Fatal(err)
	}
	return thread
}

func publicListPostgresIDs(items []*domain.Thread) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

func TestPublicThreadListPostgreSQLStateMatrixAndPagination(t *testing.T) {
	repo, pool, ctx := publicListPostgres(t)
	index := 0
	var publicID string
	for _, publication := range []string{"draft", "published", "private"} {
		for _, moderation := range []string{"clear", "pending", "rejected", "taken_down"} {
			for _, deletion := range []string{"active", "trashed", "purged"} {
				id := fmt.Sprintf("%d", 98200+index)
				index++
				publicListPostgresFixture(t, repo, id)
				// A contradictory legacy status cannot authorize these raw states.
				if _, err := pool.Exec(ctx, `UPDATE threads SET status='published',
					publication_status=$1,moderation_status=$2,deletion_status=$3 WHERE id=$4`, publication, moderation, deletion, id); err != nil {
					t.Fatal(err)
				}
				if publication == "published" && moderation == "clear" && deletion == "active" {
					publicID = id
				}
			}
		}
	}
	svc := NewThreadService(repo, nil)
	filter := domain.ThreadListFilter{CategoryID: "98111", Page: 1, PageSize: 100}
	items, total, err := svc.ListPublicThreads(ctx, filter)
	if err != nil || total != 1 || !reflect.DeepEqual(publicListPostgresIDs(items), []string{publicID}) {
		t.Fatalf("36-state public predicate: ids=%v total=%d err=%v", publicListPostgresIDs(items), total, err)
	}
	// Caller filters cannot widen the public predicate or restore trashed rows.
	filter.PublicationStatus, filter.ModerationStatus, filter.DeletionStatus = "private", "taken_down", "trashed"
	filter.IncludeTrashed = true
	items, total, err = svc.ListPublicThreads(ctx, filter)
	if err != nil || total != 1 || !reflect.DeepEqual(publicListPostgresIDs(items), []string{publicID}) {
		t.Fatalf("caller widened public predicate: ids=%v total=%d err=%v", publicListPostgresIDs(items), total, err)
	}
	for _, id := range []string{"98250", "98251", "98252"} {
		publicListPostgresFixture(t, repo, id)
	}
	// These rows have equal timestamps; the ID tie-break must survive page cuts.
	wanted := []string{"98252", "98251", "98250", publicID}
	filter = domain.ThreadListFilter{CategoryID: "98111", PageSize: 2}
	for _, test := range []struct {
		page int
		ids  []string
	}{{1, wanted[:2]}, {2, wanted[2:]}, {3, []string{}}} {
		filter.Page = test.page
		items, total, err = svc.ListPublicThreads(ctx, filter)
		if err != nil || total != 4 || !reflect.DeepEqual(publicListPostgresIDs(items), test.ids) {
			t.Fatalf("page %d: ids=%v total=%d err=%v", test.page, publicListPostgresIDs(items), total, err)
		}
	}
	filter.CategoryID = "98112"
	items, total, err = svc.ListPublicThreads(ctx, filter)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("empty board: items=%+v total=%d err=%v", items, total, err)
	}
}

// Hooks affect real PostgreSQL snapshots while preserving the production
// repository. Adapter corruption is injected only into returned read objects,
// never through NormalizeContentState or weakened database constraints.
type postgresPublicListHookRepository struct {
	repository.ThreadRepository
	calls  int
	before func(int)
	after  func(int, []*domain.Thread)
}

func (r *postgresPublicListHookRepository) List(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	r.calls++
	if r.before != nil {
		r.before(r.calls)
	}
	items, total, err := r.ThreadRepository.List(ctx, filter)
	if err == nil && r.after != nil {
		r.after(r.calls, items)
	}
	return items, total, err
}

func TestPublicThreadListPostgreSQLSnapshotRetry(t *testing.T) {
	for _, change := range []string{"private", "taken-down", "trashed", "content", "content-aba", "state-aba", "move-out", "move-in", "off-page-total"} {
		t.Run(change, func(t *testing.T) {
			repo, pool, ctx := publicListPostgres(t)
			first := publicListPostgresFixture(t, repo, "98271")
			second := publicListPostgresFixture(t, repo, "98272")
			third := publicListPostgresFixture(t, repo, "98273")
			if _, err := pool.Exec(ctx, "UPDATE threads SET category_id=98112 WHERE id=98273"); err != nil {
				t.Fatal(err)
			}
			// The writer is held open before snapshot one. The second read uses
			// another connection and must observe the commit, then retry the page.
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			switch change {
			case "private":
				_, err = tx.Exec(ctx, "UPDATE threads SET publication_status='private',content='private secret' WHERE id=98272")
			case "taken-down":
				_, err = tx.Exec(ctx, "UPDATE threads SET moderation_status='taken_down',content='taken down secret' WHERE id=98272")
			case "trashed":
				_, err = tx.Exec(ctx, "UPDATE threads SET deletion_status='trashed',content='trashed secret' WHERE id=98272")
			case "content":
				_, err = tx.Exec(ctx, "UPDATE threads SET content='new public body' WHERE id=98272")
			case "content-aba":
				_, err = tx.Exec(ctx, "UPDATE threads SET content='intermediate public body' WHERE id=98272")
				if err == nil {
					_, err = tx.Exec(ctx, "UPDATE threads SET content=$1 WHERE id=98272", second.Content)
				}
			case "state-aba":
				_, err = tx.Exec(ctx, "UPDATE threads SET publication_status='private' WHERE id=98272")
				if err == nil {
					_, err = tx.Exec(ctx, "UPDATE threads SET publication_status='published' WHERE id=98272")
				}
			case "move-out":
				_, err = tx.Exec(ctx, "UPDATE threads SET category_id=98112 WHERE id=98272")
			case "move-in":
				_, err = tx.Exec(ctx, "UPDATE threads SET category_id=98111 WHERE id=98273")
			case "off-page-total":
				// Remove the lower-ranked row. Page one's IDs and versions stay
				// unchanged, so total alone must force a fresh decision/page.
				_, err = tx.Exec(ctx, "UPDATE threads SET publication_status='private' WHERE id=98271")
			}
			if err != nil {
				t.Fatal(err)
			}
			raced := &postgresPublicListHookRepository{ThreadRepository: repo}
			raced.before = func(call int) {
				if call == 2 {
					if err := tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}
			}
			engine := &recordingPublicPolicy{}
			svc := NewThreadService(raced, nil)
			svc.publicPolicy = engine
			filter := domain.ThreadListFilter{CategoryID: "98111", Page: 1, PageSize: 1}
			items, total, err := svc.ListPublicThreads(ctx, filter)
			if err != nil || raced.calls != 4 || len(items) != 1 || len(engine.requests) != 2 {
				t.Fatalf("whole-page retry: items=%+v total=%d err=%v list-calls=%d decisions=%d", items, total, err, raced.calls, len(engine.requests))
			}
			wantedID, wantedTotal := second.ID, int64(2)
			switch change {
			case "private", "taken-down", "trashed", "move-out":
				wantedID, wantedTotal = first.ID, 1
			case "move-in":
				wantedID, wantedTotal = third.ID, 3
			case "off-page-total":
				wantedTotal = 1
			}
			if items[0].ID != wantedID || total != wantedTotal || !items[0].IsPublic() {
				t.Fatalf("returned stale page: items=%+v total=%d want=%s/%d", items, total, wantedID, wantedTotal)
			}
			if engine.requests[0].RequestID == engine.requests[1].RequestID {
				t.Fatal("retry reused the old policy request")
			}
			if change == "content" && items[0].Content != "new public body" {
				t.Fatal("content retry returned the old body")
			}
			if (change == "content" || change == "content-aba" || change == "state-aba") &&
				(items[0].FactsVersion <= second.FactsVersion || engine.requests[0].Facts.FactsVersion == engine.requests[1].Facts.FactsVersion) {
				t.Fatal("content/state ABA reused an old fact version")
			}
		})
	}
}

func TestPublicThreadListPostgreSQLSustainedConflict(t *testing.T) {
	repo, pool, ctx := publicListPostgres(t)
	publicListPostgresFixture(t, repo, "98280")
	raced := &postgresPublicListHookRepository{ThreadRepository: repo}
	raced.before = func(call int) {
		if call%2 == 0 {
			if _, err := pool.Exec(ctx, "UPDATE threads SET content=content||'!' WHERE id=98280"); err != nil {
				t.Fatal(err)
			}
		}
	}
	items, total, err := NewThreadService(raced, nil).ListPublicThreads(ctx,
		domain.ThreadListFilter{CategoryID: "98111", Page: 1, PageSize: 10})
	if items != nil || total != 0 || !errors.Is(err, port.ErrPolicyFactsChanged) || raced.calls != 6 {
		t.Fatalf("unbounded or partial list: items=%+v total=%d err=%v list-calls=%d", items, total, err, raced.calls)
	}
}

func TestPublicThreadListPostgreSQLRawFactsFailsClosed(t *testing.T) {
	for _, mutation := range []string{"publication", "moderation", "deletion", "version", "owner", "board"} {
		for _, snapshot := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/snapshot-%d", mutation, snapshot), func(t *testing.T) {
				repo, _, ctx := publicListPostgres(t)
				publicListPostgresFixture(t, repo, "98281")
				raced := &postgresPublicListHookRepository{ThreadRepository: repo}
				raced.after = func(call int, items []*domain.Thread) {
					if call != snapshot || len(items) != 1 {
						return
					}
					row := items[0]
					switch mutation {
					case "publication":
						row.PublicationStatus = ""
					case "moderation":
						row.ModerationStatus = ""
					case "deletion":
						row.DeletionStatus = ""
					case "version":
						row.FactsVersion = 0
					case "owner":
						row.AuthorID = ""
					case "board":
						row.CategoryID = ""
					}
				}
				items, total, err := NewThreadService(raced, nil).ListPublicThreads(ctx,
					domain.ThreadListFilter{CategoryID: "98111", Page: 1, PageSize: 10})
				if err == nil || items != nil || total != 0 {
					t.Fatalf("raw adapter fault normalized or partially returned: items=%+v total=%d err=%v", items, total, err)
				}
			})
		}
	}
}

func TestPublicThreadListPostgreSQLRepositorySnapshotConcurrency(t *testing.T) {
	repo, pool, _ := publicListPostgres(t)
	for i := 0; i < 20; i++ {
		publicListPostgresFixture(t, repo, fmt.Sprintf("%d", 98200+i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	writerDone := make(chan error, 1)
	start := make(chan struct{})
	go func() {
		<-start
		for i := 0; i < 100; i++ {
			publication := "published"
			if i%2 == 0 {
				publication = "private"
			}
			tx, err := pool.Begin(ctx)
			if err == nil {
				_, err = tx.Exec(ctx, "UPDATE threads SET publication_status=$1 WHERE id BETWEEN 98200 AND 98209", publication)
				if err == nil {
					err = tx.Commit(ctx)
				} else {
					_ = tx.Rollback(ctx)
				}
			}
			if err != nil {
				writerDone <- err
				return
			}
		}
		writerDone <- nil
	}()
	close(start)
	filter := domain.ThreadListFilter{CategoryID: "98111", AuthorID: "98101", PublicationStatus: "published",
		ModerationStatus: "clear", DeletionStatus: "active", Page: 1, PageSize: 100}
	var readErr error
	for i := 0; i < 200; i++ {
		items, total, err := repo.List(ctx, filter)
		if err != nil {
			readErr = err
			break
		}
		if int64(len(items)) != total || (total != 10 && total != 20) {
			readErr = fmt.Errorf("count and page mixed snapshots: page=%d total=%d", len(items), total)
			break
		}
		for _, item := range items {
			if item.PublicationStatus != "published" || item.ModerationStatus != "clear" || item.DeletionStatus != "active" {
				readErr = fmt.Errorf("nonpublic row in repository snapshot: id=%s", item.ID)
				break
			}
		}
		if readErr != nil {
			break
		}
	}
	// Join before fixture cleanup even on assertion failure; cancellation bounds
	// both connections and prevents a writer outliving this test's database rows.
	if readErr != nil {
		cancel()
	}
	writerErr := <-writerDone
	if readErr != nil {
		t.Fatal(readErr)
	}
	if writerErr != nil {
		t.Fatal(writerErr)
	}
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
}
