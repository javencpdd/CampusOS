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
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ownerReadPostgresConfig(t *testing.T) *pgxpool.Config {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("CAMPUSOS_COMMUNITY_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("CAMPUSOS_COMMUNITY_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_owner" {
		t.Fatal("owner detail tests require the owned campusos_v12_02b_owner drill database")
	}
	return config
}

func ownerReadPostgres(t *testing.T) (*repository.PgThreadRepository, *pgxpool.Pool, context.Context) {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, ownerReadPostgresConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
DELETE FROM threads WHERE id BETWEEN 99200 AND 99299;
INSERT INTO users (id,username,nickname,email) VALUES
 (99101,'owner-read-owner','Owner','owner-read-owner@test.local'),
 (99102,'owner-read-other','Other','owner-read-other@test.local') ON CONFLICT (id) DO NOTHING;
INSERT INTO categories (id,name,slug,node_kind,lifecycle_status) VALUES
 (99110,'Owner read group','owner-read-group','group','active') ON CONFLICT (id) DO NOTHING;
INSERT INTO categories (id,name,slug,parent_id,node_kind,lifecycle_status) VALUES
 (99111,'Owner read board','owner-read-board',99110,'board','active'),
 (99112,'Owner read other board','owner-read-other-board',99110,'board','active') ON CONFLICT (id) DO NOTHING;
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES
 (99111,'discussion',TRUE),(99112,'discussion',TRUE) ON CONFLICT (category_id,thread_type) DO NOTHING;`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), `
DELETE FROM threads WHERE id BETWEEN 99200 AND 99299;
DELETE FROM category_thread_type_policies WHERE category_id IN (99111,99112);
DELETE FROM categories WHERE id IN (99111,99112);
DELETE FROM categories WHERE id=99110;
DELETE FROM users WHERE id IN (99101,99102);`)
		if err != nil {
			t.Errorf("clean owner read fixtures: %v", err)
		}
	})
	return repository.NewPgThreadRepository(pool), pool, ctx
}

func ownerReadPostgresFixture(t *testing.T, repo repository.ThreadRepository, id string) *domain.Thread {
	t.Helper()
	timestamp := time.Date(2026, 10, 12, 1, 2, 3, 0, time.UTC)
	thread := &domain.Thread{ID: id, AuthorID: "99101", CategoryID: "99111", Title: "owner read title",
		ThreadType: domain.ThreadTypeDiscussion, Content: "private owner body", ContentFormat: "markdown",
		Status: domain.ThreadStatusPrivate, PublicationStatus: domain.PublicationStatusPrivate,
		ModerationStatus: domain.ModerationStatusClear, DeletionStatus: domain.DeletionStatusActive,
		CurrentRevision: 1, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := repo.Create(context.Background(), thread); err != nil {
		t.Fatal(err)
	}
	return thread
}

// This callback is trusted test ingress, rather than proof obtained from HTTP.
// Real user-session/Token revocation is verified by the separate HTTP suite.
func ownerReadPostgresResolver(_ context.Context) (port.PrincipalContext, error) {
	principal := ownerReadPrincipal()
	principal.Actor.ID = "99101"
	return principal, nil
}

func TestOwnerThreadDetailPostgreSQLStateMatrix(t *testing.T) {
	repo, pool, ctx := ownerReadPostgres(t)
	svc := NewThreadService(repo, nil)
	index := 0
	for _, publication := range []string{"draft", "published", "private"} {
		for _, moderation := range []string{"clear", "pending", "rejected", "taken_down"} {
			for _, deletion := range []string{"active", "trashed", "purged"} {
				id := fmt.Sprintf("%d", 99200+index)
				index++
				ownerReadPostgresFixture(t, repo, id)
				if _, err := pool.Exec(ctx, `UPDATE threads SET status='published',
					publication_status=$1,moderation_status=$2,deletion_status=$3 WHERE id=$4`, publication, moderation, deletion, id); err != nil {
					t.Fatal(err)
				}
				got, err := svc.GetThreadForPrincipal(ctx, id, ownerReadPostgresResolver)
				if deletion == "purged" {
					if got != nil || !errors.Is(err, repository.ErrThreadNotFound) {
						t.Fatalf("purged %s/%s returned: %+v %v", publication, moderation, got, err)
					}
				} else {
					if err != nil || got == nil || got.PublicationStatus != domain.PublicationStatus(publication) ||
						got.ModerationStatus != domain.ModerationStatus(moderation) || got.DeletionStatus != domain.DeletionStatus(deletion) {
						t.Fatalf("owner %s/%s/%s snapshot: %+v %v", publication, moderation, deletion, got, err)
					}
				}
				wantViews := int64(0)
				if publication == "published" && moderation == "clear" && deletion == "active" {
					wantViews = 1
				}
				var views int64
				if err := pool.QueryRow(ctx, "SELECT view_count FROM threads WHERE id=$1", id).Scan(&views); err != nil || views != wantViews {
					t.Fatalf("%s/%s/%s views=%d want=%d err=%v", publication, moderation, deletion, views, wantViews, err)
				}
			}
		}
	}
}

type ownerReadPostgresHookRepository struct {
	repository.ThreadRepository
	before       func(int)
	initial      func(*domain.Thread)
	final        func(*domain.Thread)
	readError    error
	recheckError error
	rechecks     int
}

func (r *ownerReadPostgresHookRepository) GetByID(ctx context.Context, id string) (*domain.Thread, error) {
	if r.readError != nil {
		return nil, r.readError
	}
	row, err := r.ThreadRepository.GetByID(ctx, id)
	if err == nil && r.initial != nil {
		r.initial(row)
	}
	return row, err
}

func (r *ownerReadPostgresHookRepository) RecheckOwnerThread(ctx context.Context, id, owner string, version int64) (*domain.Thread, error) {
	r.rechecks++
	if r.before != nil {
		r.before(r.rechecks)
	}
	if r.recheckError != nil {
		return nil, r.recheckError
	}
	row, err := r.ThreadRepository.(repository.OwnerThreadSnapshotRepository).RecheckOwnerThread(ctx, id, owner, version)
	if err == nil && r.final != nil {
		r.final(row)
	}
	return row, err
}

func TestOwnerThreadDetailPostgreSQLCommittedRaces(t *testing.T) {
	ownerReadPostgresConfig(t)
	for _, change := range []string{"owner", "purge", "content", "board", "owner-aba", "content-aba", "state-aba", "trash", "replacement", "becomes-public"} {
		t.Run(change, func(t *testing.T) {
			repo, pool, ctx := ownerReadPostgres(t)
			thread := ownerReadPostgresFixture(t, repo, "99270")
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			switch change {
			case "owner":
				_, err = tx.Exec(ctx, "UPDATE threads SET author_id=99102,content='other owner secret' WHERE id=99270")
			case "purge":
				_, err = tx.Exec(ctx, "UPDATE threads SET deletion_status='purged',deleted_at=NOW(),content='purged secret' WHERE id=99270")
			case "content":
				_, err = tx.Exec(ctx, "UPDATE threads SET content='new owner body' WHERE id=99270")
			case "board":
				_, err = tx.Exec(ctx, "UPDATE threads SET category_id=99112 WHERE id=99270")
			case "owner-aba":
				_, err = tx.Exec(ctx, "UPDATE threads SET author_id=99102 WHERE id=99270")
				if err == nil {
					_, err = tx.Exec(ctx, "UPDATE threads SET author_id=99101 WHERE id=99270")
				}
			case "content-aba":
				_, err = tx.Exec(ctx, "UPDATE threads SET content='temporary owner body' WHERE id=99270")
				if err == nil {
					_, err = tx.Exec(ctx, "UPDATE threads SET content=$1 WHERE id=99270", thread.Content)
				}
			case "state-aba":
				_, err = tx.Exec(ctx, "UPDATE threads SET publication_status='published' WHERE id=99270")
				if err == nil {
					_, err = tx.Exec(ctx, "UPDATE threads SET publication_status='private' WHERE id=99270")
				}
			case "trash":
				_, err = tx.Exec(ctx, "UPDATE threads SET deletion_status='trashed' WHERE id=99270")
			case "replacement":
				_, err = tx.Exec(ctx, "DELETE FROM threads WHERE id=99270")
				if err == nil {
					_, err = tx.Exec(ctx, `INSERT INTO threads(id,thread_type,title,content,content_format,author_id,category_id,status,
						publication_status,moderation_status,deletion_status,current_revision)
						VALUES(99270,'discussion','replacement title','replacement owner body','markdown',99101,99111,'private','private','clear','active',1)`)
				}
			case "becomes-public":
				_, err = tx.Exec(ctx, "UPDATE threads SET publication_status='published' WHERE id=99270")
			}
			if err != nil {
				t.Fatal(err)
			}
			raced := &ownerReadPostgresHookRepository{ThreadRepository: repo}
			raced.before = func(call int) {
				if call == 1 {
					if err := tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}
			}
			recorder := &recordingOwnerPolicy{}
			svc := NewThreadService(raced, nil)
			svc.ownerPolicy = recorder
			got, err := svc.GetThreadForPrincipal(ctx, thread.ID, ownerReadPostgresResolver)
			if change == "owner" || change == "purge" {
				if got != nil || !errors.Is(err, repository.ErrThreadNotFound) || raced.rechecks != 1 {
					t.Fatalf("committed revocation returned content: %+v %v rechecks=%d", got, err, raced.rechecks)
				}
			} else {
				if err != nil || got == nil || raced.rechecks != 2 || len(recorder.requests) != 2 || got.FactsVersion <= thread.FactsVersion {
					t.Fatalf("fresh owner retry: %+v %v rechecks=%d decisions=%d", got, err, raced.rechecks, len(recorder.requests))
				}
				if recorder.requests[0].RequestID == recorder.requests[1].RequestID || recorder.requests[0].Facts.FactsVersion == recorder.requests[1].Facts.FactsVersion {
					t.Fatal("retry reused a decision or ABA fact epoch")
				}
				if change == "content" && got.Content != "new owner body" {
					t.Fatal("content retry returned the stale body")
				}
				if change == "board" && (got.CategoryID != "99112" || recorder.requests[1].Facts.Board.ID != "99112") {
					t.Fatal("retry used the old board facts")
				}
				if change == "trash" && got.DeletionStatus != "trashed" {
					t.Fatal("retry used the old deletion facts")
				}
				if change == "replacement" && got.Content != "replacement owner body" {
					t.Fatal("replacement returned the preceding incarnation")
				}
			}
			var views int64
			if err := pool.QueryRow(ctx, "SELECT view_count FROM threads WHERE id=99270").Scan(&views); err != nil || views != 0 {
				t.Fatalf("owner read changed public views=%d err=%v", views, err)
			}
		})
	}
}

func TestOwnerThreadDetailPostgreSQLFailureBoundaries(t *testing.T) {
	ownerReadPostgresConfig(t)
	for _, fault := range []string{"persistent", "read-error", "recheck-error", "decision", "initial-publication", "initial-moderation", "initial-deletion", "initial-version", "initial-owner", "initial-board", "final-publication", "final-moderation", "final-deletion", "final-version", "final-owner", "final-board"} {
		t.Run(fault, func(t *testing.T) {
			repo, pool, ctx := ownerReadPostgres(t)
			thread := ownerReadPostgresFixture(t, repo, "99280")
			raced := &ownerReadPostgresHookRepository{ThreadRepository: repo}
			switch fault {
			case "persistent":
				raced.before = func(int) {
					if _, err := pool.Exec(ctx, "UPDATE threads SET content=content||'!' WHERE id=99280"); err != nil {
						t.Fatal(err)
					}
				}
			case "read-error":
				raced.readError = errors.New("database read fault with private diagnostic")
			case "recheck-error":
				raced.recheckError = errors.New("database snapshot fault with private diagnostic")
			}
			// Faulty adapter read objects bypass write normalization and retain
			// a contradictory legacy published status. No constraints are removed.
			mutate := func(row *domain.Thread) {
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
			if strings.HasPrefix(fault, "initial-") {
				raced.initial = mutate
			}
			if strings.HasPrefix(fault, "final-") {
				raced.final = mutate
			}
			recorder := &recordingOwnerPolicy{corrupt: fault == "decision"}
			svc := NewThreadService(raced, nil)
			svc.ownerPolicy = recorder
			got, err := svc.GetThreadForPrincipal(ctx, thread.ID, ownerReadPostgresResolver)
			if got != nil || err == nil {
				t.Fatalf("owner read fault returned content: %+v %v", got, err)
			}
			if fault == "persistent" && (!errors.Is(err, port.ErrPolicyFactsChanged) || raced.rechecks != 3 || len(recorder.requests) != 3) {
				t.Fatalf("unbounded conflict: rechecks=%d decisions=%d err=%v", raced.rechecks, len(recorder.requests), err)
			}
			if fault == "decision" && raced.rechecks != 0 {
				t.Fatal("consumed a decision bound to another credential")
			}
			var views int64
			if err := pool.QueryRow(ctx, "SELECT view_count FROM threads WHERE id=99280").Scan(&views); err != nil || views != 0 {
				t.Fatalf("failed owner read changed views=%d err=%v", views, err)
			}
		})
	}
}

func TestOwnerThreadDetailPostgreSQLCountersAndVersion(t *testing.T) {
	repo, pool, ctx := ownerReadPostgres(t)
	thread := ownerReadPostgresFixture(t, repo, "99290")
	initialVersion := thread.FactsVersion
	if _, err := pool.Exec(ctx, `UPDATE threads SET view_count=7,reply_count=3,like_count=2,updated_at=NOW(),facts_version=1 WHERE id=99290`); err != nil {
		t.Fatal(err)
	}
	raced := &ownerReadPostgresHookRepository{ThreadRepository: repo}
	recorder := &recordingOwnerPolicy{}
	svc := NewThreadService(raced, nil)
	svc.ownerPolicy = recorder
	got, err := svc.GetThreadForPrincipal(ctx, thread.ID, ownerReadPostgresResolver)
	if err != nil || got == nil || got.FactsVersion != initialVersion || got.ViewCount != 7 || got.ReplyCount != 3 || got.LikeCount != 2 || raced.rechecks != 1 || len(recorder.requests) != 1 {
		t.Fatalf("counter-only writes invalidated or changed owner snapshot: %+v %v rechecks=%d", got, err, raced.rechecks)
	}
	for _, update := range []string{"content='new owner content'", "author_id=99102", "author_id=99101", "category_id=99112", "publication_status='draft'", "moderation_status='rejected'", "deletion_status='trashed'"} {
		oldVersion := got.FactsVersion
		if _, err := pool.Exec(ctx, "UPDATE threads SET "+update+" WHERE id=99290"); err != nil {
			t.Fatal(err)
		}
		got, err = repo.GetByID(ctx, thread.ID)
		if err != nil || got.FactsVersion <= oldVersion {
			t.Fatalf("fact change did not advance %s: %+v %v", update, got, err)
		}
		if stale, err := repo.RecheckOwnerThread(ctx, thread.ID, "99101", oldVersion); stale != nil || !errors.Is(err, repository.ErrThreadNotFound) {
			t.Fatalf("stale owner snapshot authorized %s: %+v %v", update, stale, err)
		}
	}
	// Direct SQL reads preserve raw explicit states despite a contradictory
	// legacy status. The owner snapshot neither normalizes nor writes the row.
	if _, err := pool.Exec(ctx, "UPDATE threads SET status='published' WHERE id=99290"); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetByID(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err = repo.RecheckOwnerThread(ctx, current.ID, current.AuthorID, current.FactsVersion)
	if err != nil || got == nil || got.Status != "published" || got.PublicationStatus != "draft" || got.ModerationStatus != "rejected" || got.DeletionStatus != "trashed" || got.ViewCount != 7 {
		t.Fatalf("raw owner snapshot defaulted legacy states or wrote counters: %+v %v", got, err)
	}
}

func TestOwnerThreadDetailPostgreSQLPublicBranchNeverFallsBack(t *testing.T) {
	repo, pool, ctx := ownerReadPostgres(t)
	thread := ownerReadPostgresFixture(t, repo, "99291")
	if _, err := pool.Exec(ctx, "UPDATE threads SET publication_status='published' WHERE id=99291"); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "UPDATE threads SET publication_status='private',content='now private owner secret' WHERE id=99291"); err != nil {
		t.Fatal(err)
	}
	raced := &publicReadRaceRepository{ThreadRepository: repo}
	raced.beforeRecheck = func() {
		raced.beforeRecheck = nil
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewThreadService(raced, nil).GetThreadForPrincipal(ctx, thread.ID, ownerReadPostgresResolver)
	if got != nil || !errors.Is(err, repository.ErrThreadNotFound) {
		t.Fatalf("public invocation switched to owner exception: %+v %v", got, err)
	}
	var views int64
	if err := pool.QueryRow(ctx, "SELECT view_count FROM threads WHERE id=99291").Scan(&views); err != nil || views != 0 {
		t.Fatalf("revoked public read changed views=%d err=%v", views, err)
	}
}
