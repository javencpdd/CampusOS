package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

const (
	indexJobs      recordKind = "index_jobs"
	indexDocuments recordKind = "index_documents"
)

type indexStore interface {
	indexDocumentCount(context.Context, string) (int, error)
	Repository
	selectIndexJob(context.Context, int64) (record, error)
	indexToken(context.Context) (int64, error)
	ensureIndexJob(context.Context, string, bool) error
	invalidateIndexes(context.Context) error
	indexPage(context.Context, recordKind, string, string, int) ([]record, error)
	deleteIndexDocument(context.Context, string) error
	documentCollection(context.Context, string) (string, error)
	retrievalPage(context.Context, string, string, int) ([]IndexDocument, error)
}

// Metadata lookup only; CurrentDocument then locks collection, job and actual
// document in executor order and checks this hint against the locked document.
func (p *PgRepository) documentCollection(ctx context.Context, id string) (string, error) {
	if !transaction.Active(ctx) {
		return "", ErrSourceUnavailable
	}
	var collection string
	err := transaction.ExecutorFor(ctx, p.pool).QueryRow(ctx, `SELECT collection_id FROM knowledge_index_documents WHERE id=$1`, id).Scan(&collection)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return collection, err
}
func (m *MemoryRepository) documentCollection(ctx context.Context, id string) (string, error) {
	if !transaction.Active(ctx) {
		return "", ErrSourceUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[indexDocuments][id]
	if !ok {
		return "", ErrNotFound
	}
	d, err := decode[IndexDocument](r)
	return d.CollectionID, err
}

func (p *PgRepository) selectIndexJob(ctx context.Context, now int64) (record, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return record{}, ErrSourceUnavailable
	}
	var row record
	err := transaction.ExecutorFor(ctx, p.pool).QueryRow(ctx, `SELECT id,data,facts_version FROM knowledge_index_jobs
	 WHERE (state IN ('pending','retry','idle','dead') AND available_at_ms<=$1) OR (state='processing' AND lease_until_ms<=$1)
	 ORDER BY available_at_ms,id LIMIT 1 FOR UPDATE SKIP LOCKED`, now).Scan(&row.ID, &row.Data, &row.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return row, err
}
func (p *PgRepository) indexToken(ctx context.Context) (int64, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return 0, ErrSourceUnavailable
	}
	var token int64
	err := transaction.ExecutorFor(ctx, p.pool).QueryRow(ctx, `SELECT nextval('knowledge_index_epoch_seq')`).Scan(&token)
	return token, err
}
func (p *PgRepository) ensureIndexJob(ctx context.Context, id string, dirty bool) error {
	if !opaque("kc_", id) {
		return ErrInvalid
	}
	if _, ok := transaction.FromContext(ctx); !ok {
		return ErrSourceUnavailable
	}
	data, err := encoded(newIndexJob(id))
	if err != nil {
		return err
	}
	executor := transaction.ExecutorFor(ctx, p.pool)
	_, err = executor.Exec(ctx, `INSERT INTO knowledge_index_jobs(id,data) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, data)
	if err == nil && dirty {
		_, err = executor.Exec(ctx, `UPDATE knowledge_index_jobs SET data=data || jsonb_build_object(
		 'requested',(((data->>'requested')::bigint+1)::text),
		 'state',CASE WHEN state IN ('idle','dead') THEN 'pending' ELSE state END,
		 'available_at_ms',CASE WHEN state IN ('idle','dead') THEN '0' ELSE (data->>'available_at_ms') END) WHERE id=$1`, id)
	}
	return err
}
func (p *PgRepository) invalidateIndexes(ctx context.Context) error {
	if _, ok := transaction.FromContext(ctx); !ok {
		return ErrSourceUnavailable
	}
	_, err := transaction.ExecutorFor(ctx, p.pool).Exec(ctx, `UPDATE knowledge_index_jobs SET data=data || jsonb_build_object(
	 'requested',(((data->>'requested')::bigint+1)::text),
	 'state',CASE WHEN state IN ('idle','dead') THEN 'pending' ELSE state END,
	 'available_at_ms',CASE WHEN state IN ('idle','dead') THEN '0' ELSE (data->>'available_at_ms') END)`)
	return err
}
func (p *PgRepository) indexPage(ctx context.Context, kind recordKind, collection, after string, limit int) ([]record, error) {
	return p.scopedIndexPage(ctx, kind, collection, after, limit, 33)
}
func (p *PgRepository) retrievalPage(ctx context.Context, collection, after string, limit int) ([]IndexDocument, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrSourceUnavailable
	}
	if !opaque("kc_", collection) || limit < 1 || limit > 128 {
		return nil, ErrInvalid
	}
	// Native JSONB transport plus one strict DTO decode avoids per-field SQL
	// transformations on every row. The read remains locked and bounded.
	rows, err := transaction.ExecutorFor(ctx, p.pool).Query(ctx, `SELECT id,data,facts_version FROM knowledge_index_documents WHERE collection_id=$1 AND id>$2 ORDER BY id LIMIT $3 FOR SHARE`, collection, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []IndexDocument{}
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.ID, &r.Data, &r.Version); err != nil {
			return nil, err
		}
		d, err := decode[IndexDocument](r)
		if err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
func (p *PgRepository) scopedIndexPage(ctx context.Context, kind recordKind, collection, after string, limit, maxPage int) ([]record, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrSourceUnavailable
	}
	if kind != members && kind != indexDocuments || !opaque("kc_", collection) || limit < 1 || limit > maxPage {
		return nil, ErrInvalid
	}
	t, _ := table(kind)
	rows, err := transaction.ExecutorFor(ctx, p.pool).Query(ctx, "SELECT id,data,facts_version FROM "+t+" WHERE collection_id=$1 AND id>$2 ORDER BY id LIMIT $3 FOR SHARE", collection, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []record{}
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.ID, &r.Data, &r.Version); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func (p *PgRepository) deleteIndexDocument(ctx context.Context, id string) error {
	if _, ok := transaction.FromContext(ctx); !ok {
		return ErrSourceUnavailable
	}
	_, err := transaction.ExecutorFor(ctx, p.pool).Exec(ctx, `DELETE FROM knowledge_index_documents WHERE id=$1`, id)
	return err
}

func (m *MemoryRepository) selectIndexJob(ctx context.Context, now int64) (record, error) {
	if !transaction.Active(ctx) {
		return record{}, ErrSourceUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	candidates := []IndexJob{}
	for _, row := range m.rows[indexJobs] {
		job, err := decode[IndexJob](row)
		if err != nil {
			return record{}, err
		}
		if job.State == "processing" && job.LeaseUntilMS <= now || job.State != "processing" && job.AvailableAtMS <= now {
			candidates = append(candidates, job)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].AvailableAtMS == candidates[j].AvailableAtMS {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].AvailableAtMS < candidates[j].AvailableAtMS
	})
	if len(candidates) == 0 {
		return record{}, ErrNotFound
	}
	return m.rows[indexJobs][candidates[0].ID], ctx.Err()
}
func (m *MemoryRepository) indexToken(ctx context.Context) (int64, error) {
	if !transaction.Active(ctx) {
		return 0, ErrSourceUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	return m.seq, ctx.Err()
}
func (m *MemoryRepository) ensureIndexJob(ctx context.Context, id string, dirty bool) error {
	if !opaque("kc_", id) || !transaction.Active(ctx) {
		return ErrInvalid
	}
	job, err := load[IndexJob](ctx, m, indexJobs, id, true)
	if errors.Is(err, ErrNotFound) {
		job, err = newIndexJob(id), nil
	}
	if err != nil {
		return err
	}
	if dirty {
		job.Requested++
		if job.State == "idle" || job.State == "dead" {
			job.State, job.AvailableAtMS = "pending", 0
		}
	}
	if job.Version == 0 || dirty {
		_, err = save(ctx, m, indexJobs, id, job.Version, job)
	}
	return err
}
func (m *MemoryRepository) invalidateIndexes(ctx context.Context) error {
	if !transaction.Active(ctx) {
		return ErrSourceUnavailable
	}
	m.mu.Lock()
	ids := []string{}
	for id := range m.rows[indexJobs] {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		if err := m.ensureIndexJob(ctx, id, true); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func (m *MemoryRepository) indexPage(ctx context.Context, kind recordKind, collection, after string, limit int) ([]record, error) {
	return m.scopedIndexPage(ctx, kind, collection, after, limit, 33)
}
func (m *MemoryRepository) retrievalPage(ctx context.Context, collection, after string, limit int) ([]IndexDocument, error) {
	if limit < 1 || limit > 128 {
		return nil, ErrInvalid
	}
	rows, err := m.scopedIndexPage(ctx, indexDocuments, collection, after, limit, 128)
	if err != nil {
		return nil, err
	}
	result := []IndexDocument{}
	for _, r := range rows {
		d, err := decode[IndexDocument](r)
		if err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, nil
}
func (m *MemoryRepository) scopedIndexPage(ctx context.Context, kind recordKind, collection, after string, limit, maxPage int) ([]record, error) {
	if !transaction.Active(ctx) {
		return nil, ErrSourceUnavailable
	}
	if kind != members && kind != indexDocuments || !opaque("kc_", collection) || limit < 1 || limit > maxPage {
		return nil, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := []record{}
	for _, row := range m.rows[kind] {
		var bound struct {
			CollectionID string `json:"collection_id"`
		}
		if err := json.Unmarshal(row.Data, &bound); err != nil {
			return nil, err
		}
		if bound.CollectionID == collection && row.ID > after {
			row.Data = append(json.RawMessage(nil), row.Data...)
			result = append(result, row)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, ctx.Err()
}
func (m *MemoryRepository) deleteIndexDocument(ctx context.Context, id string) error {
	if !transaction.Active(ctx) {
		return ErrSourceUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows[indexDocuments], id)
	return ctx.Err()
}

func (p *PgRepository) indexDocumentCount(ctx context.Context, id string) (int, error) {
	if !transaction.Active(ctx) {
		return 0, ErrSourceUnavailable
	}
	var count int
	err := transaction.ExecutorFor(ctx, p.pool).QueryRow(ctx, `SELECT count(*) FROM knowledge_index_documents WHERE collection_id=$1`, id).Scan(&count)
	return count, err
}
func (m *MemoryRepository) indexDocumentCount(ctx context.Context, id string) (int, error) {
	if !transaction.Active(ctx) {
		return 0, ErrSourceUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, r := range m.rows[indexDocuments] {
		d, e := decode[IndexDocument](r)
		if e != nil {
			return 0, e
		}
		if d.CollectionID == id {
			count++
		}
	}
	return count, nil
}
