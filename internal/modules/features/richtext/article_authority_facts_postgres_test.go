package richtext

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestArticleAuthorityPostgreSQLCurrentLocksCASAndTypedActor(t *testing.T) {
	s, _, pool, proof, _ := articleCreatePostgres(t, "148201")
	ctx := context.Background()
	req := articleCreateRequest()
	req.CategoryID = "115111"
	draft, err := s.CreateDraftForPrincipal(ctx, "Owner", req, proof)
	if err != nil {
		t.Fatal(err)
	}
	store := s.store.(*PgStore)
	before, err := store.GetArticleByThreadID(ctx, draft.ThreadID)
	if err != nil || before.FactsVersion < 1 || before.UpdatedActorKind != "user" || before.UpdatedActorID != "148201" {
		t.Fatal(before, err)
	}
	if _, err := store.LockArticleAuthority(ctx, draft.ThreadID); !errors.Is(err, ErrArticleFactsUnavailable) {
		t.Fatal(err)
	}
	err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		actual, err := store.LockArticleAuthority(tx, draft.ThreadID)
		if err != nil {
			return err
		}
		_, e := pool.Exec(ctx, `SELECT id FROM richtext_article_contents WHERE thread_id=$1 FOR UPDATE NOWAIT`, draft.ThreadID)
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "55P03" {
			t.Fatal("actual article row not locked", e)
		}
		if err := store.UpdateArticleIfFacts(tx, actual, actual.FactsVersion+1); !errors.Is(err, ErrArticleFactsConflict) {
			t.Fatal("stale CAS accepted", err)
		}
		actual.Summary = "Admin update"
		actual.UpdatedBy = ""
		actual.UpdatedActorKind, actual.UpdatedActorID = "admin", "adm.opaque.article"
		actual.UpdatedAt = time.Now().UTC()
		if err := store.UpdateArticleIfFacts(tx, actual, before.FactsVersion); err != nil {
			return err
		}
		if actual.FactsVersion <= before.FactsVersion {
			t.Fatal(actual)
		}
		current, err := store.LockArticleAuthority(tx, draft.ThreadID)
		if err != nil {
			return err
		}
		if current.UpdatedBy != "" || current.UpdatedActorKind != "admin" || current.UpdatedActorID != "adm.opaque.article" || current.Summary != actual.Summary {
			t.Fatal(current)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	base, _ := store.GetArticleByThreadID(ctx, draft.ThreadID)
	high := base.FactsVersion
	err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		actual, err := store.LockArticleAuthority(tx, draft.ThreadID)
		if err != nil {
			return err
		}
		actual.Summary = "rolled back"
		if err := store.UpdateArticleIfFacts(tx, actual, actual.FactsVersion); err != nil {
			return err
		}
		high = actual.FactsVersion
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatal("rollback ignored")
	}
	actual, _ := store.GetArticleByThreadID(ctx, draft.ThreadID)
	if actual.Summary != base.Summary || actual.FactsVersion != base.FactsVersion || actual.UpdatedActorID != base.UpdatedActorID {
		t.Fatal(actual)
	}
	actual.Summary = "next write"
	if err := store.UpdateArticle(ctx, actual); err != nil || actual.FactsVersion <= high {
		t.Fatal("allocation reused", actual, err)
	}
}

func TestArticleAuthorityPostgreSQLNonceABAAndGuard(t *testing.T) {
	s, _, pool, proof, _ := articleCreatePostgres(t, "148202")
	ctx := context.Background()
	req := articleCreateRequest()
	req.CategoryID = "115111"
	draft, err := s.CreateDraftForPrincipal(ctx, "Owner", req, proof)
	if err != nil {
		t.Fatal(err)
	}
	store := s.store.(*PgStore)
	for _, mode := range []string{"summary", "json", "actor", "deleted_at", "reinsert"} {
		t.Run(mode, func(t *testing.T) {
			before, err := store.GetArticleByThreadID(ctx, draft.ThreadID)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			exec := func(q string, args ...any) {
				if _, err := tx.Exec(ctx, q, args...); err != nil {
					t.Fatal(mode, err)
				}
			}
			switch mode {
			case "summary":
				exec(`UPDATE richtext_article_contents SET summary='changed' WHERE thread_id=$1`, draft.ThreadID)
				exec(`UPDATE richtext_article_contents SET summary=$2 WHERE thread_id=$1`, draft.ThreadID, before.Summary)
			case "json":
				exec(`UPDATE richtext_article_contents SET content_json='{"value":9007199254740993}'::jsonb WHERE thread_id=$1`, draft.ThreadID)
				exec(`UPDATE richtext_article_contents SET content_json=$2::jsonb WHERE thread_id=$1`, draft.ThreadID, string(before.ContentJSON))
			case "actor":
				exec(`UPDATE richtext_article_contents SET updated_by=NULL,updated_actor_kind='admin',updated_actor_id='adm.aba' WHERE thread_id=$1`, draft.ThreadID)
				exec(`UPDATE richtext_article_contents SET updated_by=148202,updated_actor_kind='user',updated_actor_id='148202' WHERE thread_id=$1`, draft.ThreadID)
			case "deleted_at":
				exec(`UPDATE richtext_article_contents SET deleted_at=NOW() WHERE thread_id=$1`, draft.ThreadID)
				exec(`UPDATE richtext_article_contents SET deleted_at=NULL WHERE thread_id=$1`, draft.ThreadID)
			case "reinsert":
				exec(`CREATE TEMP TABLE saved_article ON COMMIT DROP AS SELECT * FROM richtext_article_contents WHERE thread_id=$1`, draft.ThreadID)
				exec(`DELETE FROM richtext_article_contents WHERE thread_id=$1`, draft.ThreadID)
				exec(`INSERT INTO richtext_article_contents SELECT * FROM saved_article`)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			after, err := store.GetArticleByThreadID(ctx, draft.ThreadID)
			if err != nil || after.FactsVersion <= before.FactsVersion {
				t.Fatal("actual ABA not advanced", mode, after, err)
			}
		})
	}
	before, _ := store.GetArticleByThreadID(ctx, draft.ThreadID)
	if _, err := pool.Exec(ctx, `UPDATE richtext_article_contents SET updated_at=NOW(),facts_version=999 WHERE thread_id=$1`, draft.ThreadID); err != nil {
		t.Fatal(err)
	}
	after, _ := store.GetArticleByThreadID(ctx, draft.ThreadID)
	if after.FactsVersion != before.FactsVersion {
		t.Fatal("caller version/timestamp advanced", after)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE richtext_article_contents DISABLE TRIGGER trg_article_facts_version`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), `ALTER TABLE richtext_article_contents ENABLE TRIGGER trg_article_facts_version`); err != nil {
			t.Fatal(err)
		}
	}()
	err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error { _, err := store.LockArticleAuthority(tx, draft.ThreadID); return err })
	if !errors.Is(err, ErrArticleFactsUnavailable) {
		t.Fatal("disabled managed guard accepted", err)
	}
}
