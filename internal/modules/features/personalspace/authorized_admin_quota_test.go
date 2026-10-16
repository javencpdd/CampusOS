package space

import (
	"context"
	"encoding/json"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"reflect"
	"strings"
	"testing"
	"time"
)

type spaceQuotaAdapterHook struct {
	*corestorage.LocalAdapter
	after     func(context.Context)
	afterLock func(context.Context, bool)
}

func (a *spaceQuotaAdapterHook) CurrentQuotaForAdminCommand(ctx context.Context, owner string) (corestorage.QuotaRecord, bool, error) {
	record, exists, err := a.LocalAdapter.CurrentQuotaForAdminCommand(ctx, owner)
	if err == nil && a.afterLock != nil {
		f := a.afterLock
		a.afterLock = nil
		f(ctx, exists)
	}
	return record, exists, err
}

func (a *spaceQuotaAdapterHook) WriteAdminQuotaForCommand(ctx context.Context, owner string, expected, limit int64, actor string, at time.Time) (corestorage.QuotaCommandReceipt, error) {
	receipt, err := a.LocalAdapter.WriteAdminQuotaForCommand(ctx, owner, expected, limit, actor, at)
	if err == nil && a.after != nil {
		a.after(ctx)
	}
	return receipt, err
}
func TestSpaceQuotaAdminSetCurrentTypedActorsExactReceiptAndRollback(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, actor := range []string{"1001", "adm.quota.space"} {
			for _, mode := range []string{"current", "inactive", "audit", "outbox", "missing_proof", "user_domain", "permission", "expired_mfa", "low_limit", "high_limit", "quota_extra", "quota_aba", "user_email", "permission_aba"} {
				t.Run(actor+"/"+map[bool]string{true: "override", false: "default"}[existing]+"/"+mode, func(t *testing.T) {
					ctx := context.Background()
					f := spaceAdminFixture(t, true, actor, mode)
					quotas := corestorage.NewMemoryQuotaRepository()
					if existing {
						if _, err := quotas.Upsert(ctx, corestorage.QuotaRecord{UserID: "1001", QuotaBytes: 1048576, UpdatedBy: "1001"}); err != nil {
							t.Fatal(err)
						}
					}
					adapter, err := corestorage.NewLocalAdapterWithQuotaRepository(t.TempDir(), corestorage.DefaultQuotaBytes, quotas)
					if err != nil {
						t.Fatal(err)
					}
					adapter.SetQuotaReliability(f.s.reliable)
					f.s.SetQuotaManager(adapter)
					proof := f.proof
					limit := int64(2097152)
					switch mode {
					case "inactive":
						user, _ := f.users.GetByID(ctx, "1001")
						user.Status = "deactivated"
						if err := f.users.Update(ctx, user); err != nil {
							t.Fatal(err)
						}
					case "missing_proof":
						proof = nil
					case "user_domain":
						proof = spaceOwnerProof
					case "permission":
						if err := f.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "revoke"); err != nil {
							t.Fatal(err)
						}
					case "low_limit":
						limit = 1048575
					case "high_limit":
						limit = 107374182401
					}
					if mode == "quota_extra" || mode == "quota_aba" || mode == "user_email" || mode == "permission_aba" {
						f.s.SetQuotaManager(&spaceQuotaAdapterHook{LocalAdapter: adapter, after: func(tx context.Context) {
							switch mode {
							case "quota_extra", "quota_aba":
								record, exists, err := adapter.CurrentQuota(tx, "1001")
								if err != nil || !exists {
									t.Fatal(record, exists, err)
								}
								old := record
								record.QuotaBytes++
								if _, err := quotas.Upsert(tx, record); err != nil {
									t.Fatal(err)
								}
								if mode == "quota_aba" {
									if _, err := quotas.Upsert(tx, old); err != nil {
										t.Fatal(err)
									}
								}
							case "user_email":
								user, _ := f.users.GetByID(tx, "1001")
								user.Email = "extra-quota@example.test"
								if err := f.users.Update(tx, user); err != nil {
									t.Fatal(err)
								}
							case "permission_aba":
								if err := f.admins.ReplaceAdminRolePermissionFacts(tx, "security_admin", nil, "revoke"); err != nil {
									t.Fatal(err)
								}
								if err := f.admins.ReplaceAdminRolePermissionFacts(tx, "security_admin", []string{"personal_space.space.manage"}, "restore"); err != nil {
									t.Fatal(err)
								}
							}
						}})
					}
					before, space, users, events, admins := quotas.Snapshot(), f.repo.Snapshot(), f.users.Snapshot(), f.events.Snapshot(), f.admins.Snapshot()
					cached := adapter.QuotaBytes("1001")
					ack, err := f.s.SetStorageQuotaForPrincipal(ctx, "1001", limit, proof)
					allowed := mode == "current" || mode == "inactive"
					if (err == nil) != allowed {
						t.Fatal(ack, err)
					}
					if !allowed {
						if ack != nil || !reflect.DeepEqual(before, quotas.Snapshot()) || !reflect.DeepEqual(space, f.repo.Snapshot()) || !reflect.DeepEqual(users, f.users.Snapshot()) || !reflect.DeepEqual(events, f.events.Snapshot()) || !reflect.DeepEqual(admins, f.admins.Snapshot()) || adapter.QuotaBytes("1001") != cached {
							t.Fatal("denied quota retained data/cache", ack, err)
						}
						return
					}
					record, exists, err := adapter.CurrentQuota(ctx, "1001")
					if err != nil || !exists || record.QuotaBytes != limit || record.UpdatedBy != "" || record.UpdatedActorKind != "admin" || record.UpdatedActorID != actor || record.FactsVersion < 1 || ack.OwnerID != "1001" || ack.QuotaBytes != limit || adapter.QuotaBytes("1001") != limit || !reflect.DeepEqual(space, f.repo.Snapshot()) {
						t.Fatal(record, exists, ack, err)
					}
					raw, _ := json.Marshal(ack)
					var fields map[string]json.RawMessage
					if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || strings.Contains(string(raw), "actor") || fields["owner_id"] == nil || fields["quota_bytes"] == nil {
						t.Fatal(string(raw))
					}
				})
			}
		}
	}
	var missing identityport.SessionPrincipalResolver
	f := spaceAdminFixture(t, false, "adm.noquota", "")
	if _, err := f.s.SetStorageQuotaForPrincipal(context.Background(), "1001", 2097152, missing); err == nil {
		t.Fatal("missing adapters accepted")
	}
}
