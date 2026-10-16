package space

import (
	"context"
	"errors"
	"reflect"
	"testing"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type currentReadQueryHook struct {
	communityport.ContentQuery
	after func()
}

func (q *currentReadQueryHook) finish() {
	if q.after != nil {
		f := q.after
		q.after = nil
		f()
	}
}
func (q *currentReadQueryHook) ListPublicThreads(ctx context.Context, f communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error) {
	rows, total, err := q.ContentQuery.ListPublicThreads(ctx, f)
	q.finish()
	return rows, total, err
}
func (q *currentReadQueryHook) ListAuthorThreadsForPrincipal(ctx context.Context, p func(context.Context) (identityport.PrincipalContext, error), f communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error) {
	rows, total, err := q.ContentQuery.(communityport.AuthenticatedAuthorContentQuery).ListAuthorThreadsForPrincipal(ctx, p, f)
	q.finish()
	return rows, total, err
}
func currentReadContentService(t *testing.T) (*Service, *ownContentsHTTPEnvironment) {
	t.Helper()
	e := newOwnContentsHTTPEnvironment(t)
	if err := e.threads.Create(context.Background(), &communitydomain.Thread{ID: "98443", AuthorID: e.owner.ID, CategoryID: "98451", Title: "public title", Content: "public body", PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "active", ModerationReason: "PRIVATE GOVERNANCE", Tags: []string{"campus"}}); err != nil {
		t.Fatal(err)
	}
	s := NewService(e.spaces, newFakeUserLookup(e.owner, e.other))
	s.SetContentQuery(e.query)
	return s, e
}
func runCurrentContentRead(ctx context.Context, s *Service, owner, name, op string, p identityport.SessionPrincipalResolver, page, size int) ([]*SpaceContent, int64, error) {
	switch op {
	case "id":
		return s.ListCurrentPublicContentsByUserID(ctx, owner, page, size)
	case "name":
		return s.ListCurrentPublicContentsByUsername(ctx, name, page, size)
	default:
		return s.ListOwnContentsForPrincipal(ctx, p, page, size)
	}
}
func TestSpaceCurrentReadContentsActualCorePoliciesFiltersCountAndNoWrites(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"id", "name", "owner"} {
		for _, existing := range []bool{false, true} {
			t.Run(op+"/"+map[bool]string{false: "default", true: "existing"}[existing], func(t *testing.T) {
				s, e := currentReadContentService(t)
				proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
					return identityport.ResolveUserSessionPrincipal(ctx, e.identity, e.identity, mustCurrentReadClaims(t, e))
				}
				if existing {
					row := defaultSpace(e.owner)
					row.ID = "98461"
					row.SyncCategories = []string{"98451"}
					row.SyncTags = []string{"campus"}
					if err := e.spaces.Upsert(ctx, row); err != nil {
						t.Fatal(err)
					}
				}
				before := e.spaces.Snapshot()
				rows, total, err := runCurrentContentRead(ctx, s, e.owner.ID, e.owner.Username, op, proof, 1, 20)
				want := int64(1)
				if op == "owner" && !existing {
					want = 2
				}
				if err != nil || total != want || len(rows) != int(want) {
					t.Fatal(op, existing, rows, total, err)
				}
				for _, row := range rows {
					if row.UserID != e.owner.ID || op != "owner" && (row.PublicationStatus != "published" || row.ModerationStatus != "clear" || row.ModerationReason != "") {
						t.Fatal("unsafe projection", row)
					}
				}
				rows, total, err = runCurrentContentRead(ctx, s, e.owner.ID, e.owner.Username, op, proof, 9, 1)
				if err != nil || rows == nil || len(rows) != 0 || total != want {
					t.Fatal("empty page lost current filtered total", rows, total, err)
				}
				if !reflect.DeepEqual(before, e.spaces.Snapshot()) {
					t.Fatal("content read wrote Space")
				}
			})
		}
	}
}
func TestSpaceCurrentReadContentsWholePageEmptyPreferencesUserAndSourceChangesClose(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"id", "name", "owner"} {
		for _, mode := range []string{"preferences", "space_aba", "disabled", "user_email", "rename", "inactive", "feature", "core_row", "empty_count", "view_counter"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				s, e := currentReadContentService(t)
				row := defaultSpace(e.owner)
				row.ID = "98461"
				if err := e.spaces.Upsert(ctx, row); err != nil {
					t.Fatal(err)
				}
				claims := mustCurrentReadClaims(t, e)
				proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
					return identityport.ResolveUserSessionPrincipal(ctx, e.identity, e.identity, claims)
				}
				hook := &currentReadQueryHook{ContentQuery: e.query}
				hook.after = func() {
					var err error
					switch mode {
					case "preferences":
						row.SyncTags = []string{"different"}
						err = e.spaces.Upsert(ctx, row)
					case "space_aba":
						old := row.Title
						row.Title = "intermediate"
						err = e.spaces.Upsert(ctx, row)
						row.Title = old
						if err == nil {
							err = e.spaces.Upsert(ctx, row)
						}
					case "disabled":
						row.SyncEnabled = false
						err = e.spaces.Upsert(ctx, row)
					case "user_email":
						s.users.(*fakeUserLookup).byID[e.owner.ID].Email = "changed@example.test"
					case "rename":
						s.users.(*fakeUserLookup).byID[e.owner.ID].Username = "renamed"
					case "inactive":
						s.users.(*fakeUserLookup).byID[e.owner.ID].Status = "suspended"
					case "feature":
						s.SetPluginEnabledChecker(func() bool { return false })
					case "core_row":
						v, _ := e.threads.GetByID(ctx, "98443")
						v.Title = "changed after first protected query"
						err = e.threads.Update(ctx, v)
					case "empty_count":
						err = e.threads.Create(ctx, &communitydomain.Thread{ID: "98444", AuthorID: e.owner.ID, CategoryID: "98451", Title: "new public member", Content: "new body", PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "active"})
					case "view_counter":
						_, err = e.threads.IncrementViewCount(ctx, "98443")
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				s.SetContentQuery(hook)
				page := 1
				if mode == "empty_count" {
					page = 9
				}
				rows, total, err := runCurrentContentRead(ctx, s, e.owner.ID, e.owner.Username, op, proof, page, 20)
				if mode == "view_counter" {
					if err != nil || len(rows) == 0 {
						t.Fatal("counter is not an authorization fact", err)
					}
					return
				}
				if err == nil || rows != nil || total != 0 {
					t.Fatal("changed current graph accepted", mode, rows, total, err)
				}
			})
		}
	}
	for _, op := range []string{"id", "name", "owner"} {
		s, e := currentReadContentService(t)
		s.contentQuery = nil
		rows, total, err := runCurrentContentRead(ctx, s, e.owner.ID, e.owner.Username, op, ownContentsResolver, 1, 20)
		if err == nil || rows != nil || total != 0 {
			t.Fatal("missing port fell back", op, err)
		}
	}
	s, _ := currentReadContentService(t)
	s.users = nil
	if _, _, err := s.ListCurrentPublicContentsByUserID(ctx, "98431", 1, 20); !errors.Is(err, ErrSpaceFactsUnavailable) {
		t.Fatal(err)
	}
}
