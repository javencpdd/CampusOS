package space

import (
	"context"
	"encoding/json"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"reflect"
	"strings"
	"testing"
	"time"
)

type adminInventoryHook struct {
	*MemoryRepository
	after func()
}

func (r *adminInventoryHook) LoadSpaceAdminInventory(ctx context.Context) ([]spaceAdminSummaryRow, error) {
	rows, err := r.MemoryRepository.LoadSpaceAdminInventory(ctx)
	if err == nil && r.after != nil {
		f := r.after
		r.after = nil
		f()
	}
	return rows, err
}
func TestSpaceQuotaAdminSummaryCurrentEmptyAggregateAndCurrentAuthority(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, mode := range []string{"current", "password", "expired_mfa", "user_domain", "revoked", "space_aba", "sync_metadata", "empty_created"} {
			t.Run(map[bool]string{true: "rows", false: "empty"}[existing]+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				f := spaceAdminFixture(t, existing, "adm.summary.space", mode)
				proof := f.proof
				if mode == "user_domain" {
					proof = spaceOwnerProof
				}
				if mode == "revoked" {
					if err := f.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "revoke"); err != nil {
						t.Fatal(err)
					}
				}
				changed := false
				if mode == "space_aba" || mode == "sync_metadata" || mode == "empty_created" {
					f.s.repo = &adminInventoryHook{MemoryRepository: f.repo, after: func() {
						changed = true
						row, err := f.repo.GetByUserID(ctx, "1001")
						if err != nil {
							user, err := f.users.GetByID(ctx, "1001")
							if err != nil {
								t.Fatal(err)
							}
							row = defaultSpace(user)
							row.ID = "2001"
							row.IsDefault = false
						}
						if mode == "sync_metadata" {
							at := time.Now().UTC()
							row.LastSyncAt = &at
							row.LastSyncError = ""
						} else {
							title := row.Title
							row.Title = "extra"
							if err := f.repo.Upsert(ctx, row); err != nil {
								t.Fatal(err)
							}
							row.Title = title
						}
						if err := f.repo.Upsert(ctx, row); err != nil {
							t.Fatal(err)
						}
					}}
				}
				before, events := f.repo.Snapshot(), f.events.Snapshot()
				summary, err := f.s.AdminSummaryForPrincipal(ctx, proof)
				allowed := mode == "current" || mode == "password" || mode == "expired_mfa"
				if (err == nil) != allowed {
					t.Fatal(summary, err)
				}
				if !allowed {
					if summary != nil || !reflect.DeepEqual(events, f.events.Snapshot()) {
						t.Fatal(summary, err)
					}
					if (mode == "space_aba" || mode == "sync_metadata" || mode == "empty_created") && !changed {
						t.Fatal("mutation not reached")
					}
					return
				}
				if !reflect.DeepEqual(before, f.repo.Snapshot()) || !reflect.DeepEqual(events, f.events.Snapshot()) {
					t.Fatal("summary wrote state")
				}
				want := int64(0)
				if existing {
					want = 1
				}
				if summary.TotalSpaces != want || summary.SyncEnabledSpaces != want || summary.SyncErrorSpaces != want || summary.PublicSpaces != 0 || summary.DisabledSpaces != 0 || summary.StyledSpaces != 0 {
					t.Fatal(summary)
				}
				raw, _ := json.Marshal(summary)
				for _, private := range []string{"/host/private", "Owner", "space-owner", "1001", "title", "body"} {
					if strings.Contains(string(raw), private) {
						t.Fatal("private summary data leaked", string(raw))
					}
				}
			})
		}
	}
}
func TestSpaceQuotaAdminStorageCurrentPersistedQuotaInactiveAndNoWrites(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, mode := range []string{"current", "password", "expired_mfa", "inactive", "revoked", "quota_aba", "user_email"} {
			t.Run(map[bool]string{true: "override", false: "default"}[existing]+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				f := spaceAdminFixture(t, true, "adm.storage.space", mode)
				quotas := corestorage.NewMemoryQuotaRepository()
				adapter, err := corestorage.NewLocalAdapterWithQuotaRepository(t.TempDir(), corestorage.DefaultQuotaBytes, quotas)
				if err != nil {
					t.Fatal(err)
				}
				if existing {
					if _, err := quotas.Upsert(ctx, corestorage.QuotaRecord{UserID: "1001", QuotaBytes: 2097152, UpdatedActorKind: "admin", UpdatedActorID: "adm.existing.quota"}); err != nil {
						t.Fatal(err)
					}
				}
				store, err := NewLocalFileStoreWithStorage(FileStorageConfig{}, adapter)
				if err != nil {
					t.Fatal(err)
				}
				f.s.SetFileStore(store)
				f.s.SetQuotaManager(adapter)
				if mode == "inactive" {
					user, _ := f.users.GetByID(ctx, "1001")
					user.Status = "suspended"
					if err := f.users.Update(ctx, user); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "revoked" {
					if err := f.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "revoke"); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "quota_aba" || mode == "user_email" {
					calls := 0
					f.s.repo = &adminStorageSpaceHook{MemoryRepository: f.repo, after: func() {
						calls++
						if calls == 2 {
							if mode == "user_email" {
								user, _ := f.users.GetByID(ctx, "1001")
								user.Email = "storage-changed@example.test"
								if err := f.users.Update(ctx, user); err != nil {
									t.Fatal(err)
								}
							} else {
								record := corestorage.QuotaRecord{UserID: "1001", QuotaBytes: 2097152}
								if existing {
									var err error
									record, _, err = adapter.CurrentQuota(ctx, "1001")
									if err != nil {
										t.Fatal(err)
									}
								}
								other := record
								other.QuotaBytes++
								if _, err := quotas.Upsert(ctx, other); err != nil {
									t.Fatal(err)
								}
								if _, err := quotas.Upsert(ctx, record); err != nil {
									t.Fatal(err)
								}
							}
						}
					}}
				}
				before, events, quotaBefore := f.repo.Snapshot(), f.events.Snapshot(), quotas.Snapshot()
				status, err := f.s.AdminStorageStatusForPrincipal(ctx, "1001", f.proof)
				allowed := mode == "current" || mode == "password" || mode == "expired_mfa" || mode == "inactive"
				if (err == nil) != allowed {
					t.Fatal(status, err)
				}
				if !allowed {
					if status != nil || !reflect.DeepEqual(events, f.events.Snapshot()) {
						t.Fatal(status, err)
					}
					return
				}
				if !reflect.DeepEqual(before, f.repo.Snapshot()) || !reflect.DeepEqual(events, f.events.Snapshot()) || !reflect.DeepEqual(quotaBefore, quotas.Snapshot()) {
					t.Fatal("metadata read wrote state")
				}
				want := corestorage.DefaultQuotaBytes
				if existing {
					want = 2097152
				}
				if status.UserID != "1001" || status.QuotaBytes != want || status.CustomQuota != existing || status.UsedBytes != 0 || status.AvailableBytes != want || status.QuotaUpdatedBy != "" {
					t.Fatal(status)
				}
			})
		}
	}
}

type adminStorageSpaceHook struct {
	*MemoryRepository
	after func()
}

func (r *adminStorageSpaceHook) LoadSpaceAuthority(ctx context.Context, owner string, lock bool) (json.RawMessage, error) {
	raw, err := r.MemoryRepository.LoadSpaceAuthority(ctx, owner, lock)
	if err == nil && r.after != nil {
		r.after()
	}
	return raw, err
}
