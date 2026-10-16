package space

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/platform/reliability"
)

func TestSpaceCurrentReadPublicProfilesDefaultExistingStateAndSafeDetachedAppearance(t *testing.T) {
	ctx := context.Background()
	for _, existing := range []bool{false, true} {
		for _, mode := range []string{"current", "private", "unlisted", "disabled", "inactive", "missing", "feature"} {
			s, repo, events := spaceOwnerFixture(t, existing, "")
			if mode == "missing" {
				s.users = nil
			}
			if mode == "inactive" {
				s.users.(*fakeUserLookup).byID["1001"].Status = "suspended"
			}
			if mode == "feature" {
				s.SetPluginEnabledChecker(func() bool { return false })
			}
			if existing {
				row, _ := repo.GetByUserID(ctx, "1001")
				row.LastSyncError = "/private/host/path"
				row.DisabledReason = "INTERNAL_REASON"
				row.SyncCategories = []string{"PRIVATE_FILTER"}
				row.StyleManifest = &StyleManifest{SchemaVersion: StyleSchemaVersion, Name: "public-style", Version: "1", Layout: "blog", CustomHTMLEnabled: true, CustomHTML: `<section>Public</section>`, CustomCSS: `.public-space[data-campusos-space] { color: red; }`}
				switch mode {
				case "private":
					row.Visibility = VisibilityPrivate
				case "unlisted":
					row.Visibility = VisibilityUnlisted
				case "disabled":
					at := time.Now()
					row.DisabledAt = &at
				}
				if err := repo.Upsert(ctx, row); err != nil {
					t.Fatal(err)
				}
			}
			before := repo.Snapshot()
			for _, byName := range []bool{false, true} {
				var result *SpacePublicProfile
				var err error
				if byName {
					result, err = s.PublicProfileByUsername(ctx, "owner")
				} else {
					result, err = s.PublicProfileByUserID(ctx, "1001")
				}
				allow := mode == "current" || !existing && (mode == "private" || mode == "unlisted" || mode == "disabled")
				if (err == nil) != allow {
					t.Fatal(existing, mode, byName, result, err)
				}
				if !allow {
					if result != nil {
						t.Fatal("partial result")
					}
					continue
				}
				if result.Owner.ID != "1001" || result.Space.UserID != "1001" || result.Space.IsDefault == existing {
					t.Fatal(result)
				}
				raw, _ := json.Marshal(result)
				for _, secret := range []string{"/private/host", "INTERNAL_REASON", "PRIVATE_FILTER", "sync_categories", "sync_tags", "disabled_", "last_sync", "email", "facts_version", "credential"} {
					if strings.Contains(string(raw), secret) {
						t.Fatal("private profile", string(raw))
					}
				}
				if result.Space.StyleManifest != nil {
					result.Space.StyleManifest.CustomHTML = "mutated client projection"
				}
			}
			if !reflect.DeepEqual(before, repo.Snapshot()) {
				t.Fatal("public read mutated Space")
			}
			audits, _, err := events.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
			if err != nil || len(audits) != 0 {
				t.Fatal("read wrote audit", audits, err)
			}
		}
	}
}

type currentReadSpaceHook struct {
	*MemoryRepository
	calls, at int
	after     func()
}

func (r *currentReadSpaceHook) LoadSpaceAuthority(ctx context.Context, owner string, lock bool) (json.RawMessage, error) {
	raw, err := r.MemoryRepository.LoadSpaceAuthority(ctx, owner, lock)
	r.calls++
	if r.calls == r.at && r.after != nil {
		r.after()
	}
	return raw, err
}
func TestSpaceCurrentReadPublicProfilesCurrentFullFactsNameSourceAndEmptyChangesClose(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"space_aba", "user_email", "name", "inactive", "default_created", "feature", "source_error"} {
		t.Run(mode, func(t *testing.T) {
			existing := mode != "default_created"
			s, repo, _ := spaceOwnerFixture(t, existing, "")
			users := s.users.(*fakeUserLookup)
			hook := &currentReadSpaceHook{MemoryRepository: repo, at: 1}
			hook.after = func() {
				switch mode {
				case "space_aba":
					row, _ := repo.GetByUserID(ctx, "1001")
					old := row.Title
					row.Title = "changed"
					if err := repo.Upsert(ctx, row); err != nil {
						t.Fatal(err)
					}
					row.Title = old
					if err := repo.Upsert(ctx, row); err != nil {
						t.Fatal(err)
					}
				case "user_email":
					users.byID["1001"].Email = "changed@example.test"
				case "name":
					users.byID["1001"].Username = "renamed"
				case "inactive":
					users.byID["1001"].Status = identitydomain.UserStatusSuspended
				case "default_created":
					row := defaultSpace(users.byID["1001"])
					row.ID = "2001"
					if err := repo.Upsert(ctx, row); err != nil {
						t.Fatal(err)
					}
				case "feature":
					s.SetPluginEnabledChecker(func() bool { return false })
				case "source_error":
					s.users = newFakeUserLookup()
				}
			}
			s.repo = hook
			result, err := s.PublicProfileByUsername(ctx, "owner")
			if err == nil || result != nil {
				t.Fatal("current profile change accepted", mode, result, err)
			}
		})
	}
	if _, err := (&Service{}).PublicProfileByUserID(ctx, "-1"); !errors.Is(err, ErrSpaceOwnerInvalid) {
		t.Fatal(err)
	}
}
