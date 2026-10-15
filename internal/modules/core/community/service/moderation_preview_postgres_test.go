package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
)

type moderationPreviewPG struct {
	takeDownPG
	posts   *repository.PgPostRepository
	service *PostService
	ids     []string
}

func moderationPreviewPostgres(t *testing.T, seed int) moderationPreviewPG {
	t.Helper()
	g := userFlagPostgres(t, seed)
	ctx := context.Background()
	grants := identityrepo.NewPgDelegationRepository(g.pool)
	g.grant = fmt.Sprintf("grant.preview.%d", seed)
	now := time.Now().UTC()
	if err := grants.InsertDelegations(ctx, []identityrepo.Delegation{{ID: g.grant, Kind: "grant", SubjectKind: "user", SubjectID: g.claims.UserID, Action: "community.post.delete", BoardID: g.row.CategoryID, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	posts := repository.NewPgPostRepository(g.pool)
	ids := []string{fmt.Sprint(seed + 5000), fmt.Sprint(seed + 5001)}
	for _, id := range ids {
		if err := posts.Create(ctx, &domain.Post{ID: id, ThreadID: g.row.ID, AuthorID: g.row.AuthorID, Content: "private preview reply", Status: "published"}); err != nil {
			t.Fatal(err)
		}
	}
	s := NewPostService(posts, nil)
	s.SetThreadRepository(g.threads)
	s.SetThreadCommandSource(g.s)
	s.SetGovernanceAuthority(identityservice.NewBoardGovernanceAuthorityService(grants, nil, nil))
	return moderationPreviewPG{g, posts, s, ids}
}

func TestModerationPreviewPostgreSQLActualBatchEmptyAndNoMutation(t *testing.T) {
	g := moderationPreviewPostgres(t, 155201)
	ctx := context.Background()
	for _, ids := range [][]string{nil, g.ids} {
		before, _ := g.threads.GetByID(ctx, g.row.ID)
		result, err := g.service.PreviewUserModeration(ctx, g.row.ID, ids, g.proof, previewConfig)
		if err != nil || result == nil || !result.Pin || !result.Lock || len(result.Posts) != len(ids) {
			t.Fatal(result, err)
		}
		for _, row := range result.Posts {
			if !row.Delete {
				t.Fatal(row)
			}
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "facts_version") || strings.Contains(string(raw), g.grant) {
			t.Fatal("private snapshot leaked", string(raw))
		}
		after, _ := g.threads.GetByID(ctx, g.row.ID)
		if !sameGovernanceThread(before, after) || before.ViewCount != after.ViewCount || before.ReplyCount != after.ReplyCount {
			t.Fatal("preview mutated actual parent", before, after)
		}
		if counts := takeDownCounts(t, g.takeDownPG, g.row.ID); !reflect.DeepEqual(counts, []int{0, 0, 0, 0, 0}) {
			t.Fatal("preview wrote history/audit/outbox", counts)
		}
	}
	// The returned preview is not a write capability. Revoking the actual
	// scoped role denies execution immediately despite a previously true flag.
	if _, err := g.pool.Exec(ctx, `UPDATE user_roles SET deleted_at=NOW() WHERE role_id=$1`, g.role); err != nil {
		t.Fatal(err)
	}
	if result, err := g.s.UserSetThreadFlag(ctx, g.row.ID, "pin", g.proof, userFlagParticipant{}); err == nil || result != nil {
		t.Fatal("preview was reused as execution authority", result, err)
	}
}

type moderationPreviewPgProbe struct {
	*repository.PgPostRepository
	reads int
	hook  func(int)
}

func (r *moderationPreviewPgProbe) ReadPostCommandSnapshot(ctx context.Context, id string) (*domain.Post, error) {
	r.reads++
	row, err := r.PgPostRepository.ReadPostCommandSnapshot(ctx, id)
	if r.hook != nil {
		r.hook(r.reads)
	}
	return row, err
}

func TestModerationPreviewPostgreSQLCommittedWholeEmptyAndGuardChanges(t *testing.T) {
	for i, mode := range []string{"post_aba", "parent_aba", "scope_aba", "permission_aba", "grant_revoke", "session_revoke", "empty_role_aba", "parent_guard", "post_guard", "config", "counters"} {
		t.Run(mode, func(t *testing.T) {
			g := moderationPreviewPostgres(t, 155301+i*10)
			ctx := context.Background()
			ids := g.ids
			changed := false
			mutate := func() {
				if changed {
					return
				}
				changed = true
				tx, err := g.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				update := func(sql string, args ...any) {
					if _, err := tx.Exec(ctx, sql, args...); err != nil {
						t.Fatal(mode, err)
					}
				}
				switch mode {
				case "post_aba":
					update(`UPDATE posts SET content='temporary' WHERE id=$1`, g.ids[0])
					update(`UPDATE posts SET content='private preview reply' WHERE id=$1`, g.ids[0])
				case "parent_aba":
					update(`UPDATE threads SET content='temporary' WHERE id=$1`, g.row.ID)
					update(`UPDATE threads SET content=$2 WHERE id=$1`, g.row.ID, g.row.Content)
				case "scope_aba":
					update(`UPDATE user_roles SET scope_id=113112 WHERE role_id=$1`, g.role)
					update(`UPDATE user_roles SET scope_id=113111 WHERE role_id=$1`, g.role)
				case "permission_aba", "empty_role_aba":
					update(`UPDATE role_permissions SET deleted_at=NOW() WHERE role_id=$1`, g.role)
					update(`UPDATE role_permissions SET deleted_at=NULL WHERE role_id=$1`, g.role)
				case "grant_revoke":
					update(`UPDATE identity_delegations SET status='revoked',revoked_at=NOW(),version=version+1 WHERE id=$1`, g.grant)
				case "session_revoke":
					update(`UPDATE sessions SET revoked_at=NOW(),revoke_reason='test' WHERE id=$1`, g.claims.SessionID)
				case "parent_guard":
					update(`ALTER TABLE threads DISABLE TRIGGER trg_threads_facts_version`)
					t.Cleanup(func() {
						if _, err := g.pool.Exec(context.Background(), `ALTER TABLE threads ENABLE TRIGGER trg_threads_facts_version`); err != nil {
							t.Error(err)
						}
					})
				case "post_guard":
					update(`ALTER TABLE posts DISABLE TRIGGER trg_posts_facts_version`)
					t.Cleanup(func() {
						if _, err := g.pool.Exec(context.Background(), `ALTER TABLE posts ENABLE TRIGGER trg_posts_facts_version`); err != nil {
							t.Error(err)
						}
					})
				case "counters":
					update(`UPDATE threads SET view_count=17 WHERE id=$1`, g.row.ID)
					update(`UPDATE posts SET like_count=19 WHERE id=$1`, g.ids[0])
				}
				if err := tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			config := previewConfig
			if mode == "empty_role_aba" {
				ids = nil
				calls := 0
				config = func(context.Context) (communityport.ModerationPreviewConfig, error) {
					calls++
					if calls == 2 {
						mutate()
					}
					return previewConfig(ctx)
				}
			} else {
				g.service.repo = &moderationPreviewPgProbe{PgPostRepository: g.posts, hook: func(read int) {
					if read == 2 {
						mutate()
					}
				}}
				if mode == "config" {
					config = func(context.Context) (communityport.ModerationPreviewConfig, error) {
						if changed {
							return communityport.ModerationPreviewConfig{}, nil
						}
						return previewConfig(ctx)
					}
				}
			}
			result, err := g.service.PreviewUserModeration(ctx, g.row.ID, ids, g.proof, config)
			if mode == "counters" {
				if err != nil || result == nil || !result.Pin || !result.Posts[0].Delete {
					t.Fatal(result, err)
				}
				return
			}
			if err == nil || result != nil {
				t.Fatal("committed full/empty preview drift accepted", mode, result, err)
			}
		})
	}
}
