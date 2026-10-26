package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

func backfillTarget(kind string, limit int) (recordKind, error) {
	if limit < 1 || limit > indexBackfillPageSize {
		return "", ErrInvalid
	}
	switch kind {
	case "collection":
		return collections, nil
	case "official":
		return officials, nil
	}
	return "", ErrInvalid
}
func finishBackfillPage(cp indexBackfillCheckpoint, ids []string, limit int) indexBackfillPage {
	result := indexBackfillPage{Checkpoint: cp, Advanced: true, Targets: len(ids)}
	result.Checkpoint.AfterID = ""
	if len(ids) > limit {
		result.Targets = limit
		result.Checkpoint.AfterID = ids[limit-1]
	} else {
		result.Checkpoint.CompletedPasses++
	}
	return result
}

func (p *PgRepository) backfillIndexPage(ctx context.Context, kind string, limit int) (indexBackfillPage, error) {
	k, err := backfillTarget(kind, limit)
	if err != nil {
		return indexBackfillPage{}, err
	}
	if _, ok := transaction.FromContext(ctx); !ok {
		return indexBackfillPage{}, ErrSourceUnavailable
	}
	executor := transaction.ExecutorFor(ctx, p.pool)
	if _, err = executor.Exec(ctx, `INSERT INTO knowledge_index_backfill_checkpoints(target_kind) VALUES($1) ON CONFLICT DO NOTHING`, kind); err != nil {
		return indexBackfillPage{}, err
	}
	var cp indexBackfillCheckpoint
	err = executor.QueryRow(ctx, `SELECT target_kind,after_id,completed_passes,next_scan_at,updated_at
	 FROM knowledge_index_backfill_checkpoints WHERE target_kind=$1 AND next_scan_at<=clock_timestamp() FOR UPDATE SKIP LOCKED`, kind).
		Scan(&cp.TargetKind, &cp.AfterID, &cp.CompletedPasses, &cp.NextScanAt, &cp.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return indexBackfillPage{}, nil // another scanner owns this page, or idle
	}
	if err != nil {
		return indexBackfillPage{}, err
	}
	targetTable, _ := table(k)
	// Enumerate IDs only, with the same ordering for the predicate and cursor.
	// Never use Repository.list: it reads JSON bodies and is not a durable page.
	rows, err := executor.Query(ctx, "SELECT id FROM "+targetTable+" WHERE id>$1 ORDER BY id LIMIT $2", cp.AfterID, limit+1)
	if err != nil {
		return indexBackfillPage{}, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return indexBackfillPage{}, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return indexBackfillPage{}, err
	}
	result := finishBackfillPage(cp, ids, limit)
	for _, id := range ids[:result.Targets] {
		// Reload the current epoch under SHARE lock, including zero if the row
		// disappeared after enumeration. Task and checkpoint share this commit.
		if _, err = p.requestCurrentIndex(ctx, kind, id); err != nil {
			return indexBackfillPage{}, err
		}
	}
	idleSeconds := int64(0)
	if result.Checkpoint.AfterID == "" {
		idleSeconds = int64(indexBackfillIdle / time.Second)
	}
	err = executor.QueryRow(ctx, `UPDATE knowledge_index_backfill_checkpoints SET after_id=$2,completed_passes=$3,
	 next_scan_at=clock_timestamp()+make_interval(secs=>$4::double precision),updated_at=clock_timestamp() WHERE target_kind=$1
	 RETURNING next_scan_at,updated_at`, kind, result.Checkpoint.AfterID, result.Checkpoint.CompletedPasses, idleSeconds).
		Scan(&result.Checkpoint.NextScanAt, &result.Checkpoint.UpdatedAt)
	return result, err
}

func (m *MemoryRepository) backfillIndexPage(ctx context.Context, kind string, limit int) (indexBackfillPage, error) {
	k, err := backfillTarget(kind, limit)
	if err != nil {
		return indexBackfillPage{}, err
	}
	if !transaction.Active(ctx) {
		return indexBackfillPage{}, ErrSourceUnavailable
	}
	m.mu.Lock()
	cp := indexBackfillCheckpoint{TargetKind: kind}
	if previous, ok := m.rows[indexBackfillCheckpoints][kind]; ok {
		err = json.Unmarshal(previous.Data, &cp)
	}
	ids := []string{}
	idle := cp.NextScanAt.After(time.Now())
	if err == nil && !idle {
		for id := range m.rows[k] {
			if id > cp.AfterID {
				ids = append(ids, id)
			}
		}
	}
	m.mu.Unlock()
	if err != nil {
		return indexBackfillPage{}, err
	}
	if idle {
		return indexBackfillPage{}, ctx.Err()
	}
	sort.Strings(ids)
	if len(ids) > limit+1 {
		ids = ids[:limit+1]
	}
	result := finishBackfillPage(cp, ids, limit)
	for _, id := range ids[:result.Targets] {
		if _, err = m.requestCurrentIndex(ctx, kind, id); err != nil {
			return indexBackfillPage{}, err
		}
	}
	result.Checkpoint.UpdatedAt = time.Now().UTC()
	result.Checkpoint.NextScanAt = result.Checkpoint.UpdatedAt
	if result.Checkpoint.AfterID == "" {
		result.Checkpoint.NextScanAt = result.Checkpoint.NextScanAt.Add(indexBackfillIdle)
	}
	data, err := json.Marshal(result.Checkpoint)
	if err != nil {
		return indexBackfillPage{}, err
	}
	m.mu.Lock()
	if m.rows[indexBackfillCheckpoints] == nil {
		m.rows[indexBackfillCheckpoints] = map[string]record{}
	}
	m.rows[indexBackfillCheckpoints][kind] = record{ID: kind, Data: data}
	m.mu.Unlock()
	return result, ctx.Err()
}
