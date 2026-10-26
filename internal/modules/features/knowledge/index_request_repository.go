package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

func (p *PgRepository) requestCurrentIndex(ctx context.Context, kind, id string) (IndexRequest, error) {
	if !validIndexTarget(kind, id) {
		return IndexRequest{}, ErrIndexEventInvalid
	}
	if _, ok := transaction.FromContext(ctx); !ok {
		return IndexRequest{}, ErrSourceUnavailable
	}
	table := "knowledge_collections"
	if kind == "official" {
		table = "knowledge_official_sources"
	}
	// Read only the owning row's epoch. In particular, never SELECT official data.
	var version int64
	e := transaction.ExecutorFor(ctx, p.pool).QueryRow(ctx, "SELECT facts_version FROM "+table+" WHERE id=$1 FOR SHARE", id).Scan(&version)
	if errors.Is(e, pgx.ErrNoRows) {
		version = 0
	} else if e != nil {
		return IndexRequest{}, e
	}
	request := IndexRequest{ID: indexRequestID(kind, id, version), TargetKind: kind, TargetID: id, TargetVersion: version, Status: "pending"}
	inserted, e := transaction.ExecutorFor(ctx, p.pool).Exec(ctx, `INSERT INTO knowledge_index_tasks(id,target_kind,target_id,target_version) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO NOTHING`, request.ID, kind, id, version)
	if e != nil {
		return IndexRequest{}, e
	}
	e = transaction.ExecutorFor(ctx, p.pool).QueryRow(ctx, `SELECT target_kind,target_id,target_version,status,created_at FROM knowledge_index_tasks WHERE id=$1`, request.ID).Scan(&request.TargetKind, &request.TargetID, &request.TargetVersion, &request.Status, &request.CreatedAt)
	if e != nil {
		return IndexRequest{}, e
	}
	if request.TargetKind != kind || request.TargetID != id || request.TargetVersion != version || request.Status != "pending" {
		return IndexRequest{}, ErrConflict
	}
	if kind == "collection" {
		if e = p.ensureIndexJob(ctx, id, inserted.RowsAffected() > 0); e != nil {
			return IndexRequest{}, e
		}
	}
	return request, nil
}
func (m *MemoryRepository) requestCurrentIndex(ctx context.Context, kind, id string) (IndexRequest, error) {
	if !validIndexTarget(kind, id) {
		return IndexRequest{}, ErrIndexEventInvalid
	}
	if !transaction.Active(ctx) {
		return IndexRequest{}, ErrSourceUnavailable
	}
	m.mu.Lock()
	if e := ctx.Err(); e != nil {
		m.mu.Unlock()
		return IndexRequest{}, e
	}
	target := collections
	if kind == "official" {
		target = officials
	}
	version := m.rows[target][id].Version
	key := indexRequestID(kind, id, version)
	if previous, ok := m.rows[indexRequestRecords][key]; ok {
		var request IndexRequest
		e := json.Unmarshal(previous.Data, &request)
		m.mu.Unlock()
		if e == nil && kind == "collection" {
			e = m.ensureIndexJob(ctx, id, false)
		}
		return request, e
	}
	request := IndexRequest{ID: key, TargetKind: kind, TargetID: id, TargetVersion: version, Status: "pending", CreatedAt: time.Now().UTC()}
	data, e := json.Marshal(request)
	if e != nil {
		m.mu.Unlock()
		return IndexRequest{}, e
	}
	if m.rows[indexRequestRecords] == nil {
		m.rows[indexRequestRecords] = map[string]record{}
	}
	m.rows[indexRequestRecords][key] = record{ID: key, Data: data}
	m.mu.Unlock()
	if kind == "collection" {
		if e = m.ensureIndexJob(ctx, id, true); e != nil {
			return IndexRequest{}, e
		}
	}
	return request, nil
}
