package space

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type currentSyncCountHook struct {
	*MemoryRepository
	after func()
	fail  bool
}

func (r *currentSyncCountHook) CountContentsByUserID(ctx context.Context, id string) (int64, error) {
	if r.fail {
		return 0, errors.New("/private/database/count-error")
	}
	count, err := r.MemoryRepository.CountContentsByUserID(ctx, id)
	if r.after != nil {
		f := r.after
		r.after = nil
		f()
	}
	return count, err
}
func TestSpaceCurrentReadSyncCurrentDefaultDisabledSafeErrorCountAndNoWrites(t *testing.T) {
	ctx := context.Background()
	for _, existing := range []bool{false, true} {
		for _, mode := range []string{"current", "disabled", "missing", "admin", "session", "count_error", "count_changed", "space_aba", "inactive"} {
			t.Run(map[bool]string{true: "existing/", false: "default/"}[existing]+mode, func(t *testing.T) {
				s, repo, _ := spaceOwnerFixture(t, existing, "")
				proof := identityport.SessionPrincipalResolver(spaceOwnerProof)
				if existing {
					row, _ := repo.GetByUserID(ctx, "1001")
					row.LastSyncError = "/private/host/path"
					if mode == "disabled" {
						at := time.Now()
						row.DisabledAt = &at
					}
					if err := repo.Upsert(ctx, row); err != nil {
						t.Fatal(err)
					}
				}
				hook := &currentSyncCountHook{MemoryRepository: repo}
				s.repo = hook
				switch mode {
				case "missing":
					proof = nil
				case "admin":
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, e := spaceOwnerProof(ctx)
						p.Actor.Kind = "admin"
						p.Audience = "admin"
						return p, e
					}
				case "session":
					calls := 0
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						calls++
						p, e := spaceOwnerProof(ctx)
						if calls >= 2 {
							p.CredentialID = "changed"
						}
						return p, e
					}
				case "count_error":
					hook.fail = true
				case "count_changed":
					hook.after = func() {
						if err := repo.UpsertContent(ctx, &SpaceContent{ID: "10801", ThreadID: "10801", UserID: "1001", Title: "PRIVATE_CONTENT"}); err != nil {
							t.Fatal(err)
						}
					}
				case "space_aba":
					if existing {
						hook.after = func() {
							row, _ := repo.GetByUserID(ctx, "1001")
							old := row.Title
							row.Title = "changed"
							repo.Upsert(ctx, row)
							row.Title = old
							repo.Upsert(ctx, row)
						}
					} else {
						hook.after = func() {
							row := defaultSpace(s.users.(*fakeUserLookup).byID["1001"])
							row.ID = "2001"
							repo.Upsert(ctx, row)
						}
					}
				case "inactive":
					s.users.(*fakeUserLookup).byID["1001"].Status = "suspended"
				}
				before := repo.Snapshot()
				value, err := s.GetSyncStatusForPrincipal(ctx, proof)
				allow := mode == "current" || mode == "disabled"
				if (err == nil) != allow {
					t.Fatal(value, err)
				}
				if allow {
					if value.UserID != "1001" || value.ContentTotal != 0 || value.Disabled != (existing && mode == "disabled") {
						t.Fatal(value)
					}
					raw, _ := json.Marshal(value)
					if strings.Contains(string(raw), "/private") || strings.Contains(string(raw), "PRIVATE_CONTENT") {
						t.Fatal(string(raw))
					}
					if existing && value.LastSyncError == "" {
						t.Fatal("failure disappeared")
					}
					if value.LastSyncAt != nil {
						*value.LastSyncAt = time.Time{}
					}
					if !reflect.DeepEqual(before, repo.Snapshot()) {
						t.Fatal("sync read mutated")
					}
				}
			})
		}
	}
}
