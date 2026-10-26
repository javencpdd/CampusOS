package knowledge

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type privateRow struct {
	Title     string            `json:"title,omitempty"`
	ID        string            `json:"id"`
	Owner     string            `json:"-"`
	Kind      string            `json:"kind"`
	SourceID  string            `json:"source_id"`
	Revision  string            `json:"revision"`
	Format    string            `json:"format"`
	Version   int64             `json:"version,string"`
	State     string            `json:"state"`
	Envelope  security.Envelope `json:"-"`
	TaskState string            `json:"task_state"`
	Attempts  int               `json:"attempts"`
	ErrorCode string            `json:"error_code"`
	Lease     int64             `json:"-"`
}

const privateSelect = `SELECT s.id,s.owner_user_id::text,s.kind,s.source_id::text,s.revision,s.format,s.consent_version,s.state,s.nonce,s.ciphertext,COALESCE(t.state,''),COALESCE(t.attempts,0),COALESCE(t.error_code,''),COALESCE(t.lease_token,0) FROM knowledge_private_sources s LEFT JOIN knowledge_private_tasks t ON t.source_id=s.id `

func scanPrivate(row pgx.Row) (privateRow, error) {
	var r privateRow
	err := row.Scan(&r.ID, &r.Owner, &r.Kind, &r.SourceID, &r.Revision, &r.Format, &r.Version, &r.State, &r.Envelope.Nonce, &r.Envelope.Ciphertext, &r.TaskState, &r.Attempts, &r.ErrorCode, &r.Lease)
	r.Envelope.KeyID = "private-v1"
	r.Envelope.Algorithm = security.AlgorithmAES256GCM
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrPrivateSourceMissing
	}
	return r, err
}
func (p *PgRepository) privateGet(ctx context.Context, owner, id string, lock bool) (privateRow, error) {
	q := privateSelect + ` WHERE s.owner_user_id=$1::bigint AND s.id=$2`
	if lock {
		q += ` FOR UPDATE OF s`
	}
	return scanPrivate(transaction.ExecutorFor(ctx, p.pool).QueryRow(ctx, q, owner, id))
}
func (p *PgRepository) privateList(ctx context.Context, owner string) ([]privateRow, error) {
	rows, err := transaction.ExecutorFor(ctx, p.pool).Query(ctx, privateSelect+` WHERE s.owner_user_id=$1::bigint ORDER BY s.id LIMIT 101`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []privateRow{}
	for rows.Next() {
		r, e := scanPrivate(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (p *PgRepository) privateInsert(ctx context.Context, r privateRow) (privateRow, error) {
	if !transaction.Active(ctx) {
		return privateRow{}, ErrPrivateUnavailable
	}
	db := transaction.ExecutorFor(ctx, p.pool)
	// Serialize only the one owner's bounded inventory, not all private users.
	if _, e := db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('knowledge.private.owner:'||$1,0))`, r.Owner); e != nil {
		return privateRow{}, e
	}
	var count int
	if e := db.QueryRow(ctx, `SELECT count(*) FROM knowledge_private_sources WHERE owner_user_id=$1::bigint`, r.Owner).Scan(&count); e != nil {
		return privateRow{}, e
	}
	if count >= 100 {
		return privateRow{}, ErrCapacity
	}
	var version int64
	e := db.QueryRow(ctx, `INSERT INTO knowledge_private_sources(id,owner_user_id,kind,source_id,revision,format,nonce,ciphertext) VALUES($1,$2::bigint,$3,$4::bigint,$5,$6,$7,$8) RETURNING consent_version`, r.ID, r.Owner, r.Kind, r.SourceID, r.Revision, r.Format, r.Envelope.Nonce, r.Envelope.Ciphertext).Scan(&version)
	if e != nil {
		var pgerr *pgconn.PgError
		if errors.As(e, &pgerr) && pgerr.Code == "23505" {
			return privateRow{}, ErrConflict
		}
		return privateRow{}, e
	}
	if _, e = db.Exec(ctx, `INSERT INTO knowledge_private_tasks(source_id,consent_version) VALUES($1,$2)`, r.ID, version); e != nil {
		return privateRow{}, e
	}
	return p.privateGet(ctx, r.Owner, r.ID, false)
}
func (p *PgRepository) privateDelete(ctx context.Context, owner, id string) error {
	if !transaction.Active(ctx) {
		return ErrPrivateUnavailable
	}
	_, e := transaction.ExecutorFor(ctx, p.pool).Exec(ctx, `DELETE FROM knowledge_private_sources WHERE owner_user_id=$1::bigint AND id=$2`, owner, id)
	return e
}
func (p *PgRepository) privateClaim(ctx context.Context, now int64) (privateRow, error) {
	if !transaction.Active(ctx) {
		return privateRow{}, ErrPrivateUnavailable
	}
	db := transaction.ExecutorFor(ctx, p.pool)
	// Source then task is also the owner withdrawal/publication lock order.
	r, e := scanPrivate(db.QueryRow(ctx, privateSelect+` WHERE s.id=(SELECT source_id FROM knowledge_private_tasks WHERE (state IN('pending','retry','idle') AND available_at_ms<=$1) OR (state='processing' AND lease_until_ms<=$1) ORDER BY available_at_ms,source_id LIMIT 1) FOR UPDATE OF s SKIP LOCKED`, now))
	if e != nil {
		return r, e
	}
	var current string
	if e = db.QueryRow(ctx, `SELECT state FROM knowledge_private_tasks WHERE source_id=$1 FOR UPDATE`, r.ID).Scan(&current); e != nil {
		return r, e
	}
	e = db.QueryRow(ctx, `UPDATE knowledge_private_tasks SET state='processing',attempts=CASE WHEN state='idle' THEN 1 ELSE LEAST(attempts+1,5) END,lease_token=nextval('knowledge_private_epoch_seq'),lease_until_ms=$2+5000 WHERE source_id=$1 AND ((state IN('pending','retry','idle') AND available_at_ms<=$2) OR(state='processing' AND lease_until_ms<=$2)) RETURNING lease_token,attempts`, r.ID, now).Scan(&r.Lease, &r.Attempts)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrPrivateSourceMissing
	}
	return r, e
}
func (p *PgRepository) privateFinish(ctx context.Context, r privateRow, now int64, code string) error {
	if !transaction.Active(ctx) {
		return ErrPrivateUnavailable
	}
	db := transaction.ExecutorFor(ctx, p.pool)
	current, e := p.privateGet(ctx, r.Owner, r.ID, true)
	if e != nil {
		return e
	}
	if current.Version != r.Version {
		return ErrConflict
	}
	state, ready, next := "idle", true, now+2000
	if code != "" {
		state, ready, next = "retry", false, now+int64(r.Attempts)*1000
		if r.Attempts >= 5 {
			state = "dead"
		}
	}
	result, e := db.Exec(ctx, `UPDATE knowledge_private_tasks SET state=$4,error_code=$5,lease_until_ms=0,available_at_ms=$6 WHERE source_id=$1 AND consent_version=$2 AND state='processing' AND lease_token=$3 AND lease_until_ms>$7`, r.ID, r.Version, r.Lease, state, code, next, now)
	if e != nil {
		return e
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	if ready {
		_, e = db.Exec(ctx, `UPDATE knowledge_private_sources SET state='ready' WHERE id=$1`, r.ID)
	}
	return e
}
