package space

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

const spaceOwnerAdmissionSQL = "SELECT pg_advisory_xact_lock(hashtextextended('personal-space:' || $1::text,0))"

func (r *PgRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}
func (r *PgRepository) withinSpaceOwner(ctx context.Context, owner string, fn func(context.Context) error) error {
	return transaction.NewPostgreSQL(r.pool).Within(ctx, func(tx context.Context) error {
		if _, err := r.db(tx).Exec(tx, spaceOwnerAdmissionSQL, owner); err != nil {
			return err
		}
		return fn(tx)
	})
}
func (r *PgRepository) CheckSpaceAuthority(ctx context.Context) error {
	if r == nil || r.pool == nil {
		return ErrSpaceFactsUnavailable
	}
	var valid bool
	err := r.db(ctx).QueryRow(ctx, `SELECT
 (SELECT count(*)=2 FROM (VALUES
 ('public.user_spaces','trg_user_spaces_facts_version'),
 ('public.user_space_style_snapshots','trg_space_style_snapshots_facts_version')) x(tbl,trg)
 JOIN pg_trigger t ON t.tgrelid=to_regclass(x.tbl) AND t.tgname=x.trg
 WHERE NOT t.tgisinternal AND t.tgenabled IN('O','A') AND t.tgtype=23
 AND t.tgfoid=to_regprocedure('public.advance_personal_space_facts_version()'))
 AND EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.personal_space_facts_version_seq')
 AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1)
 AND (SELECT count(*)=2 FROM pg_attribute WHERE attrelid IN(to_regclass('public.user_spaces'),to_regclass('public.user_space_style_snapshots'))
 AND attname='facts_version' AND attnotnull AND atttypid='bigint'::regtype AND NOT attisdropped)`).Scan(&valid)
	if err != nil || !valid {
		return errors.Join(ErrSpaceFactsUnavailable, err)
	}
	return nil
}
func (r *PgRepository) LoadSpaceAuthority(ctx context.Context, owner string, lock bool) (json.RawMessage, error) {
	if err := r.CheckSpaceAuthority(ctx); err != nil {
		return nil, err
	}
	if lock {
		if _, ok := transaction.FromContext(ctx); !ok {
			return nil, ErrSpaceFactsUnavailable
		}
		if _, err := r.db(ctx).Exec(ctx, spaceOwnerAdmissionSQL, owner); err != nil {
			return nil, err
		}
	}
	query := "SELECT to_jsonb(s) FROM public.user_spaces s WHERE user_id=$1 AND deleted_at IS NULL"
	if lock {
		query += " FOR UPDATE"
	}
	var raw []byte
	err := r.db(ctx).QueryRow(ctx, query, owner).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrSpaceNotFound
		}
		return nil, err
	}
	if err := r.CheckSpaceAuthority(ctx); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), raw...), nil
}
func (r *PgRepository) LoadStyleSnapshotAuthority(ctx context.Context, owner string, lock bool) ([]json.RawMessage, error) {
	if err := r.CheckSpaceAuthority(ctx); err != nil {
		return nil, err
	}
	if lock {
		if _, ok := transaction.FromContext(ctx); !ok {
			return nil, ErrSpaceFactsUnavailable
		}
		if _, err := r.db(ctx).Exec(ctx, spaceOwnerAdmissionSQL, owner); err != nil {
			return nil, err
		}
	}
	query := "SELECT to_jsonb(s) FROM public.user_space_style_snapshots s WHERE user_id=$1 ORDER BY id"
	if lock {
		query += " FOR UPDATE"
	}
	rows, err := r.db(ctx).Query(ctx, query, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	facts := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		facts = append(facts, append(json.RawMessage(nil), raw...))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := r.CheckSpaceAuthority(ctx); err != nil {
		return nil, err
	}
	return facts, nil
}
