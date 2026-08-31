package storage

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrObjectNotFound    = errors.New("storage object was not found")
	ErrObjectVersion     = errors.New("storage object version conflict")
	ErrObjectQuota       = errors.New("storage object exceeds quota")
	ErrObjectUnavailable = errors.New("storage object is unavailable")
	ErrObjectPageCursor  = errors.New("storage object page cursor is invalid")
)

type storedObject struct {
	Object
	storageKey string
}

type reservation struct {
	ID            string
	ObjectID      string
	ReservedBytes int64
	Status        string
}

// objectAccount is the persisted quota ledger for one owner. It stays
// provider-internal: callers receive only the safe usage projection.
type objectAccount struct {
	UsedBytes     int64
	ReservedBytes int64
}

// ObjectSummary is an aggregate-only, provider-internal operational view. It
// never includes an owner, object ID, path, filename, or byte payload.
type ObjectSummary struct {
	ObjectStatuses      map[string]int64
	ReservationStatuses map[string]int64
}

type ObjectRepository interface {
	Account(context.Context, string) (objectAccount, bool, error)
	EnsureAccount(context.Context, string, int64) (objectAccount, error)
	AdjustUsage(context.Context, string, int64, int64, int64) error
	Reserve(context.Context, storedObject, int64, int64, int64) (storedObject, reservation, error)
	Commit(context.Context, string, reservation, int64, string, string) (storedObject, error)
	Abort(context.Context, string, reservation) error
	GetOwned(context.Context, string, string) (storedObject, error)
	ListOwned(context.Context, string, ObjectFilter, PageRequest) (ObjectPage, error)
	PrepareDelete(context.Context, string, string, int64) (storedObject, error)
	RestoreReady(context.Context, string) error
	FinalizeDelete(context.Context, string) error
	Summary(context.Context) (ObjectSummary, error)
}

type MemoryObjectRepository struct {
	mu           sync.Mutex
	objects      map[string]storedObject
	reservations map[string]reservation
	accounts     map[string]storageAccount
}
type storageAccount struct {
	used, reserved int64
	initialized    bool
}

func NewMemoryObjectRepository() *MemoryObjectRepository {
	return &MemoryObjectRepository{objects: map[string]storedObject{}, reservations: map[string]reservation{}, accounts: map[string]storageAccount{}}
}

func (r *MemoryObjectRepository) Account(_ context.Context, owner string) (objectAccount, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, ok := r.accounts[owner]
	if !ok || !account.initialized {
		return objectAccount{}, false, nil
	}
	return objectAccount{UsedBytes: account.used, ReservedBytes: account.reserved}, true, nil
}

func (r *MemoryObjectRepository) EnsureAccount(_ context.Context, owner string, observed int64) (objectAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account := r.accounts[owner]
	if !account.initialized {
		account.used = maxInt64(0, observed)
		account.initialized = true
		r.accounts[owner] = account
	}
	return objectAccount{UsedBytes: account.used, ReservedBytes: account.reserved}, nil
}

func (r *MemoryObjectRepository) AdjustUsage(_ context.Context, owner string, delta, quota, observed int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	account := r.accounts[owner]
	if !account.initialized {
		account.used = maxInt64(0, observed)
		account.initialized = true
	}
	if delta > 0 && account.used+account.reserved+delta > quota {
		return ErrObjectQuota
	}
	account.used += delta
	if account.used < 0 {
		account.used = 0
	}
	r.accounts[owner] = account
	return nil
}

func (r *MemoryObjectRepository) Reserve(_ context.Context, item storedObject, requested, quota, observed int64) (storedObject, reservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account := r.accounts[item.OwnerID]
	// The initial observation accounts for legacy files that predate the
	// object ledger.  Re-observing every Put would double count ready objects
	// (their physical bytes are already represented by account.used).
	if !account.initialized {
		account.used = observed
		account.initialized = true
	}
	if requested < 0 || account.used+account.reserved+requested > quota {
		return storedObject{}, reservation{}, ErrObjectQuota
	}
	account.reserved += requested
	r.accounts[item.OwnerID] = account
	item.Status, item.Version = ObjectStatusPending, 1
	r.objects[item.ID] = item
	res := reservation{ID: item.ID, ObjectID: item.ID, ReservedBytes: requested, Status: ObjectStatusPending}
	r.reservations[res.ID] = res
	return item, res, nil
}
func (r *MemoryObjectRepository) Commit(_ context.Context, id string, res reservation, actual int64, sha256, storageKey string) (storedObject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.objects[id]
	if !ok || item.Status != ObjectStatusPending {
		return storedObject{}, ErrObjectUnavailable
	}
	current, ok := r.reservations[res.ID]
	if !ok || current.Status != ObjectStatusPending || actual < 0 || actual > current.ReservedBytes {
		return storedObject{}, ErrObjectUnavailable
	}
	account := r.accounts[item.OwnerID]
	account.reserved -= current.ReservedBytes
	account.used += actual
	r.accounts[item.OwnerID] = account
	item.SizeBytes, item.SHA256, item.storageKey, item.Status, item.Version, item.UpdatedAt = actual, sha256, storageKey, ObjectStatusReady, item.Version+1, time.Now().UTC()
	r.objects[id] = item
	current.Status = "committed"
	r.reservations[res.ID] = current
	return item, nil
}
func (r *MemoryObjectRepository) Abort(_ context.Context, id string, res reservation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.objects[id]
	if !ok {
		return ErrObjectNotFound
	}
	if current, ok := r.reservations[res.ID]; ok && current.Status == ObjectStatusPending {
		account := r.accounts[item.OwnerID]
		account.reserved -= current.ReservedBytes
		r.accounts[item.OwnerID] = account
		current.Status = "released"
		r.reservations[res.ID] = current
	}
	now := time.Now().UTC()
	item.Status, item.DeletedAt, item.UpdatedAt, item.Version = ObjectStatusDeleted, &now, now, item.Version+1
	r.objects[id] = item
	return nil
}
func (r *MemoryObjectRepository) GetOwned(_ context.Context, owner, id string) (storedObject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.objects[id]
	if !ok || item.OwnerID != owner || item.Status != ObjectStatusReady {
		return storedObject{}, ErrObjectNotFound
	}
	return item, nil
}
func (r *MemoryObjectRepository) ListOwned(_ context.Context, owner string, filter ObjectFilter, page PageRequest) (ObjectPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cursor, err := parseObjectPageCursor(page.Cursor)
	if err != nil {
		return ObjectPage{}, err
	}
	items := []Object{}
	for _, item := range r.objects {
		itemID, idErr := parseObjectPageCursor(item.ID)
		if idErr != nil {
			return ObjectPage{}, idErr
		}
		if item.OwnerID != owner || (!filter.IncludeDeleted && item.Status == ObjectStatusDeleted) || (filter.Namespace != "" && item.Namespace != filter.Namespace) || (filter.Purpose != "" && item.Purpose != filter.Purpose) || (cursor > 0 && itemID >= cursor) {
			continue
		}
		items = append(items, item.Object)
	}
	sort.Slice(items, func(i, j int) bool {
		left, _ := parseObjectPageCursor(items[i].ID)
		right, _ := parseObjectPageCursor(items[j].ID)
		return left > right
	})
	limit := normalizePageLimit(page.Limit)
	next := ""
	if len(items) > limit {
		next = items[limit-1].ID
		items = items[:limit]
	}
	return ObjectPage{Items: items, NextCursor: next}, nil
}
func (r *MemoryObjectRepository) PrepareDelete(_ context.Context, owner, id string, version int64) (storedObject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.objects[id]
	if !ok || item.OwnerID != owner || item.Status != ObjectStatusReady {
		return storedObject{}, ErrObjectNotFound
	}
	if item.Version != version {
		return storedObject{}, ErrObjectVersion
	}
	item.Status = ObjectStatusDeleting
	item.Version++
	item.UpdatedAt = time.Now().UTC()
	r.objects[id] = item
	return item, nil
}
func (r *MemoryObjectRepository) RestoreReady(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.objects[id]
	if !ok {
		return ErrObjectNotFound
	}
	item.Status = ObjectStatusReady
	item.Version++
	item.UpdatedAt = time.Now().UTC()
	r.objects[id] = item
	return nil
}
func (r *MemoryObjectRepository) FinalizeDelete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.objects[id]
	if !ok || item.Status != ObjectStatusDeleting {
		return ErrObjectNotFound
	}
	account := r.accounts[item.OwnerID]
	account.used -= item.SizeBytes
	if account.used < 0 {
		account.used = 0
	}
	r.accounts[item.OwnerID] = account
	now := time.Now().UTC()
	item.Status, item.DeletedAt, item.UpdatedAt, item.Version = ObjectStatusDeleted, &now, now, item.Version+1
	r.objects[id] = item
	return nil
}
func (r *MemoryObjectRepository) Summary(_ context.Context) (ObjectSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	summary := ObjectSummary{ObjectStatuses: map[string]int64{}, ReservationStatuses: map[string]int64{}}
	for _, item := range r.objects {
		summary.ObjectStatuses[item.Status]++
	}
	for _, item := range r.reservations {
		summary.ReservationStatuses[item.Status]++
	}
	return summary, nil
}

type PgObjectRepository struct {
	pool         *pgxpool.Pool
	transactions transaction.Manager
}

func NewPgObjectRepository(pool *pgxpool.Pool) *PgObjectRepository {
	return &PgObjectRepository{pool: pool, transactions: transaction.NewPostgreSQL(pool)}
}

func (r *PgObjectRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

// Account reads the durable ledger without inspecting the provider tree. A
// missing row is normal before the one-time legacy adoption handoff.
func (r *PgObjectRepository) Account(ctx context.Context, owner string) (objectAccount, bool, error) {
	var account objectAccount
	err := r.db(ctx).QueryRow(ctx, `SELECT used_bytes,reserved_bytes FROM user_storage_accounts WHERE user_id=$1::bigint`, owner).Scan(&account.UsedBytes, &account.ReservedBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return objectAccount{}, false, nil
	}
	if err != nil {
		return objectAccount{}, false, err
	}
	return account, true, nil
}

// EnsureAccount performs the one-time legacy observation handoff. Once an
// account exists, normal ObjectPort reads and writes use it as quota fact.
func (r *PgObjectRepository) EnsureAccount(ctx context.Context, owner string, observed int64) (objectAccount, error) {
	var account objectAccount
	err := r.transactions.Within(ctx, func(txCtx context.Context) error {
		db := r.db(txCtx)
		if _, err := db.Exec(txCtx, `INSERT INTO user_storage_accounts (user_id,used_bytes,reserved_bytes,version,created_at,updated_at)
			VALUES ($1::bigint,$2,0,1,NOW(),NOW()) ON CONFLICT (user_id) DO NOTHING`, owner, maxInt64(0, observed)); err != nil {
			return err
		}
		return db.QueryRow(txCtx, `SELECT used_bytes,reserved_bytes FROM user_storage_accounts WHERE user_id=$1::bigint`, owner).Scan(&account.UsedBytes, &account.ReservedBytes)
	})
	if err != nil {
		return objectAccount{}, err
	}
	return account, nil
}

// AdjustUsage applies the byte delta of an explicitly retained compatibility
// copy. It keeps legacy JSON/index files in the same transactional quota
// ledger without making normal ObjectPort calls rescan a user's directory.
func (r *PgObjectRepository) AdjustUsage(ctx context.Context, owner string, delta, quota, observed int64) error {
	return r.transactions.Within(ctx, func(txCtx context.Context) error {
		db := r.db(txCtx)
		if _, err := db.Exec(txCtx, `INSERT INTO user_storage_accounts (user_id,used_bytes,reserved_bytes,version,created_at,updated_at)
			VALUES ($1::bigint,$2,0,1,NOW(),NOW()) ON CONFLICT (user_id) DO NOTHING`, owner, maxInt64(0, observed)); err != nil {
			return err
		}
		var used, reserved int64
		if err := db.QueryRow(txCtx, `SELECT used_bytes,reserved_bytes FROM user_storage_accounts WHERE user_id=$1::bigint FOR UPDATE`, owner).Scan(&used, &reserved); err != nil {
			return err
		}
		if delta > 0 && used+reserved+delta > quota {
			return ErrObjectQuota
		}
		_, err := db.Exec(txCtx, `UPDATE user_storage_accounts SET used_bytes=GREATEST(0,used_bytes+$2),version=version+1,updated_at=NOW() WHERE user_id=$1::bigint`, owner, delta)
		return err
	})
}

func (r *PgObjectRepository) Reserve(ctx context.Context, item storedObject, requested, quota, observed int64) (storedObject, reservation, error) {
	res := reservation{ID: item.ID, ObjectID: item.ID, ReservedBytes: requested, Status: ObjectStatusPending}
	err := r.transactions.Within(ctx, func(txCtx context.Context) error {
		db := r.db(txCtx)
		created, queryErr := db.Exec(txCtx, `INSERT INTO user_storage_accounts (user_id,used_bytes,reserved_bytes,version) VALUES ($1,$2,0,1) ON CONFLICT (user_id) DO NOTHING`, item.OwnerID, maxInt64(0, observed))
		if queryErr != nil {
			return queryErr
		}
		var used, reserved int64
		if queryErr = db.QueryRow(txCtx, `SELECT used_bytes,reserved_bytes FROM user_storage_accounts WHERE user_id=$1 FOR UPDATE`, item.OwnerID).Scan(&used, &reserved); queryErr != nil {
			return queryErr
		}
		_ = created // Existing ledgers are authoritative; do not double count managed objects from a directory scan.
		if requested < 0 || used+reserved+requested > quota {
			return ErrObjectQuota
		}
		item.Status, item.Version = ObjectStatusPending, 1
		if queryErr = db.QueryRow(txCtx, `INSERT INTO storage_objects (id,owner_user_id,namespace,purpose,provider,storage_key,original_name,mime_type,size_bytes,sha256,status,version,created_at,updated_at) VALUES ($1,$2,$3,$4,'local',$5,$6,$7,0,'','pending',1,NOW(),NOW()) RETURNING created_at,updated_at`, item.ID, item.OwnerID, item.Namespace, item.Purpose, item.storageKey, item.OriginalName, item.MimeType).Scan(&item.CreatedAt, &item.UpdatedAt); queryErr != nil {
			return queryErr
		}
		if _, queryErr = db.Exec(txCtx, `INSERT INTO user_storage_reservations (id,user_id,object_id,reserved_bytes,status,expires_at,created_at,updated_at) VALUES ($1,$2,$3,$4,'pending',NOW()+INTERVAL '15 minutes',NOW(),NOW())`, res.ID, item.OwnerID, item.ID, requested); queryErr != nil {
			return queryErr
		}
		_, queryErr = db.Exec(txCtx, `UPDATE user_storage_accounts SET reserved_bytes=reserved_bytes+$2,version=version+1,updated_at=NOW() WHERE user_id=$1`, item.OwnerID, requested)
		return queryErr
	})
	if err != nil {
		return storedObject{}, reservation{}, err
	}
	return item, res, nil
}
func (r *PgObjectRepository) Commit(ctx context.Context, id string, res reservation, actual int64, sha256, key string) (storedObject, error) {
	var item storedObject
	err := r.transactions.Within(ctx, func(txCtx context.Context) error {
		db := r.db(txCtx)
		var owner, status string
		var reserved int64
		if queryErr := db.QueryRow(txCtx, `SELECT user_id::text,reserved_bytes,status FROM user_storage_reservations WHERE id=$1 AND object_id=$2 FOR UPDATE`, res.ID, id).Scan(&owner, &reserved, &status); queryErr != nil {
			if errors.Is(queryErr, pgx.ErrNoRows) {
				return ErrObjectUnavailable
			}
			return queryErr
		}
		if status != "pending" || actual < 0 || actual > reserved {
			return ErrObjectUnavailable
		}
		if _, queryErr := db.Exec(txCtx, `UPDATE user_storage_accounts SET reserved_bytes=reserved_bytes-$2,used_bytes=used_bytes+$3,version=version+1,updated_at=NOW() WHERE user_id=$1`, owner, reserved, actual); queryErr != nil {
			return queryErr
		}
		if _, queryErr := db.Exec(txCtx, `UPDATE user_storage_reservations SET status='committed',updated_at=NOW() WHERE id=$1`, res.ID); queryErr != nil {
			return queryErr
		}
		var queryErr error
		item, queryErr = queryStoredObject(txCtx, db, `UPDATE storage_objects SET storage_key=$2,size_bytes=$3,sha256=$4,status='ready',version=version+1,updated_at=NOW() WHERE id=$1 AND status='pending' RETURNING id::text,owner_user_id::text,namespace,purpose,storage_key,original_name,mime_type,size_bytes,sha256,status,version,created_at,updated_at,deleted_at`, id, key, actual, sha256)
		return queryErr
	})
	if err != nil {
		return storedObject{}, err
	}
	return item, nil
}
func (r *PgObjectRepository) Abort(ctx context.Context, id string, res reservation) error {
	return r.transactions.Within(ctx, func(txCtx context.Context) error {
		db := r.db(txCtx)
		var owner, status string
		var bytes int64
		queryErr := db.QueryRow(txCtx, `SELECT user_id::text,reserved_bytes,status FROM user_storage_reservations WHERE id=$1 FOR UPDATE`, res.ID).Scan(&owner, &bytes, &status)
		if queryErr == nil && status == "pending" {
			if _, queryErr = db.Exec(txCtx, `UPDATE user_storage_accounts SET reserved_bytes=GREATEST(0,reserved_bytes-$2),version=version+1,updated_at=NOW() WHERE user_id=$1`, owner, bytes); queryErr != nil {
				return queryErr
			}
			if _, queryErr = db.Exec(txCtx, `UPDATE user_storage_reservations SET status='released',updated_at=NOW() WHERE id=$1`, res.ID); queryErr != nil {
				return queryErr
			}
		} else if queryErr != nil && !errors.Is(queryErr, pgx.ErrNoRows) {
			return queryErr
		}
		_, queryErr = db.Exec(txCtx, `UPDATE storage_objects SET status='deleted',deleted_at=NOW(),version=version+1,updated_at=NOW() WHERE id=$1 AND status='pending'`, id)
		return queryErr
	})
}
func (r *PgObjectRepository) GetOwned(ctx context.Context, owner, id string) (storedObject, error) {
	item, err := queryStoredObject(ctx, r.db(ctx), `SELECT id::text,owner_user_id::text,namespace,purpose,storage_key,original_name,mime_type,size_bytes,sha256,status,version,created_at,updated_at,deleted_at FROM storage_objects WHERE id=$1 AND owner_user_id=$2 AND status='ready'`, id, owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedObject{}, ErrObjectNotFound
	}
	return item, err
}
func (r *PgObjectRepository) ListOwned(ctx context.Context, owner string, filter ObjectFilter, page PageRequest) (ObjectPage, error) {
	limit := normalizePageLimit(page.Limit)
	cursor, err := parseObjectPageCursor(page.Cursor)
	if err != nil {
		return ObjectPage{}, err
	}
	rows, err := r.db(ctx).Query(ctx, `SELECT id::text,owner_user_id::text,namespace,purpose,storage_key,original_name,mime_type,size_bytes,sha256,status,version,created_at,updated_at,deleted_at FROM storage_objects WHERE owner_user_id=$1 AND ($2='' OR namespace=$2) AND ($3='' OR purpose=$3) AND ($4 OR status<>'deleted') AND ($5::bigint=0 OR id<$5::bigint) ORDER BY id DESC LIMIT $6`, owner, filter.Namespace, filter.Purpose, filter.IncludeDeleted, cursor, limit+1)
	if err != nil {
		return ObjectPage{}, err
	}
	defer rows.Close()
	items := []Object{}
	for rows.Next() {
		item, scanErr := scanStoredObject(rows)
		if scanErr != nil {
			return ObjectPage{}, scanErr
		}
		items = append(items, item.Object)
	}
	if err = rows.Err(); err != nil {
		return ObjectPage{}, err
	}
	next := ""
	if len(items) > limit {
		next = items[limit-1].ID
		items = items[:limit]
	}
	return ObjectPage{Items: items, NextCursor: next}, nil
}
func (r *PgObjectRepository) PrepareDelete(ctx context.Context, owner, id string, version int64) (storedObject, error) {
	item, err := queryStoredObject(ctx, r.db(ctx), `UPDATE storage_objects SET status='deleting',version=version+1,updated_at=NOW() WHERE id=$1 AND owner_user_id=$2 AND status='ready' AND version=$3 RETURNING id::text,owner_user_id::text,namespace,purpose,storage_key,original_name,mime_type,size_bytes,sha256,status,version,created_at,updated_at,deleted_at`, id, owner, version)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedObject{}, ErrObjectVersion
	}
	return item, err
}
func (r *PgObjectRepository) RestoreReady(ctx context.Context, id string) error {
	_, err := r.db(ctx).Exec(ctx, `UPDATE storage_objects SET status='ready',version=version+1,updated_at=NOW() WHERE id=$1 AND status='deleting'`, id)
	return err
}
func (r *PgObjectRepository) FinalizeDelete(ctx context.Context, id string) error {
	return r.transactions.Within(ctx, func(txCtx context.Context) error {
		db := r.db(txCtx)
		var owner string
		var size int64
		if err := db.QueryRow(txCtx, `SELECT owner_user_id::text,size_bytes FROM storage_objects WHERE id=$1 AND status='deleting' FOR UPDATE`, id).Scan(&owner, &size); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrObjectNotFound
			}
			return err
		}
		if _, err := db.Exec(txCtx, `UPDATE user_storage_accounts SET used_bytes=GREATEST(0,used_bytes-$2),version=version+1,updated_at=NOW() WHERE user_id=$1`, owner, size); err != nil {
			return err
		}
		_, err := db.Exec(txCtx, `UPDATE storage_objects SET status='deleted',deleted_at=NOW(),version=version+1,updated_at=NOW() WHERE id=$1`, id)
		return err
	})
}
func (r *PgObjectRepository) Summary(ctx context.Context) (ObjectSummary, error) {
	summary := ObjectSummary{ObjectStatuses: map[string]int64{}, ReservationStatuses: map[string]int64{}}
	rows, err := r.db(ctx).Query(ctx, `SELECT kind,status,total FROM (
		SELECT 'object' AS kind,status,COUNT(*)::bigint AS total FROM storage_objects GROUP BY status
		UNION ALL
		SELECT 'reservation' AS kind,status,COUNT(*)::bigint AS total FROM user_storage_reservations GROUP BY status
	) summary ORDER BY kind,status`)
	if err != nil {
		return ObjectSummary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, status string
		var total int64
		if err := rows.Scan(&kind, &status, &total); err != nil {
			return ObjectSummary{}, err
		}
		if kind == "object" {
			summary.ObjectStatuses[status] = total
		} else {
			summary.ReservationStatuses[status] = total
		}
	}
	return summary, rows.Err()
}

type rowScanner interface{ Scan(...any) error }
type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func queryStoredObject(ctx context.Context, q queryer, sql string, args ...any) (storedObject, error) {
	return scanStoredObject(q.QueryRow(ctx, sql, args...))
}
func scanStoredObject(row rowScanner) (storedObject, error) {
	var item storedObject
	err := row.Scan(&item.ID, &item.OwnerID, &item.Namespace, &item.Purpose, &item.storageKey, &item.OriginalName, &item.MimeType, &item.SizeBytes, &item.SHA256, &item.Status, &item.Version, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt)
	return item, err
}
func normalizePageLimit(value int) int {
	if value <= 0 {
		return ObjectPageDefaultLimit
	}
	if value > ObjectPageMaximumLimit {
		return ObjectPageMaximumLimit
	}
	return value
}

// parseObjectPageCursor preserves the numeric order of Snowflake-style object
// IDs. Comparing id::text would order values lexicographically and prevent
// PostgreSQL from using the (owner_user_id, id DESC) keyset index reliably on
// large catalogs.
func parseObjectPageCursor(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, ErrObjectPageCursor
	}
	return parsed, nil
}

func normalizeObjectText(value string) string { return strings.TrimSpace(value) }

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
