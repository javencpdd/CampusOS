package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("knowledge resource unavailable")
	ErrConflict = errors.New("knowledge facts changed")
	ErrInvalid  = errors.New("invalid knowledge request")
	ErrDenied   = errors.New("knowledge management denied")
	ErrCapacity = errors.New("knowledge collection capacity reached")
)

type recordKind string

const (
	collections    recordKind = "collections"
	officials      recordKind = "official_sources"
	selections     recordKind = "source_selections"
	members        recordKind = "collection_sources"
	revisions      recordKind = "official_revisions"
	portRepository            = "knowledge.adapter.repository"
)

func table(k recordKind) (string, error) {
	switch k {
	case collections, officials, selections, members, revisions:
		return "knowledge_" + string(k), nil
	}
	return "", ErrInvalid
}

type record struct {
	ID      string
	Version int64
	Data    json.RawMessage
}

// list is capped by its caller. Projection rows contain references, never origin bodies.
type Repository interface {
	lockInventory(context.Context, recordKind) error
	get(context.Context, recordKind, string, bool) (record, error)
	list(context.Context, recordKind, string, int) ([]record, error)
	save(context.Context, recordKind, string, int64, any) (record, error)
	history(context.Context, string, int64) ([]record, error)
	deleteMember(context.Context, string) error
}

func decode[T any](r record) (T, error) {
	var v T
	data := map[string]json.RawMessage{}
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return v, err
	}
	wire, _ := json.Marshal(r.Version)
	data["version"], _ = json.Marshal(string(wire))
	raw, _ := json.Marshal(data)
	err := json.Unmarshal(raw, &v)
	return v, err
}
func encoded(v any) (json.RawMessage, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	var doc map[string]json.RawMessage
	if e = json.Unmarshal(b, &doc); e != nil {
		return nil, e
	}
	delete(doc, "version")
	return json.Marshal(doc)
}

type PgRepository struct{ pool *pgxpool.Pool }

func NewPgRepository(pool *pgxpool.Pool) *PgRepository { return &PgRepository{pool} }
func (p *PgRepository) lockInventory(ctx context.Context, k recordKind) error {
	t, e := table(k)
	if e != nil {
		return e
	}
	if _, ok := transaction.FromContext(ctx); !ok {
		return ErrSourceUnavailable
	}
	_, e = transaction.ExecutorFor(ctx, p.pool).Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", t)
	return e
}
func (p *PgRepository) get(ctx context.Context, k recordKind, id string, write bool) (record, error) {
	t, e := table(k)
	if e != nil {
		return record{}, e
	}
	if _, ok := transaction.FromContext(ctx); !ok {
		return record{}, ErrSourceUnavailable
	}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	var r record
	r.ID = id
	e = transaction.ExecutorFor(ctx, p.pool).QueryRow(ctx, "SELECT data,facts_version FROM "+t+" WHERE id=$1"+lock, id).Scan(&r.Data, &r.Version)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return r, e
}
func (p *PgRepository) list(ctx context.Context, k recordKind, scope string, limit int) ([]record, error) {
	t, e := table(k)
	if e != nil || limit < 1 || limit > MaxMembers+1 {
		return nil, ErrInvalid
	}
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrSourceUnavailable
	}
	where := ""
	args := []any{limit}
	if scope != "" {
		switch k {
		case selections, members:
			where = " WHERE collection_id=$2"
		case revisions:
			where = " WHERE source_id=$2"
		default:
			return nil, ErrInvalid
		}
		args = append(args, scope)
	}
	rows, e := transaction.ExecutorFor(ctx, p.pool).Query(ctx, "SELECT id,data,facts_version FROM "+t+where+" ORDER BY id LIMIT $1 FOR SHARE", args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []record{}
	for rows.Next() {
		var r record
		if e = rows.Scan(&r.ID, &r.Data, &r.Version); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (p *PgRepository) save(ctx context.Context, k recordKind, id string, expected int64, v any) (record, error) {
	t, e := table(k)
	if e != nil {
		return record{}, e
	}
	if _, ok := transaction.FromContext(ctx); !ok {
		return record{}, ErrSourceUnavailable
	}
	b, e := encoded(v)
	if e != nil {
		return record{}, e
	}
	r := record{ID: id}
	if expected == 0 {
		e = transaction.ExecutorFor(ctx, p.pool).QueryRow(ctx, "INSERT INTO "+t+"(id,data) VALUES($1,$2) RETURNING data,facts_version", id, b).Scan(&r.Data, &r.Version)
	} else {
		e = transaction.ExecutorFor(ctx, p.pool).QueryRow(ctx, "UPDATE "+t+" SET data=$2 WHERE id=$1 AND facts_version=$3 RETURNING data,facts_version", id, b, expected).Scan(&r.Data, &r.Version)
	}
	var pe *pgconn.PgError
	if errors.Is(e, pgx.ErrNoRows) || (errors.As(e, &pe) && pe.Code == "23505") {
		e = ErrConflict
	}
	return r, e
}
func (p *PgRepository) history(ctx context.Context, id string, before int64) ([]record, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrSourceUnavailable
	}
	rows, e := transaction.ExecutorFor(ctx, p.pool).Query(ctx, `SELECT id,data,facts_version FROM knowledge_official_revisions WHERE source_id=$1 AND ($2::bigint=0 OR (data->>'official_version')::bigint<$2) ORDER BY (data->>'official_version')::bigint DESC LIMIT 51 FOR SHARE`, id, before)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []record{}
	for rows.Next() {
		var r record
		if e = rows.Scan(&r.ID, &r.Data, &r.Version); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (p *PgRepository) deleteMember(ctx context.Context, id string) error {
	if _, ok := transaction.FromContext(ctx); !ok {
		return ErrSourceUnavailable
	}
	tag, e := transaction.ExecutorFor(ctx, p.pool).Exec(ctx, "DELETE FROM knowledge_collection_sources WHERE id=$1", id)
	if e == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return e
}

type MemoryRepository struct {
	mu   sync.Mutex
	seq  int64
	rows map[recordKind]map[string]record
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{rows: map[recordKind]map[string]record{}}
}
func (m *MemoryRepository) lockInventory(ctx context.Context, _ recordKind) error { return ctx.Err() }
func (m *MemoryRepository) get(ctx context.Context, k recordKind, id string, _ bool) (record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[k][id]
	if !ok {
		return record{}, ErrNotFound
	}
	r.Data = append(json.RawMessage(nil), r.Data...)
	return r, ctx.Err()
}
func (m *MemoryRepository) list(ctx context.Context, k recordKind, scope string, limit int) ([]record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []record{}
	for _, r := range m.rows[k] {
		if scope != "" {
			var doc map[string]any
			_ = json.Unmarshal(r.Data, &doc)
			field := "collection_id"
			if k == revisions {
				field = "source_id"
			}
			if doc[field] != scope {
				continue
			}
		}
		r.Data = append(json.RawMessage(nil), r.Data...)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, ctx.Err()
}
func (m *MemoryRepository) save(ctx context.Context, k recordKind, id string, expected int64, v any) (record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, e := table(k); e != nil {
		return record{}, e
	}
	if m.rows[k] == nil {
		m.rows[k] = map[string]record{}
	}
	old, exists := m.rows[k][id]
	if (expected == 0 && exists) || (expected > 0 && (!exists || old.Version != expected)) || (k == revisions && exists) {
		return record{}, ErrConflict
	}
	b, e := encoded(v)
	if e != nil {
		return record{}, e
	}
	m.seq++
	r := record{ID: id, Version: m.seq, Data: b}
	m.rows[k][id] = r
	return r, ctx.Err()
}
func (m *MemoryRepository) history(ctx context.Context, id string, before int64) ([]record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []record{}
	for _, r := range m.rows[revisions] {
		v, e := decode[OfficialRevision](r)
		if e != nil {
			return nil, e
		}
		if v.SourceID == id && (before == 0 || v.OfficialVersion < before) {
			r.Data = append(json.RawMessage(nil), r.Data...)
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := decode[OfficialRevision](out[i])
		b, _ := decode[OfficialRevision](out[j])
		return a.OfficialVersion > b.OfficialVersion
	})
	if len(out) > 51 {
		out = out[:51]
	}
	return out, ctx.Err()
}
func (m *MemoryRepository) deleteMember(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[members][id]; !ok {
		return ErrNotFound
	}
	delete(m.rows[members], id)
	return ctx.Err()
}
func (m *MemoryRepository) Snapshot() any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[recordKind]map[string]record{}
	for k, rows := range m.rows {
		out[k] = map[string]record{}
		for id, r := range rows {
			r.Data = append(json.RawMessage(nil), r.Data...)
			out[k][id] = r
		}
	}
	return out
}
func (m *MemoryRepository) Restore(snapshot any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = snapshot.(map[recordKind]map[string]record)
} // sequence deliberately never rolls back
func BindPostgreSQLAdapter(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if pool == nil {
		return ErrSourceUnavailable
	}
	return app.Provide(portRepository, Repository(NewPgRepository(pool)))
}
func BindMemoryAdapter(app *platformmodule.AppContext) error {
	return app.Provide(portRepository, Repository(NewMemoryRepository()))
}
