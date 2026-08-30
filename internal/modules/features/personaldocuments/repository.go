package personaldocuments

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalid     = errors.New("personal document is invalid")
	ErrNotFound    = errors.New("personal document was not found")
	ErrConflict    = errors.New("personal document version conflict")
	ErrNotEditable = errors.New("personal document format is not editable")
)

type MemoryRepository struct {
	mu       sync.RWMutex
	docs     map[string]Document
	versions map[string][]DocumentVersion
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{docs: map[string]Document{}, versions: map[string][]DocumentVersion{}}
}
func (r *MemoryRepository) Create(_ context.Context, d Document, v DocumentVersion) (DocumentDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.docs[d.ID]; ok {
		return DocumentDetail{}, ErrConflict
	}
	d.CurrentVersionID = v.ID
	r.docs[d.ID] = d
	r.versions[d.ID] = []DocumentVersion{v}
	return DocumentDetail{Document: d, CurrentVersion: &v}, nil
}
func (r *MemoryRepository) List(_ context.Context, owner string, f ListFilter) ([]DocumentDetail, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []DocumentDetail{}
	for _, d := range r.docs {
		if d.OwnerID != owner || (f.Status != "" && d.Status != f.Status) {
			continue
		}
		vs := r.versions[d.ID]
		var current *DocumentVersion
		if len(vs) > 0 {
			v := vs[len(vs)-1]
			current = &v
		}
		out = append(out, DocumentDetail{Document: d, CurrentVersion: current})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}
func (r *MemoryRepository) Get(_ context.Context, owner, id string) (DocumentDetail, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.docs[id]
	if !ok || d.OwnerID != owner {
		return DocumentDetail{}, ErrNotFound
	}
	for _, v := range r.versions[id] {
		if v.ID == d.CurrentVersionID {
			return DocumentDetail{Document: d, CurrentVersion: &v}, nil
		}
	}
	return DocumentDetail{Document: d}, nil
}
func (r *MemoryRepository) Versions(_ context.Context, owner, id string) ([]DocumentVersion, error) {
	if _, err := r.Get(context.Background(), owner, id); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := append([]DocumentVersion(nil), r.versions[id]...)
	sort.Slice(out, func(i, j int) bool { return out[i].VersionNumber > out[j].VersionNumber })
	return out, nil
}
func (r *MemoryRepository) AppendVersion(_ context.Context, owner, id string, expected int64, v DocumentVersion, name string) (DocumentDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.docs[id]
	if !ok || d.OwnerID != owner {
		return DocumentDetail{}, ErrNotFound
	}
	if d.Status != StatusActive {
		return DocumentDetail{}, ErrNotEditable
	}
	if d.Version != expected {
		return DocumentDetail{}, ErrConflict
	}
	v.VersionNumber = len(r.versions[id]) + 1
	r.versions[id] = append(r.versions[id], v)
	d.CurrentVersionID = v.ID
	d.Version++
	if name != "" {
		d.Name = name
	}
	d.UpdatedAt = time.Now().UTC()
	r.docs[id] = d
	return DocumentDetail{Document: d, CurrentVersion: &v}, nil
}
func (r *MemoryRepository) SetStatus(_ context.Context, owner, id string, expected int64, status string) (DocumentDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.docs[id]
	if !ok || d.OwnerID != owner {
		return DocumentDetail{}, ErrNotFound
	}
	if d.Version != expected {
		return DocumentDetail{}, ErrConflict
	}
	now := time.Now().UTC()
	d.Status = status
	d.Version++
	d.UpdatedAt = now
	if status == StatusTrashed {
		d.DeletedAt = &now
	} else {
		d.DeletedAt = nil
	}
	r.docs[id] = d
	var c *DocumentVersion
	for _, v := range r.versions[id] {
		if v.ID == d.CurrentVersionID {
			x := v
			c = &x
		}
	}
	return DocumentDetail{Document: d, CurrentVersion: c}, nil
}
func (r *MemoryRepository) Version(_ context.Context, owner, id, vid string) (DocumentVersion, error) {
	if _, err := r.Get(context.Background(), owner, id); err != nil {
		return DocumentVersion{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, v := range r.versions[id] {
		if v.ID == vid {
			return v, nil
		}
	}
	return DocumentVersion{}, ErrNotFound
}

// PreviewSummary implements the optional operational reader used by Service.
// The in-memory development adapter has no converter queue, so all bounded
// lifecycle gauges are reported as zero by the caller.
func (r *MemoryRepository) PreviewSummary(context.Context) (map[PreviewMetricKey]int64, error) {
	return map[PreviewMetricKey]int64{}, nil
}

type PgRepository struct {
	pool         *pgxpool.Pool
	transactions transaction.Manager
}

func NewPgRepository(pool *pgxpool.Pool) *PgRepository {
	return &PgRepository{pool: pool, transactions: transaction.NewPostgreSQL(pool)}
}

func (r *PgRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgRepository) Create(ctx context.Context, d Document, v DocumentVersion) (DocumentDetail, error) {
	if err := r.transactions.Within(ctx, func(txCtx context.Context) error {
		db := r.db(txCtx)
		if _, err := db.Exec(txCtx, `INSERT INTO personal_documents (id,owner_user_id,name,document_type,status,current_version_id,version,created_at,updated_at) VALUES ($1::bigint,$2::bigint,$3,$4,'active',NULL,1,NOW(),NOW())`, d.ID, d.OwnerID, d.Name, d.Format); err != nil {
			return err
		}
		if _, err := db.Exec(txCtx, `INSERT INTO personal_document_versions (id,document_id,version_number,source_object_id,source_type,size_bytes,sha256,created_by,created_at) VALUES ($1::bigint,$2::bigint,1,$3::bigint,$4,$5,$6,$7::bigint,NOW())`, v.ID, d.ID, v.SourceObjectID, v.Format, v.SizeBytes, v.SHA256, d.OwnerID); err != nil {
			return err
		}
		_, err := db.Exec(txCtx, `UPDATE personal_documents SET current_version_id=$2::bigint WHERE id=$1::bigint`, d.ID, v.ID)
		return err
	}); err != nil {
		return DocumentDetail{}, err
	}
	return r.Get(ctx, d.OwnerID, d.ID)
}
func (r *PgRepository) List(ctx context.Context, owner string, f ListFilter) ([]DocumentDetail, error) {
	rows, e := r.db(ctx).Query(ctx, detailSQL+` WHERE d.owner_user_id=$1::bigint AND ($2='' OR d.status=$2) ORDER BY d.updated_at DESC`, owner, f.Status)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []DocumentDetail{}
	for rows.Next() {
		x, e := scanDetail(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (r *PgRepository) Get(ctx context.Context, owner, id string) (DocumentDetail, error) {
	x, e := scanDetail(r.db(ctx).QueryRow(ctx, detailSQL+` WHERE d.id=$1::bigint AND d.owner_user_id=$2::bigint`, id, owner))
	if errors.Is(e, pgx.ErrNoRows) {
		return DocumentDetail{}, ErrNotFound
	}
	return x, e
}
func (r *PgRepository) Versions(ctx context.Context, owner, id string) ([]DocumentVersion, error) {
	if _, e := r.Get(ctx, owner, id); e != nil {
		return nil, e
	}
	rows, e := r.db(ctx).Query(ctx, versionSQL+` WHERE document_id=$1::bigint ORDER BY version_number DESC`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []DocumentVersion{}
	for rows.Next() {
		var x DocumentVersion
		if e = rows.Scan(&x.ID, &x.DocumentID, &x.VersionNumber, &x.SourceObjectID, &x.Format, &x.SizeBytes, &x.SHA256, &x.RestoredFromVersionID, &x.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (r *PgRepository) AppendVersion(ctx context.Context, owner, id string, expected int64, v DocumentVersion, name string) (DocumentDetail, error) {
	if err := r.transactions.Within(ctx, func(txCtx context.Context) error {
		db := r.db(txCtx)
		var status string
		var current int64
		if err := db.QueryRow(txCtx, `SELECT status,version FROM personal_documents WHERE id=$1::bigint AND owner_user_id=$2::bigint FOR UPDATE`, id, owner).Scan(&status, &current); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status != StatusActive {
			return ErrNotEditable
		}
		if current != expected {
			return ErrConflict
		}
		var number int
		if err := db.QueryRow(txCtx, `SELECT COALESCE(MAX(version_number),0)+1 FROM personal_document_versions WHERE document_id=$1::bigint`, id).Scan(&number); err != nil {
			return err
		}
		v.VersionNumber = number
		if _, err := db.Exec(txCtx, `INSERT INTO personal_document_versions (id,document_id,version_number,source_object_id,source_type,size_bytes,sha256,restored_from_version_id,created_by,created_at) VALUES ($1::bigint,$2::bigint,$3,$4::bigint,$5,$6,$7,NULLIF($8,'')::bigint,$9::bigint,NOW())`, v.ID, id, number, v.SourceObjectID, v.Format, v.SizeBytes, v.SHA256, v.RestoredFromVersionID, owner); err != nil {
			return err
		}
		_, err := db.Exec(txCtx, `UPDATE personal_documents SET current_version_id=$2::bigint,version=version+1,name=CASE WHEN $3='' THEN name ELSE $3 END,updated_at=NOW() WHERE id=$1::bigint`, id, v.ID, name)
		return err
	}); err != nil {
		return DocumentDetail{}, err
	}
	return r.Get(ctx, owner, id)
}
func (r *PgRepository) SetStatus(ctx context.Context, owner, id string, expected int64, status string) (DocumentDetail, error) {
	cmd, e := r.db(ctx).Exec(ctx, `UPDATE personal_documents SET status=$4,version=version+1,updated_at=NOW(),deleted_at=CASE WHEN $4='trashed' THEN NOW() ELSE NULL END WHERE id=$1::bigint AND owner_user_id=$2::bigint AND version=$3`, id, owner, expected, status)
	if e != nil {
		return DocumentDetail{}, e
	}
	if cmd.RowsAffected() != 1 {
		if _, e = r.Get(ctx, owner, id); e != nil {
			return DocumentDetail{}, e
		}
		return DocumentDetail{}, ErrConflict
	}
	return r.Get(ctx, owner, id)
}
func (r *PgRepository) Version(ctx context.Context, owner, id, vid string) (DocumentVersion, error) {
	if _, e := r.Get(ctx, owner, id); e != nil {
		return DocumentVersion{}, e
	}
	var x DocumentVersion
	e := r.db(ctx).QueryRow(ctx, versionSQL+` WHERE id=$1::bigint AND document_id=$2::bigint`, vid, id).Scan(&x.ID, &x.DocumentID, &x.VersionNumber, &x.SourceObjectID, &x.Format, &x.SizeBytes, &x.SHA256, &x.RestoredFromVersionID, &x.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return DocumentVersion{}, ErrNotFound
	}
	return x, e
}

// PreviewSummary is deliberately aggregate-only. It is used for operational
// telemetry and never returns user, document, version, object, or filename
// data to the metrics path.
func (r *PgRepository) PreviewSummary(ctx context.Context) (map[PreviewMetricKey]int64, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT p.status,COALESCE(v.source_type,'unknown'),COUNT(*)::bigint
		FROM personal_document_previews p
		JOIN personal_document_versions v ON v.id=p.document_version_id
		GROUP BY p.status,COALESCE(v.source_type,'unknown')
		ORDER BY p.status,COALESCE(v.source_type,'unknown')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summary := map[PreviewMetricKey]int64{}
	for rows.Next() {
		var key PreviewMetricKey
		var total int64
		if err := rows.Scan(&key.Status, &key.Format, &total); err != nil {
			return nil, err
		}
		summary[key] = total
	}
	return summary, rows.Err()
}

const detailSQL = `SELECT d.id::text,d.owner_user_id::text,d.name,d.document_type,d.status,COALESCE(d.current_version_id::text,''),d.version,d.created_at,d.updated_at,d.deleted_at,COALESCE(v.id::text,''),COALESCE(v.version_number,0),COALESCE(v.source_object_id::text,''),COALESCE(v.source_type,''),COALESCE(v.size_bytes,0),COALESCE(v.sha256,''),COALESCE(v.restored_from_version_id::text,''),v.created_at FROM personal_documents d LEFT JOIN personal_document_versions v ON v.id=d.current_version_id`
const versionSQL = `SELECT id::text,document_id::text,version_number,source_object_id::text,source_type,size_bytes,sha256,COALESCE(restored_from_version_id::text,''),created_at FROM personal_document_versions`

type scanner interface{ Scan(...any) error }

func scanDetail(s scanner) (DocumentDetail, error) {
	var d Document
	var v DocumentVersion
	var vid string
	var created *time.Time
	e := s.Scan(&d.ID, &d.OwnerID, &d.Name, &d.Format, &d.Status, &d.CurrentVersionID, &d.Version, &d.CreatedAt, &d.UpdatedAt, &d.DeletedAt, &vid, &v.VersionNumber, &v.SourceObjectID, &v.Format, &v.SizeBytes, &v.SHA256, &v.RestoredFromVersionID, &created)
	if e != nil {
		return DocumentDetail{}, e
	}
	if vid != "" {
		v.ID = vid
		v.DocumentID = d.ID
		if created != nil {
			v.CreatedAt = *created
		}
		return DocumentDetail{Document: d, CurrentVersion: &v}, nil
	}
	return DocumentDetail{Document: d}, nil
}
