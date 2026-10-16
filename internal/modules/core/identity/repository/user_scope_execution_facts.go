package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var ErrUserScopeFactsUnavailable = errors.New("user scoped execution facts unavailable")

// These complete private storage snapshots are never an HTTP permission DTO.
// Identity must still prove the current User and decide the named execution.
type UserScopedExecutionFact struct {
	Assignment json.RawMessage
	Role       json.RawMessage
	Link       json.RawMessage
	Definition json.RawMessage
}
type UserScopedExecutionFactsReader interface {
	LoadUserScopedExecutionFacts(context.Context, string, int64, bool) ([]UserScopedExecutionFact, error)
}

func (r *PgRoleRepository) requireUserScopeFacts(ctx context.Context) error {
	var managed bool
	err := r.db(ctx).QueryRow(ctx, `SELECT
 (SELECT count(*)=4 FROM (VALUES
 ('public.roles','trg_roles_user_scope_facts_version'),
 ('public.user_roles','trg_user_roles_user_scope_facts_version'),
 ('public.role_permissions','trg_role_permissions_user_scope_facts_version'),
 ('public.permission_definitions','trg_permission_definitions_user_scope_facts_version')
 ) x(tbl,trg) JOIN pg_trigger t ON t.tgrelid=to_regclass(x.tbl) AND t.tgname=x.trg
 WHERE NOT t.tgisinternal AND t.tgenabled IN('O','A') AND t.tgtype=23
 AND t.tgfoid=to_regprocedure('public.advance_user_scope_facts_version()'))
 AND EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.user_scope_facts_version_seq')
 AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1)`).Scan(&managed)
	if err != nil || !managed {
		return ErrUserScopeFactsUnavailable
	}
	return nil
}

// Scope selection precedes snapshot/page formation. lock=true is meaningful
// only in the platform transaction and locks all four actual storage rows.
func (r *PgRoleRepository) LoadUserScopedExecutionFacts(ctx context.Context, userID string, boardID int64, lock bool) ([]UserScopedExecutionFact, error) {
	n, err := strconv.ParseInt(userID, 10, 64)
	if r == nil || r.pool == nil || err != nil || n <= 0 || boardID <= 0 {
		return nil, ErrUserScopeFactsUnavailable
	}
	if lock {
		if _, ok := transaction.FromContext(ctx); !ok {
			return nil, ErrUserScopeFactsUnavailable
		}
	}
	if err := r.requireUserScopeFacts(ctx); err != nil {
		return nil, err
	}
	query := `SELECT to_jsonb(ur),to_jsonb(r),to_jsonb(rp),to_jsonb(pd)
 FROM public.user_roles ur
 JOIN public.roles r ON r.id=ur.role_id AND r.deleted_at IS NULL
 JOIN public.role_permissions rp ON rp.role_id=r.id AND rp.deleted_at IS NULL
 JOIN public.permission_definitions pd ON pd.id=rp.permission_id AND pd.deprecated_at IS NULL
 WHERE ur.user_id=$1 AND ur.deleted_at IS NULL AND
 ((ur.scope_type='global' AND ur.scope_id IS NULL) OR (ur.scope_type='category' AND ur.scope_id=$2))
 ORDER BY ur.id,rp.id`
	if lock {
		query += " FOR UPDATE OF ur,r,rp,pd"
	}
	rows, err := r.db(ctx).Query(ctx, query, userID, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	facts := make([]UserScopedExecutionFact, 0)
	for rows.Next() {
		var assignment, role, link, definition []byte
		if err := rows.Scan(&assignment, &role, &link, &definition); err != nil {
			return nil, err
		}
		facts = append(facts, UserScopedExecutionFact{Assignment: append(json.RawMessage(nil), assignment...), Role: append(json.RawMessage(nil), role...), Link: append(json.RawMessage(nil), link...), Definition: append(json.RawMessage(nil), definition...)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	// Check after the relation/row locks too: an intervening DDL change must
	// never leave a stale successful trigger probe bound to these rows.
	if err := r.requireUserScopeFacts(ctx); err != nil {
		return nil, err
	}
	return facts, nil
}
