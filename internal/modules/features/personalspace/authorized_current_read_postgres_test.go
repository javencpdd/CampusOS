package space

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

var currentSpaceReadPaths = []string{"profile_id", "profile_name", "contents_id", "contents_name", "owner", "sync"}

func runCurrentSpaceRead(ctx context.Context, s *Service, owner, op string, proof identityport.SessionPrincipalResolver) (any, error) {
	var result any
	var err error
	switch op {
	case "profile_id":
		result, err = s.PublicProfileByUserID(ctx, owner)
	case "profile_name":
		result, err = s.PublicProfileByUsername(ctx, "files-"+owner)
	case "contents_id":
		var rows []*SpaceContent
		var total int64
		rows, total, err = s.ListCurrentPublicContentsByUserID(ctx, owner, 1, 20)
		result = currentSpaceContentResult{rows, total}
	case "contents_name":
		var rows []*SpaceContent
		var total int64
		rows, total, err = s.ListCurrentPublicContentsByUsername(ctx, "files-"+owner, 1, 20)
		result = currentSpaceContentResult{rows, total}
	case "owner":
		var rows []*SpaceContent
		var total int64
		rows, total, err = s.ListOwnContentsForPrincipal(ctx, proof, 1, 20)
		result = currentSpaceContentResult{rows, total}
	case "sync":
		result, err = s.GetSyncStatusForPrincipal(ctx, proof)
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}
func currentSpacePGThreads(t *testing.T, s *Service, pool *pgxpool.Pool, owner string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	cat := owner + "2"
	if err := communityrepo.NewPgCategoryRepository(pool).Create(ctx, &communitydomain.Category{ID: cat, Name: cat, Slug: "space-read-" + cat, DefaultTags: []string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := communityrepo.NewPgThreadTypePolicyRepository(pool).Replace(ctx, cat, []communitydomain.ThreadType{communitydomain.ThreadTypeDiscussion}); err != nil {
		t.Fatal(err)
	}
	repo := communityrepo.NewPgThreadRepository(pool)
	for i, pub := range []communitydomain.PublicationStatus{"published", "private", "draft"} {
		row := &communitydomain.Thread{ID: fmt.Sprint(mustCurrentOwnerNumber(t, owner)*10 + int64(i) + 3), AuthorID: owner, CategoryID: cat, ThreadType: communitydomain.ThreadTypeDiscussion, Title: "actual current " + string(pub), Content: "PG_PRIVATE_" + string(pub), PublicationStatus: pub, ModerationStatus: "clear", DeletionStatus: "active", ModerationReason: "PG_INTERNAL_REASON", CreatedAt: now, UpdatedAt: now}
		if err := repo.Create(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	s.SetContentQuery(community.NewContentQuery(communityservice.NewThreadService(repo, nil)))
}
func mustCurrentOwnerNumber(t *testing.T, owner string) int64 {
	t.Helper()
	var n int64
	if _, err := fmt.Sscan(owner, &n); err != nil {
		t.Fatal(err)
	}
	return n
}
func currentSpacePGState(t *testing.T, pool *pgxpool.Pool, owner, session string) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(context.Background(), `SELECT jsonb_build_object('user',(SELECT to_jsonb(u)FROM users u WHERE id=$1),'threads',(SELECT jsonb_agg(to_jsonb(t)ORDER BY id)FROM threads t WHERE author_id=$1),'contents',(SELECT jsonb_agg(to_jsonb(c)ORDER BY id)FROM user_space_contents c WHERE user_id=$1))::text`, owner).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state + spaceStylePGState(t, pool, owner, session)
}
func TestSpaceCurrentReadPostgreSQLActualSixPathsDefaultCurrentStatesCorePoliciesAndNoWrites(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, op := range currentSpaceReadPaths {
		for _, mode := range []string{"default", "existing", "private", "unlisted", "disabled", "inactive", "sync_off"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				index++
				owner := fmt.Sprint(2610201 + index*10)
				s, repo, pool, proof, claims := spaceOwnerPG(t, owner, mode != "default")
				currentSpacePGThreads(t, s, pool, owner)
				if mode != "default" {
					row, err := repo.GetByUserID(ctx, owner)
					if err != nil {
						t.Fatal(err)
					}
					row.LastSyncError = "/HOST_PRIVATE_PATH"
					switch mode {
					case "private":
						row.Visibility = VisibilityPrivate
					case "unlisted":
						row.Visibility = VisibilityUnlisted
					case "disabled":
						at := time.Now()
						row.DisabledAt = &at
					case "sync_off":
						row.SyncEnabled = false
					}
					if err := repo.Upsert(ctx, row); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "inactive" {
					if _, err := pool.Exec(ctx, "UPDATE users SET status='suspended' WHERE id=$1", owner); err != nil {
						t.Fatal(err)
					}
				}
				before := currentSpacePGState(t, pool, owner, claims.SessionID)
				result, err := runCurrentSpaceRead(ctx, s, owner, op, proof)
				public := op != "owner" && op != "sync"
				allow := mode != "inactive" && (!public || (mode != "private" && mode != "unlisted" && mode != "disabled"))
				if (err == nil) != allow {
					t.Fatal(op, mode, result, err)
				}
				if !allow && result != nil {
					t.Fatal("partial result")
				}
				if allow {
					raw, _ := json.Marshal(result)
					for _, secret := range []string{"/HOST_PRIVATE_PATH", "facts_version", "disabled_actor", "sync_categories"} {
						if strings.Contains(string(raw), secret) {
							t.Fatal("private response", string(raw))
						}
					}
					if public && (strings.Contains(string(raw), "PG_PRIVATE_private") || strings.Contains(string(raw), "PG_INTERNAL_REASON")) {
						t.Fatal("public data leak", string(raw))
					}
					if page, ok := result.(currentSpaceContentResult); ok {
						want := int64(1)
						if op == "owner" {
							want = 3
						}
						if mode == "sync_off" {
							want = 0
						}
						if page.Total != want || int64(len(page.Items)) != want {
							t.Fatal("Core predicate did not precede count/page", page, want)
						}
					}
				}
				if after := currentSpacePGState(t, pool, owner, claims.SessionID); after != before {
					t.Fatal("read mutated actual data")
				}
			})
		}
	}
}

type currentSpacePGHook struct {
	*PgRepository
	after func()
}

func (r *currentSpacePGHook) LoadSpaceAuthority(ctx context.Context, owner string, lock bool) (json.RawMessage, error) {
	raw, err := r.PgRepository.LoadSpaceAuthority(ctx, owner, lock)
	if r.after != nil {
		f := r.after
		r.after = nil
		f()
	}
	return raw, err
}
func TestSpaceCurrentReadPostgreSQLActualCommittedChangesGuardDefaultAndWholeCorePageClose(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, op := range currentSpaceReadPaths {
		modes := []string{"space_aba", "email", "name", "inactive", "default_created", "guard", "feature"}
		if op == "owner" || op == "sync" {
			modes = append(modes, "session")
		}
		if strings.Contains(op, "contents") || op == "owner" {
			modes = append(modes, "core_page", "core_empty_count")
		}
		for _, mode := range modes {
			t.Run(op+"/"+mode, func(t *testing.T) {
				index++
				owner := fmt.Sprint(2620201 + index*10)
				s, repo, pool, proof, claims := spaceOwnerPG(t, owner, mode != "default_created")
				currentSpacePGThreads(t, s, pool, owner)
				ran := false
				afterMutation := ""
				mutate := func() {
					ran = true
					var err error
					switch mode {
					case "space_aba":
						var title string
						err = pool.QueryRow(ctx, "SELECT title FROM user_spaces WHERE user_id=$1", owner).Scan(&title)
						if err == nil {
							_, err = pool.Exec(ctx, "UPDATE user_spaces SET title='intermediate' WHERE user_id=$1", owner)
						}
						if err == nil {
							_, err = pool.Exec(ctx, "UPDATE user_spaces SET title=$2 WHERE user_id=$1", owner, title)
						}
					case "email":
						_, err = pool.Exec(ctx, "UPDATE users SET email=$2 WHERE id=$1", owner, owner+"changed@example.test")
					case "name":
						_, err = pool.Exec(ctx, "UPDATE users SET username=$2 WHERE id=$1", owner, "renamed-"+owner)
					case "inactive":
						_, err = pool.Exec(ctx, "UPDATE users SET status='suspended' WHERE id=$1", owner)
					case "default_created":
						u, e := s.users.GetByID(ctx, owner)
						if e != nil {
							t.Fatal(e)
						}
						row := defaultSpace(u)
						row.ID = owner + "1"
						err = repo.Upsert(ctx, row)
					case "guard":
						_, err = pool.Exec(ctx, "ALTER TABLE user_spaces DISABLE TRIGGER trg_user_spaces_facts_version")
						t.Cleanup(func() {
							if _, e := pool.Exec(context.Background(), "ALTER TABLE user_spaces ENABLE TRIGGER trg_user_spaces_facts_version"); e != nil {
								t.Error(e)
							}
						})
					case "feature":
						s.SetPluginEnabledChecker(func() bool { return false })
					case "session":
						_, err = pool.Exec(ctx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='current space read' WHERE id=$1", claims.SessionID)
					case "core_page":
						_, err = pool.Exec(ctx, "UPDATE threads SET title='changed after Core protected page' WHERE author_id=$1", owner)
					case "core_empty_count":
						_, err = pool.Exec(ctx, "UPDATE threads SET deletion_status='trashed' WHERE author_id=$1", owner)
					}
					if err != nil {
						t.Fatal("mutation did not commit", mode, err)
					}
					afterMutation = currentSpacePGState(t, pool, owner, claims.SessionID)
				}
				if strings.HasPrefix(mode, "core_") {
					s.SetContentQuery(&currentReadQueryHook{ContentQuery: s.contentQuery, after: mutate})
				} else {
					s.repo = &currentSpacePGHook{PgRepository: repo, after: mutate}
				}
				result, err := runCurrentSpaceRead(ctx, s, owner, op, proof)
				if !ran || err == nil || result != nil {
					t.Fatal("committed source change accepted", op, mode, result, err, ran)
				}
				if currentSpacePGState(t, pool, owner, claims.SessionID) != afterMutation {
					t.Fatal("rejected read wrote data")
				}
			})
		}
	}
}
