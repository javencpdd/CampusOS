package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ownerListPostgresConfig(t *testing.T) *pgxpool.Config {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("CAMPUSOS_COMMUNITY_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("CAMPUSOS_COMMUNITY_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_owner_list" {
		t.Fatal("owner list tests require the owned campusos_v12_02b_owner_list drill database")
	}
	return config
}

func ownerListPostgres(t *testing.T) (*repository.PgThreadRepository, *pgxpool.Pool, context.Context) {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, ownerListPostgresConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
DELETE FROM threads WHERE id BETWEEN 101200 AND 101299;
INSERT INTO users (id,username,nickname,email) VALUES
 (101101,'owner-list-owner','Owner','owner-list-owner@test.local'),
 (101102,'owner-list-other','Other','owner-list-other@test.local') ON CONFLICT (id) DO NOTHING;
INSERT INTO categories (id,name,slug,node_kind,lifecycle_status) VALUES
 (101110,'Owner list group','owner-list-group','group','active') ON CONFLICT (id) DO NOTHING;
INSERT INTO categories (id,name,slug,parent_id,node_kind,lifecycle_status) VALUES
 (101111,'Owner list board','owner-list-board',101110,'board','active'),
 (101112,'Owner list other board','owner-list-other-board',101110,'board','active') ON CONFLICT (id) DO NOTHING;
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES
 (101111,'discussion',TRUE),(101112,'discussion',TRUE) ON CONFLICT (category_id,thread_type) DO NOTHING;`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), `
DELETE FROM threads WHERE id BETWEEN 101200 AND 101299;
DELETE FROM category_thread_type_policies WHERE category_id IN (101111,101112);
DELETE FROM categories WHERE id IN (101111,101112);
DELETE FROM categories WHERE id=101110;
DELETE FROM users WHERE id IN (101101,101102);`)
		if err != nil {
			t.Errorf("clean owner list fixtures: %v", err)
		}
	})
	return repository.NewPgThreadRepository(pool), pool, ctx
}

func ownerListPostgresFixture(t *testing.T, repo repository.ThreadRepository, id string) *domain.Thread {
	t.Helper()
	timestamp := time.Date(2026, 10, 12, 1, 2, 3, 0, time.UTC)
	row := &domain.Thread{ID: id, AuthorID: "101101", CategoryID: "101111", Title: "owner list title",
		ThreadType: domain.ThreadTypeDiscussion, Content: "private owner list body", ContentFormat: "markdown",
		Status: domain.ThreadStatusPrivate, PublicationStatus: domain.PublicationStatusPrivate,
		ModerationStatus: domain.ModerationStatusClear, DeletionStatus: domain.DeletionStatusActive,
		CurrentRevision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := repo.Create(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	return row
}

// Trusted ingress fixture, independent from HTTP credential verification. The
// PostgreSQL target here is Community storage, rather than Identity sessions.
func ownerListPostgresResolver(_ context.Context) (port.PrincipalContext, error) {
	principal := ownerReadPrincipal()
	principal.Actor.ID = "101101"
	return principal, nil
}

func ownerListPostgresIDs(rows []*domain.Thread) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids
}

func TestOwnerThreadListPostgreSQLStateMatrixAndPagination(t *testing.T) {
	repo, pool, ctx := ownerListPostgres(t)
	index := 0
	wanted := make([]string, 0, 12)
	for _, publication := range []string{"draft", "published", "private"} {
		for _, moderation := range []string{"clear", "pending", "rejected", "taken_down"} {
			for _, deletion := range []string{"active", "trashed", "purged"} {
				id := fmt.Sprintf("%d", 101200+index)
				index++
				ownerListPostgresFixture(t, repo, id)
				if _, err := pool.Exec(ctx, `UPDATE threads SET status='published',
					publication_status=$1,moderation_status=$2,deletion_status=$3 WHERE id=$4`, publication, moderation, deletion, id); err != nil {
					t.Fatal(err)
				}
				if deletion == "active" {
					wanted = append(wanted, id)
				}
			}
		}
	}
	for _, id := range []string{"101290", "101291", "101292"} {
		ownerListPostgresFixture(t, repo, id)
	}
	if _, err := pool.Exec(ctx, `UPDATE threads SET author_id=101102 WHERE id IN(101290,101291);
UPDATE threads SET publication_status='published' WHERE id=101291;
UPDATE threads SET deleted_at=NOW() WHERE id=101292;`); err != nil {
		t.Fatal(err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(wanted)))
	svc := NewThreadService(repo, nil)
	filter := domain.ThreadListFilter{CategoryID: "101111", Page: 1, PageSize: 100}
	rows, total, err := svc.ListThreadsForPrincipal(ctx, ownerListPostgresResolver, filter)
	if err != nil || total != 12 || !reflect.DeepEqual(ownerListPostgresIDs(rows), wanted) {
		t.Fatalf("36-state owner predicate: ids=%v total=%d err=%v", ownerListPostgresIDs(rows), total, err)
	}
	for _, row := range rows {
		if row.AuthorID != "101101" || row.DeletionStatus != "active" || row.ViewCount != 0 {
			t.Fatalf("other owner/trash/count side effect: %+v", row)
		}
	}
	// Client-supplied owner and trash permission cannot widen the trusted list.
	filter.AuthorID, filter.IncludeTrashed = "101102", true
	rows, total, err = svc.ListThreadsForPrincipal(ctx, ownerListPostgresResolver, filter)
	if err != nil || total != 12 || !reflect.DeepEqual(ownerListPostgresIDs(rows), wanted) {
		t.Fatalf("client owner widened or replaced trusted list: ids=%v total=%d err=%v", ownerListPostgresIDs(rows), total, err)
	}
	filter = domain.ThreadListFilter{CategoryID: "101111", PageSize: 5}
	for _, test := range []struct {
		page int
		ids  []string
	}{{1, wanted[:5]}, {2, wanted[5:10]}, {3, wanted[10:]}, {4, []string{}}} {
		filter.Page = test.page
		rows, total, err = svc.ListThreadsForPrincipal(ctx, ownerListPostgresResolver, filter)
		if err != nil || total != 12 || !reflect.DeepEqual(ownerListPostgresIDs(rows), test.ids) {
			t.Fatalf("page %d: ids=%v total=%d err=%v", test.page, ownerListPostgresIDs(rows), total, err)
		}
	}
	filter.CategoryID = "101112"
	rows, total, err = svc.ListThreadsForPrincipal(ctx, ownerListPostgresResolver, filter)
	if err != nil || len(rows) != 0 || total != 0 {
		t.Fatalf("empty board: rows=%+v total=%d err=%v", rows, total, err)
	}
}

type postgresOwnerListSnapshot interface {
	ListOwnerThreadSnapshot(context.Context, string, domain.ThreadListFilter) ([]*domain.Thread, int64, error)
}

type ownerListPostgresHookRepository struct {
	repository.ThreadRepository
	calls   int
	before  func(int)
	after   func(int, []*domain.Thread)
	errorAt int
}

func (r *ownerListPostgresHookRepository) ListOwnerThreadSnapshot(ctx context.Context, owner string, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	r.calls++
	if r.before != nil {
		r.before(r.calls)
	}
	if r.calls == r.errorAt {
		return nil, 0, errors.New("snapshot query fault with private diagnostic")
	}
	rows, total, err := r.ThreadRepository.(postgresOwnerListSnapshot).ListOwnerThreadSnapshot(ctx, owner, filter)
	if err == nil && r.after != nil {
		r.after(r.calls, rows)
	}
	return rows, total, err
}

func TestOwnerThreadListPostgreSQLCommittedRaces(t *testing.T) {
	ownerListPostgresConfig(t)
	for _, change := range []string{"owner", "purge", "trash", "deleted", "membership", "order", "body", "state-aba", "owner-aba", "replacement", "off-page-total", "empty-to-member"} {
		t.Run(change, func(t *testing.T) {
			repo, pool, ctx := ownerListPostgres(t)
			ownerListPostgresFixture(t, repo, "101271")
			top := ownerListPostgresFixture(t, repo, "101272")
			ownerListPostgresFixture(t, repo, "101273")
			if _, err := pool.Exec(ctx, "UPDATE threads SET author_id=101102 WHERE id=101273"); err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			switch change {
			case "owner":
				_, err = tx.Exec(ctx, "UPDATE threads SET author_id=101102,content='other owner secret' WHERE id=101272")
			case "purge":
				_, err = tx.Exec(ctx, "UPDATE threads SET deletion_status='purged',deleted_at=NOW() WHERE id=101272")
			case "trash":
				_, err = tx.Exec(ctx, "UPDATE threads SET deletion_status='trashed' WHERE id=101272")
			case "deleted":
				_, err = tx.Exec(ctx, "UPDATE threads SET deleted_at=NOW() WHERE id=101272")
			case "membership", "empty-to-member":
				_, err = tx.Exec(ctx, "UPDATE threads SET author_id=101101 WHERE id=101273")
			case "order":
				_, err = tx.Exec(ctx, "UPDATE threads SET is_pinned=TRUE WHERE id=101271")
			case "body":
				_, err = tx.Exec(ctx, "UPDATE threads SET content='fresh owner body' WHERE id=101272")
			case "state-aba":
				_, err = tx.Exec(ctx, "UPDATE threads SET publication_status='published' WHERE id=101272")
				if err == nil {
					_, err = tx.Exec(ctx, "UPDATE threads SET publication_status='private' WHERE id=101272")
				}
			case "owner-aba":
				_, err = tx.Exec(ctx, "UPDATE threads SET author_id=101102 WHERE id=101272")
				if err == nil {
					_, err = tx.Exec(ctx, "UPDATE threads SET author_id=101101 WHERE id=101272")
				}
			case "replacement":
				_, err = tx.Exec(ctx, "DELETE FROM threads WHERE id=101272")
				if err == nil {
					_, err = tx.Exec(ctx, `INSERT INTO threads(id,thread_type,title,content,content_format,author_id,category_id,status,
						publication_status,moderation_status,deletion_status,current_revision,created_at,updated_at)
						VALUES(101272,'discussion','replacement','replacement owner body','markdown',101101,101111,'private','private','clear','active',1,$1,$2)`, top.CreatedAt, top.UpdatedAt)
				}
			case "off-page-total":
				_, err = tx.Exec(ctx, "UPDATE threads SET author_id=101102 WHERE id=101271")
			}
			if err != nil {
				t.Fatal(err)
			}
			raced := &ownerListPostgresHookRepository{ThreadRepository: repo}
			raced.before = func(call int) {
				if call == 2 {
					if err := tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}
			}
			recorder := &recordingOwnerPolicy{}
			svc := NewThreadService(raced, nil)
			svc.ownerPolicy = recorder
			filter := domain.ThreadListFilter{CategoryID: "101111", Page: 1, PageSize: 1}
			if change == "empty-to-member" {
				filter.Page = 3
			}
			rows, total, err := svc.ListThreadsForPrincipal(ctx, ownerListPostgresResolver, filter)
			if err != nil || len(rows) != 1 || raced.calls != 4 {
				t.Fatalf("whole owner page retry: rows=%+v total=%d err=%v calls=%d", rows, total, err, raced.calls)
			}
			wantedID, wantedTotal, wantedDecisions := "101272", int64(2), 2
			switch change {
			case "owner", "purge", "trash", "deleted":
				wantedID, wantedTotal = "101271", 1
			case "membership":
				wantedID, wantedTotal = "101273", 3
			case "order":
				wantedID = "101271"
			case "off-page-total":
				wantedTotal = 1
			case "empty-to-member":
				wantedID, wantedTotal, wantedDecisions = "101271", 3, 1
			}
			if rows[0].ID != wantedID || total != wantedTotal || rows[0].AuthorID != "101101" || rows[0].DeletionStatus != "active" || rows[0].ViewCount != 0 || len(recorder.requests) != wantedDecisions {
				t.Fatalf("stale/unowned page: rows=%+v total=%d decisions=%d want=%s/%d/%d", rows, total, len(recorder.requests), wantedID, wantedTotal, wantedDecisions)
			}
			if wantedDecisions == 2 && recorder.requests[0].RequestID == recorder.requests[1].RequestID {
				t.Fatal("retry reused the policy request")
			}
			if change == "body" && rows[0].Content != "fresh owner body" {
				t.Fatal("returned stale owner content")
			}
			if change == "replacement" && rows[0].Content != "replacement owner body" {
				t.Fatal("returned preceding thread incarnation")
			}
			if (change == "body" || change == "state-aba" || change == "owner-aba" || change == "replacement") &&
				(rows[0].FactsVersion <= top.FactsVersion || recorder.requests[0].Facts.FactsVersion == recorder.requests[1].Facts.FactsVersion) {
				t.Fatal("content/state/owner/replacement ABA reused a fact epoch")
			}
		})
	}
}

func TestOwnerThreadListPostgreSQLFailureBoundaries(t *testing.T) {
	ownerListPostgresConfig(t)
	faults := []string{"persistent", "initial-query", "final-query", "decision", "initial-publication", "initial-moderation", "initial-deletion", "initial-version", "initial-owner", "initial-board", "final-publication", "final-moderation", "final-deletion", "final-version", "final-owner", "final-board", "revoked-before", "revoked-after", "empty-revoked-before", "empty-revoked-after"}
	for _, fault := range faults {
		t.Run(fault, func(t *testing.T) {
			repo, pool, ctx := ownerListPostgres(t)
			if !strings.HasPrefix(fault, "empty-") {
				ownerListPostgresFixture(t, repo, "101280")
			}
			raced := &ownerListPostgresHookRepository{ThreadRepository: repo}
			switch fault {
			case "persistent":
				raced.before = func(call int) {
					if call%2 == 0 {
						if _, err := pool.Exec(ctx, "UPDATE threads SET content=content||'!' WHERE id=101280"); err != nil {
							t.Fatal(err)
						}
					}
				}
			case "initial-query":
				raced.errorAt = 1
			case "final-query":
				raced.errorAt = 2
			}
			raced.after = func(call int, rows []*domain.Thread) {
				if (!strings.HasPrefix(fault, "initial-") && !strings.HasPrefix(fault, "final-")) || len(rows) != 1 ||
					(strings.HasPrefix(fault, "initial-") && call != 1) ||
					(strings.HasPrefix(fault, "final-") && call != 2) {
					return
				}
				row := rows[0]
				row.Status = domain.ThreadStatusPublished
				switch strings.TrimPrefix(strings.TrimPrefix(fault, "initial-"), "final-") {
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
			resolves := 0
			resolve := func(ctx context.Context) (port.PrincipalContext, error) {
				resolves++
				p, err := ownerListPostgresResolver(ctx)
				if ((fault == "revoked-before" || fault == "empty-revoked-before") && resolves >= 2) ||
					((fault == "revoked-after" || fault == "empty-revoked-after") && resolves >= 3) {
					return p, errors.New("revoked trusted session fixture")
				}
				return p, err
			}
			recorder := &recordingOwnerPolicy{corrupt: fault == "decision"}
			svc := NewThreadService(raced, nil)
			svc.ownerPolicy = recorder
			rows, total, err := svc.ListThreadsForPrincipal(ctx, resolve, domain.ThreadListFilter{CategoryID: "101111", Page: 1, PageSize: 10})
			if err == nil || rows != nil || total != 0 {
				t.Fatalf("fault returned partial content/count: rows=%+v total=%d err=%v", rows, total, err)
			}
			if fault == "persistent" && (!errors.Is(err, port.ErrPolicyFactsChanged) || raced.calls != 6 || len(recorder.requests) != 3) {
				t.Fatalf("unbounded conflict: calls=%d decisions=%d err=%v", raced.calls, len(recorder.requests), err)
			}
			if fault == "decision" && raced.calls != 1 {
				t.Fatal("consumed a decision bound to another credential")
			}
			if strings.Contains(fault, "revoked-") && !errors.Is(err, ErrThreadOwnerPrincipalUnavailable) {
				t.Fatalf("proof failure boundary: %v", err)
			}
		})
	}
}

func TestOwnerThreadListPostgreSQLCountersAndVersion(t *testing.T) {
	repo, pool, ctx := ownerListPostgres(t)
	thread := ownerListPostgresFixture(t, repo, "101290")
	raced := &ownerListPostgresHookRepository{ThreadRepository: repo}
	raced.before = func(call int) {
		if call == 2 {
			if _, err := pool.Exec(ctx, `UPDATE threads SET view_count=7,reply_count=3,like_count=2,updated_at=NOW(),facts_version=1 WHERE id=101290`); err != nil {
				t.Fatal(err)
			}
		}
	}
	recorder := &recordingOwnerPolicy{}
	svc := NewThreadService(raced, nil)
	svc.ownerPolicy = recorder
	rows, total, err := svc.ListThreadsForPrincipal(ctx, ownerListPostgresResolver, domain.ThreadListFilter{CategoryID: "101111", Page: 1, PageSize: 10})
	if err != nil || len(rows) != 1 || total != 1 || raced.calls != 2 || len(recorder.requests) != 1 ||
		rows[0].FactsVersion != thread.FactsVersion || rows[0].ViewCount != 7 || rows[0].ReplyCount != 3 || rows[0].LikeCount != 2 {
		t.Fatalf("counter writes invalidated or were overwritten by owner read: rows=%+v total=%d err=%v calls=%d decisions=%d", rows, total, err, raced.calls, len(recorder.requests))
	}
	if _, err := pool.Exec(ctx, "UPDATE threads SET content='new private list body' WHERE id=101290"); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetByID(ctx, thread.ID)
	if err != nil || current.FactsVersion <= thread.FactsVersion || current.ViewCount != 7 {
		t.Fatalf("content change did not advance epoch or read wrote counters: %+v %v", current, err)
	}
}

func TestOwnerThreadListPostgreSQLRepositorySnapshotConcurrency(t *testing.T) {
	repo, pool, _ := ownerListPostgres(t)
	for i := 0; i < 20; i++ {
		ownerListPostgresFixture(t, repo, fmt.Sprintf("%d", 101200+i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	writerDone := make(chan error, 1)
	start := make(chan struct{})
	go func() {
		<-start
		for i := 0; i < 100; i++ {
			owner := int64(101101)
			if i%2 == 0 {
				owner = 101102
			}
			tx, err := pool.Begin(ctx)
			if err == nil {
				_, err = tx.Exec(ctx, "UPDATE threads SET author_id=$1 WHERE id BETWEEN 101200 AND 101209", owner)
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
	filter := domain.ThreadListFilter{CategoryID: "101111", Page: 1, PageSize: 100}
	var readErr error
	for i := 0; i < 200; i++ {
		rows, total, err := any(repo).(postgresOwnerListSnapshot).ListOwnerThreadSnapshot(ctx, "101101", filter)
		if err != nil {
			readErr = err
			break
		}
		if int64(len(rows)) != total || (total != 10 && total != 20) {
			readErr = fmt.Errorf("mixed owner count/page snapshot: page=%d total=%d", len(rows), total)
			break
		}
		for _, row := range rows {
			if row.AuthorID != "101101" || row.DeletionStatus != "active" || row.ViewCount != 0 {
				readErr = fmt.Errorf("other owner/inactive row or view side effect: id=%s", row.ID)
				break
			}
		}
		if readErr != nil {
			break
		}
	}
	if readErr != nil {
		cancel()
	}
	// Join the writer before fixture cleanup, including a failed/cancelled read.
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
