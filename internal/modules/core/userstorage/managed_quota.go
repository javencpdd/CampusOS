package storage

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"regexp"
	"time"
)

var ErrQuotaFactsChanged = errors.New("storage quota facts changed")
var quotaAdminActorID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// ManagedQuotaCommands is an owning data adapter. It supplies neither Admin
// authentication nor authorization; the command must obtain both separately.
type ManagedQuotaCommands interface {
	CurrentQuotaForAdminCommand(context.Context, string) (QuotaRecord, bool, error)
	WriteAdminQuotaForCommand(context.Context, string, int64, int64, string, time.Time) (QuotaCommandReceipt, error)
	PublishCommittedQuota(context.Context, QuotaCommandReceipt) error
}

// Non-serializable and bound to the actual adapter. The command compares its
// independent expected model before publishing this cache hint after commit.
type QuotaCommandReceipt struct {
	adapter *LocalAdapter
	record  QuotaRecord
}

func (r QuotaCommandReceipt) Record() QuotaRecord { return r.record }

type managedQuotaRepository interface {
	GetOwnerQuotaForAdminCommand(context.Context, string) (QuotaRecord, bool, error)
	WriteAdminQuota(context.Context, string, int64, int64, string, time.Time) (QuotaRecord, error)
}

func (a *LocalAdapter) SetQuotaReliability(command *reliability.Service) {
	if a == nil || command == nil {
		return
	}
	if snapshot, ok := a.quotaRepository.(transaction.Snapshotter); ok {
		command.RegisterMemorySnapshotters(snapshot)
	}
}
func (a *LocalAdapter) CurrentQuotaForAdminCommand(ctx context.Context, owner string) (QuotaRecord, bool, error) {
	if a == nil || !transaction.Active(ctx) || !SafeSegment(owner) {
		return QuotaRecord{}, false, ErrQuotaFactsUnavailable
	}
	r, ok := a.quotaRepository.(managedQuotaRepository)
	if !ok || NilImageAdapter(r) {
		return QuotaRecord{}, false, ErrQuotaFactsUnavailable
	}
	return r.GetOwnerQuotaForAdminCommand(ctx, owner)
}
func (a *LocalAdapter) WriteAdminQuotaForCommand(ctx context.Context, owner string, expected, limit int64, actor string, at time.Time) (QuotaCommandReceipt, error) {
	if a == nil || !transaction.Active(ctx) || !SafeSegment(owner) || expected < 0 || limit < 1 || !quotaAdminActorID.MatchString(actor) || at.IsZero() {
		return QuotaCommandReceipt{}, ErrQuotaFactsUnavailable
	}
	r, ok := a.quotaRepository.(managedQuotaRepository)
	if !ok || NilImageAdapter(r) {
		return QuotaCommandReceipt{}, ErrQuotaFactsUnavailable
	}
	record, err := r.WriteAdminQuota(ctx, owner, expected, limit, actor, at.UTC().Truncate(time.Microsecond))
	if err != nil {
		return QuotaCommandReceipt{}, err
	}
	return QuotaCommandReceipt{adapter: a, record: record}, nil
}
func (a *LocalAdapter) PublishCommittedQuota(ctx context.Context, receipt QuotaCommandReceipt) error {
	if a == nil || transaction.Active(ctx) || receipt.adapter != a || receipt.record.FactsVersion < 1 || !SafeSegment(receipt.record.UserID) {
		return ErrQuotaFactsUnavailable
	}
	a.quotaMu.Lock()
	defer a.quotaMu.Unlock()
	current, exists := a.quotaOverrides[receipt.record.UserID]
	if !exists || current.FactsVersion < receipt.record.FactsVersion {
		a.quotaOverrides[receipt.record.UserID] = receipt.record
	}
	return nil
}
func (r *MemoryQuotaRepository) GetOwnerQuotaForAdminCommand(ctx context.Context, owner string) (QuotaRecord, bool, error) {
	if !transaction.Active(ctx) {
		return QuotaRecord{}, false, ErrQuotaFactsUnavailable
	}
	return r.GetOwnerQuota(ctx, owner)
}
func (r *MemoryQuotaRepository) WriteAdminQuota(ctx context.Context, owner string, expected, limit int64, actor string, at time.Time) (QuotaRecord, error) {
	if !transaction.Active(ctx) {
		return QuotaRecord{}, ErrQuotaFactsUnavailable
	}
	if err := ctx.Err(); err != nil {
		return QuotaRecord{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.items[owner]
	if exists && current.FactsVersion != expected || !exists && expected != 0 {
		return QuotaRecord{}, ErrQuotaFactsChanged
	}
	item := QuotaRecord{UserID: owner, QuotaBytes: limit, CreatedAt: at, UpdatedAt: at, UpdatedActorKind: "admin", UpdatedActorID: actor}
	if exists {
		item.CreatedAt = current.CreatedAt
	}
	if exists && sameQuotaMetadata(current, item) {
		item.FactsVersion = current.FactsVersion
	} else {
		if r.factsCounter == int64(^uint64(0)>>1) {
			return QuotaRecord{}, ErrQuotaFactsUnavailable
		}
		r.factsCounter++
		item.FactsVersion = r.factsCounter
	}
	r.items[owner] = item
	return item, nil
}
func (r *PgQuotaRepository) GetOwnerQuotaForAdminCommand(ctx context.Context, owner string) (QuotaRecord, bool, error) {
	if !transaction.Active(ctx) {
		return QuotaRecord{}, false, ErrQuotaFactsUnavailable
	}
	if err := r.CheckQuotaFacts(ctx); err != nil {
		return QuotaRecord{}, false, err
	}
	db := transaction.ExecutorFor(ctx, r.pool)
	if _, err := db.Exec(ctx, objectQuotaAdmissionSQL, owner); err != nil {
		return QuotaRecord{}, false, err
	}
	var item QuotaRecord
	err := db.QueryRow(ctx, "SELECT "+quotaRecordColumns+" FROM user_storage_quotas WHERE user_id=$1 FOR UPDATE", owner).Scan(quotaRecordTargets(&item)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return QuotaRecord{}, false, nil
	}
	return item, err == nil, err
}
func (r *PgQuotaRepository) WriteAdminQuota(ctx context.Context, owner string, expected, limit int64, actor string, at time.Time) (QuotaRecord, error) {
	if !transaction.Active(ctx) {
		return QuotaRecord{}, ErrQuotaFactsUnavailable
	}
	if err := r.CheckQuotaFacts(ctx); err != nil {
		return QuotaRecord{}, err
	}
	db := transaction.ExecutorFor(ctx, r.pool)
	if _, err := db.Exec(ctx, objectQuotaAdmissionSQL, owner); err != nil {
		return QuotaRecord{}, err
	}
	var item QuotaRecord
	var err error
	if expected == 0 {
		err = db.QueryRow(ctx, `INSERT INTO user_storage_quotas(user_id,quota_bytes,updated_by,created_at,updated_at,updated_actor_kind,updated_actor_id)VALUES($1,$2,NULL,$4,$4,'admin',$3) ON CONFLICT(user_id) DO NOTHING RETURNING `+quotaRecordColumns, owner, limit, actor, at).Scan(quotaRecordTargets(&item)...)
	} else {
		err = db.QueryRow(ctx, `UPDATE user_storage_quotas SET quota_bytes=$2,updated_by=NULL,updated_at=$4,updated_actor_kind='admin',updated_actor_id=$3 WHERE user_id=$1 AND facts_version=$5 RETURNING `+quotaRecordColumns, owner, limit, actor, at, expected).Scan(quotaRecordTargets(&item)...)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return QuotaRecord{}, ErrQuotaFactsChanged
	}
	if err != nil {
		return QuotaRecord{}, err
	}
	if err := r.CheckQuotaFacts(ctx); err != nil {
		return QuotaRecord{}, err
	}
	return item, nil
}
