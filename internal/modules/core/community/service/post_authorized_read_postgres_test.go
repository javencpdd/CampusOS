package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
)

func TestPostReadPostgreSQLAtomicCountPageAndParentScope(t *testing.T) {
	s, pool, parent, proof, _ := postAuthorPostgres(t, "136201")
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := s.CreatePostForPrincipal(ctx, parent.ID, "Owner", domain.CreatePostRequest{Content: fmt.Sprint("reply-", i)}, proof); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO posts(id,thread_id,author_id,author_name,content,status,floor_number) VALUES(136301,$1,136201,'Owner','must not count','deleted',10),(136302,$1,136201,'Owner','deleted must not count','published',11)`, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE posts SET deleted_at=NOW() WHERE id=136302`); err != nil {
		t.Fatal(err)
	}
	rows, total, err := s.ListPostsForPrincipal(ctx, parent.ID, 2, 1, proof)
	if err != nil || total != 3 || len(rows) != 1 || rows[0].Content != "reply-1" || rows[0].FactsVersion < 1 {
		t.Fatal(rows, total, err)
	}
	if rows, total, err := s.ListPostsForPrincipal(ctx, parent.ID, 99, 1, proof); err != nil || total != 3 || len(rows) != 0 {
		t.Fatal(rows, total, err)
	}
	if rows, total, err := s.ListPostsForPrincipal(ctx, parent.ID, 1, 20, nil); !errors.Is(err, repository.ErrThreadNotFound) || rows != nil || total != 0 {
		t.Fatal("anonymous read private parent", rows, total, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE threads SET publication_status='published',moderation_status='clear' WHERE id=$1`, parent.ID); err != nil {
		t.Fatal(err)
	}
	rows, total, err = s.ListPostsForPrincipal(ctx, parent.ID, 1, 20, nil)
	if err != nil || total != 3 || len(rows) != 3 {
		t.Fatal(rows, total, err)
	}
	if p, err := s.GetPostForPrincipal(ctx, parent.ID, rows[0].ID, nil); err != nil || p == nil || p.ID != rows[0].ID {
		t.Fatal(p, err)
	}
	actual, _ := s.threadRepo.GetByID(ctx, parent.ID)
	raw := s.repo.(*repository.PgPostRepository)
	if rows, total, err := raw.ReadPublishedPostSnapshot(ctx, parent.ID, actual.FactsVersion-1, "", "", 1, 20); !errors.Is(err, repository.ErrThreadNotFound) || rows != nil || total != 0 {
		t.Fatal("stale parent allowed count", rows, total, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE threads SET deletion_status='trashed' WHERE id=$1`, parent.ID); err != nil {
		t.Fatal(err)
	}
	if rows, total, err := s.ListPostsForPrincipal(ctx, parent.ID, 99, 1, proof); !errors.Is(err, repository.ErrThreadNotFound) || rows != nil || total != 0 {
		t.Fatal("trash exposed empty count", rows, total, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE threads SET deletion_status='active' WHERE id=$1`, parent.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"posts", "threads"} {
		trigger := "trg_posts_facts_version"
		if table == "threads" {
			trigger = "trg_threads_facts_version"
		}
		if _, err := pool.Exec(ctx, "ALTER TABLE "+table+" DISABLE TRIGGER "+trigger); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pool.Exec(context.Background(), "ALTER TABLE "+table+" ENABLE TRIGGER "+trigger) })
		rows, total, err := s.ListPostsForPrincipal(ctx, parent.ID, 99, 1, proof)
		if !errors.Is(err, repository.ErrPostFactsUnavailable) || rows != nil || total != 0 {
			t.Fatal("disabled authority guard returned count", table, rows, total, err)
		}
		if _, err := pool.Exec(ctx, "ALTER TABLE "+table+" ENABLE TRIGGER "+trigger); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostReadPostgreSQLCommittedWholePageChanges(t *testing.T) {
	modes := []string{"parent_private", "parent_aba", "parent_board", "post_aba", "post_parent", "post_owner", "post_metadata", "member", "offpage", "order", "status", "session_revoke", "user_auth", "strength", "empty_parent", "empty_member", "empty_session", "counters"}
	for i, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			s, pool, parent, proof, claims := postAuthorPostgres(t, fmt.Sprint(136210+i))
			ctx := context.Background()
			post, err := s.CreatePostForPrincipal(ctx, parent.ID, "Owner", domain.CreatePostRequest{Content: "original reply"}, proof)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE threads SET publication_status='published',moderation_status='clear' WHERE id=$1`, parent.ID); err != nil {
				t.Fatal(err)
			}
			raw := s.repo.(*repository.PgPostRepository)
			probe := &postReadProbe{PostRepository: raw, snapshots: raw, after: func() {
				exec := func(query string, args ...any) {
					if _, err := pool.Exec(ctx, query, args...); err != nil {
						t.Fatal(mode, err)
					}
				}
				switch mode {
				case "parent_private", "empty_parent":
					exec(`UPDATE threads SET publication_status='private' WHERE id=$1`, parent.ID)
				case "parent_aba":
					exec(`UPDATE threads SET content='temporary' WHERE id=$1`, parent.ID)
					exec(`UPDATE threads SET content=$2 WHERE id=$1`, parent.ID, parent.Content)
				case "parent_board":
					exec(`UPDATE threads SET category_id=113112 WHERE id=$1`, parent.ID)
				case "post_aba":
					exec(`UPDATE posts SET content='temporary' WHERE id=$1`, post.ID)
					exec(`UPDATE posts SET content=$2 WHERE id=$1`, post.ID, post.Content)
				case "post_parent":
					exec(`UPDATE posts SET thread_id=119201 WHERE id=$1`, post.ID)
				case "post_owner":
					exec(`UPDATE posts SET author_id=119101 WHERE id=$1`, post.ID)
				case "post_metadata":
					exec(`UPDATE posts SET metadata='{"changed":true}'::jsonb WHERE id=$1`, post.ID)
				case "member", "empty_member":
					exec(`UPDATE posts SET deleted_at=NOW() WHERE id=$1`, post.ID)
				case "offpage":
					exec(`INSERT INTO posts(id,thread_id,author_id,author_name,content,status,floor_number) VALUES($1,$2,$3,'Owner','offpage','published',10)`, fmt.Sprint(136410+i), parent.ID, claims.UserID)
				case "order":
					exec(`UPDATE posts SET floor_number=9 WHERE id=$1`, post.ID)
				case "status":
					exec(`UPDATE posts SET status='deleted' WHERE id=$1`, post.ID)
				case "session_revoke", "empty_session":
					exec(`UPDATE sessions SET revoked_at=NOW() WHERE id=$1`, claims.SessionID)
				case "user_auth":
					exec(`UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, claims.UserID)
				case "strength":
					exec(`UPDATE sessions SET authentication_strength='mfa',mfa_authenticated_at=NOW() WHERE id=$1`, claims.SessionID)
				case "counters":
					exec(`UPDATE posts SET like_count=9,updated_at=NOW() WHERE id=$1`, post.ID)
				}
			}}
			s.repo = probe
			page := 1
			if mode == "empty_parent" || mode == "empty_member" || mode == "empty_session" {
				page = 99
			}
			rows, total, err := s.ListPostsForPrincipal(ctx, parent.ID, page, 1, proof)
			if mode == "counters" {
				if err != nil || total != 1 || len(rows) != 1 || rows[0].LikeCount != 9 {
					t.Fatal(rows, total, err)
				}
				return
			}
			if err == nil || rows != nil || total != 0 {
				t.Fatal("committed mutation returned whole page", rows, total, err)
			}
		})
	}
}
