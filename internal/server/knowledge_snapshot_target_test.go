package server

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommunitySourceReadSnapshotPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_SNAPSHOT_DSN")
	if dsn == "" {
		t.Skip("requires owned source snapshot PostgreSQL")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "campusos_v12_10_snapshot" {
		t.Fatal("owned snapshot database required")
	}
	ctx := t.Context()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if _, e = pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email)VALUES(470101,'snapshot-owner','Owner','snapshot@example.test');
 INSERT INTO categories(id,name,slug,node_kind,lifecycle_status)VALUES(470111,'Snapshot','snapshot-board','board','active');
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled)VALUES(470111,'discussion',true);
 INSERT INTO threads(id,title,content,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status)VALUES
 (470201,'Public snapshot','CURRENT_PUBLIC_TEXT',470101,'Owner',470111,'published','discussion','published','clear','active'),
 (470202,'Private snapshot','PRIVATE_SNAPSHOT_SECRET',470101,'Owner',470111,'private','discussion','private','clear','active');`); e != nil {
		t.Fatal(e)
	}
	repo := repository.NewPgThreadRepository(pool)
	cats := repository.NewPgCategoryRepository(pool)
	svc := service.NewThreadService(repo, nil)
	svc.SetCategoryRepository(cats)
	query := community.NewContentQuery(svc)
	snapshot := query.(communityport.SourceReadSnapshot)
	tx := transaction.NewPostgreSQL(pool)
	var escaped context.Context
	if e = tx.Within(ctx, func(c context.Context) error {
		return snapshot.WithSourceReadSnapshot(c, []string{"470202", "470201", "470201"}, func(c context.Context) error {
			escaped = c
			if !query.(communityport.SourceReadPin).SourceReadPinned(c, "470201") {
				t.Fatal("row pin absent inside snapshot")
			}
			row, e := query.GetPublicThread(c, "470201")
			if e != nil || row.Content != "CURRENT_PUBLIC_TEXT" {
				t.Fatal(row, e)
			}
			row.Content = "SPOOF"
			row.PublicationStatus = "private"
			current, e := query.GetPublicThread(c, "470201")
			if e != nil || current.Content != "CURRENT_PUBLIC_TEXT" {
				t.Fatal("detached snapshot mutated", e)
			}
			if row, e := query.GetPublicThread(c, "470202"); e == nil || row != nil {
				t.Fatal("G1 bypassed for batch private row")
			}
			if row, e := repo.RecheckPublicThread(c, "470201", current.FactsVersion+1, false); e == nil || row != nil {
				t.Fatal("version condition bypassed")
			}
			first, e := cats.ListForSource(c)
			if e != nil {
				t.Fatal(e)
			}
			first[0].LifecycleStatus = "archived"
			next, e := cats.ListForSource(c)
			if e != nil || next[0].LifecycleStatus != "active" {
				t.Fatal("category copy mutation")
			}
			fact, _, e := cats.ReadSourceBoardFacts(c, "470111")
			if e != nil || fact.ID != "470111" || fact.LifecycleStatus != "active" || fact.Version < 1 {
				t.Fatal("board facts missing", e)
			}
			fact.LifecycleStatus = "archived"
			fresh, _, e := cats.ReadSourceBoardFacts(c, "470111")
			if e != nil || fresh.LifecycleStatus != "active" {
				t.Fatal("board fact value mutated its owner", e)
			}
			return nil
		})
	}); e != nil {
		t.Fatal(e)
	}
	if row, e := query.GetPublicThread(escaped, "470201"); e == nil || row != nil {
		t.Fatal("escaped committed snapshot released bytes")
	}
	if query.(communityport.SourceReadPin).SourceReadPinned(escaped, "470201") {
		t.Fatal("row pin survived callback")
	}
	if b, _, e := cats.ReadSourceBoardFacts(escaped, "470111"); e == nil || b.ID != "" {
		t.Fatal("board facts escaped transaction")
	}
	// The callback scope ends before the outer transaction, so a write after
	// it cannot reuse the old row even in the same transaction.
	if e = tx.Within(ctx, func(c context.Context) error {
		var old context.Context
		if e := snapshot.WithSourceReadSnapshot(c, []string{"470201"}, func(sc context.Context) error { old = sc; return nil }); e != nil {
			return e
		}
		exec := transaction.ExecutorFor(c, pool)
		if _, e := exec.Exec(c, `UPDATE categories SET lifecycle_status='archived' WHERE id=470111`); e != nil {
			return e
		}
		if b, _, e := cats.ReadSourceBoardFacts(old, "470111"); e != nil || b.LifecycleStatus != "archived" {
			t.Fatal("expired board facts reused", e)
		}
		if _, e := exec.Exec(c, `UPDATE threads SET publication_status='private',status='private' WHERE id=470201`); e != nil {
			return e
		}
		if row, e := query.GetPublicThread(old, "470201"); e == nil || row != nil {
			t.Fatal("expired same-transaction cache reused")
		}
		return errors.New("rollback fixture")
	}); e == nil {
		t.Fatal("rollback missing")
	}
	if row, e := query.GetPublicThread(ctx, "470201"); e != nil || row == nil {
		t.Fatal("rollback did not retain public fact")
	}
	writerDone := make(chan error, 1)
	if e = tx.Within(ctx, func(c context.Context) error {
		return snapshot.WithSourceReadSnapshot(c, []string{"470201"}, func(sc context.Context) error {
			go func() {
				wc, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				_, e := pool.Exec(wc, `UPDATE threads SET publication_status='private',status='private' WHERE id=470201`)
				writerDone <- e
			}()
			select {
			case e := <-writerDone:
				t.Fatal("takedown committed behind pinned source", e)
			case <-time.After(50 * time.Millisecond):
			}
			if row, e := query.GetPublicThread(sc, "470201"); e != nil || row == nil {
				t.Fatal("pinned public read lost")
			}
			return nil
		})
	}); e != nil {
		t.Fatal(e)
	}
	if e := <-writerDone; e != nil {
		t.Fatal(e)
	}
	if row, e := query.GetPublicThread(ctx, "470201"); e == nil || row != nil {
		t.Fatal("fresh request reused pre-takedown allow")
	}
	if _, e = pool.Exec(ctx, `INSERT INTO richtext_article_contents(id,thread_id,title,content_html,status,created_by)VALUES(470301,470201,'Public snapshot','<p>ARTICLE_CURRENT</p>','published',470101)`); e != nil {
		t.Fatal(e)
	}
	articles := richtext.NewPgStore(pool)
	if e = articles.WithPublicTextReadSnapshot(ctx, []string{"470201"}, func(context.Context) error { return nil }); e == nil {
		t.Fatal("unlocked article snapshot")
	}
	if e = tx.Within(ctx, func(c context.Context) error {
		var old context.Context
		if e := articles.WithPublicTextReadSnapshot(c, []string{"470201"}, func(sc context.Context) error {
			old = sc
			a, e := articles.GetArticleByThreadID(sc, "470201")
			if e != nil {
				t.Fatal(e)
			}
			a.ContentHTML = "SPOOF"
			b, e := articles.GetArticleByThreadID(sc, "470201")
			if e != nil || b.ContentHTML != "<p>ARTICLE_CURRENT</p>" {
				t.Fatal("article snapshot copy changed")
			}
			return nil
		}); e != nil {
			return e
		}
		if _, e := transaction.ExecutorFor(c, pool).Exec(c, `UPDATE richtext_article_contents SET content_html='<p>ARTICLE_CHANGED</p>' WHERE id=470301`); e != nil {
			return e
		}
		a, e := articles.GetArticleByThreadID(old, "470201")
		if e != nil || a.ContentHTML != "<p>ARTICLE_CHANGED</p>" {
			t.Fatal("expired article snapshot reused")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
