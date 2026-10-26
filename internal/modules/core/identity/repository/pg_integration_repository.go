package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgIntegrationRepository struct{ pool *pgxpool.Pool }

func NewPgIntegrationRepository(pool *pgxpool.Pool) *PgIntegrationRepository {
	return &PgIntegrationRepository{pool: pool}
}
func (r *PgIntegrationRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}
func integrationRowError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIntegrationNotFound
	}
	return err
}

const integrationPrincipalColumns = `id,name,status,facts_version,created_by_admin_id,created_at,updated_at`
const integrationCredentialColumns = `id,principal_id,prefix,digest,purpose,status,rules,not_before_ms,expires_at_ms,facts_version,created_by_admin_id,created_at,updated_at`

func scanIntegrationPrincipal(row pgx.Row) (p IntegrationPrincipalRecord, err error) {
	err = row.Scan(&p.ID, &p.Name, &p.Status, &p.Version, &p.CreatedByAdminID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, integrationRowError(err)
	}
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	if !validIntegrationPrincipal(p) || p.Version < 1 {
		return p, ErrIntegrationStoreUnavailable
	}
	return
}
func scanIntegrationCredential(row pgx.Row) (c IntegrationCredentialRecord, err error) {
	var digest, raw []byte
	err = row.Scan(&c.ID, &c.PrincipalID, &c.Prefix, &digest, &c.Purpose, &c.Status, &raw, &c.NotBeforeMS, &c.ExpiresAtMS, &c.Version, &c.CreatedByAdminID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return c, integrationRowError(err)
	}
	if len(digest) != 32 || json.Unmarshal(raw, &c.Rules) != nil {
		return c, ErrIntegrationStoreUnavailable
	}
	copy(c.Digest[:], digest)
	c.CreatedAt = c.CreatedAt.UTC()
	c.UpdatedAt = c.UpdatedAt.UTC()
	if !validIntegrationCredential(c) || c.Version < 1 {
		return c, ErrIntegrationStoreUnavailable
	}
	return
}
func (r *PgIntegrationRepository) CreateIntegrationPrincipal(ctx context.Context, p *IntegrationPrincipalRecord) error {
	if p == nil || !validIntegrationPrincipal(*p) {
		return ErrIntegrationConflict
	}
	out, err := scanIntegrationPrincipal(r.db(ctx).QueryRow(ctx, `INSERT INTO integration_principals(id,name,status,created_by_admin_id) VALUES($1,$2,$3,$4) RETURNING `+integrationPrincipalColumns, p.ID, p.Name, p.Status, p.CreatedByAdminID))
	if err == nil {
		*p = out
	}
	return err
}

// The trigger/sequence guards are part of authentication facts. A disabled
// version trigger is an unavailable proof source, never a legacy fallback.
func (r *PgIntegrationRepository) managed(ctx context.Context) error {
	var ok bool
	err := r.db(ctx).QueryRow(ctx, `SELECT (SELECT count(*)=2 FROM pg_trigger WHERE tgrelid IN('public.integration_principals'::regclass,'public.integration_credentials'::regclass) AND tgname IN('trg_integration_principal_facts','trg_integration_credential_facts') AND NOT tgisinternal AND tgenabled IN('O','A') AND tgfoid=to_regprocedure('public.advance_integration_facts()')) AND EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.integration_facts_version_seq') AND NOT seqcycle)`).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIntegrationStoreUnavailable
	}
	return nil
}
func (r *PgIntegrationRepository) GetIntegrationPrincipal(ctx context.Context, id string, lock bool) (IntegrationPrincipalRecord, error) {
	if err := integrationLock(ctx, lock); err != nil {
		return IntegrationPrincipalRecord{}, err
	}
	if err := r.managed(ctx); err != nil {
		return IntegrationPrincipalRecord{}, err
	}
	q := `SELECT ` + integrationPrincipalColumns + ` FROM integration_principals WHERE id=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	return scanIntegrationPrincipal(r.db(ctx).QueryRow(ctx, q, id))
}
func (r *PgIntegrationRepository) ListIntegrationPrincipals(ctx context.Context, after string, n int) ([]IntegrationPrincipalRecord, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT `+integrationPrincipalColumns+` FROM integration_principals WHERE id>$1 ORDER BY id LIMIT $2`, after, integrationLimit(n))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IntegrationPrincipalRecord{}
	for rows.Next() {
		p, err := scanIntegrationPrincipal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *PgIntegrationRepository) UpdateIntegrationPrincipal(ctx context.Context, p *IntegrationPrincipalRecord, version int64) (bool, error) {
	if p == nil || !validIntegrationPrincipal(*p) {
		return false, ErrIntegrationConflict
	}
	if err := integrationLock(ctx, true); err != nil {
		return false, err
	}
	out, err := scanIntegrationPrincipal(r.db(ctx).QueryRow(ctx, `UPDATE integration_principals SET name=$2,status=$3 WHERE id=$1 AND facts_version=$4 AND status<>'revoked' RETURNING `+integrationPrincipalColumns, p.ID, p.Name, p.Status, version))
	if errors.Is(err, ErrIntegrationNotFound) {
		return false, nil
	}
	if err == nil {
		*p = out
	}
	return err == nil, err
}
func (r *PgIntegrationRepository) CreateIntegrationCredential(ctx context.Context, c *IntegrationCredentialRecord) error {
	if c == nil || !validIntegrationCredential(*c) {
		return ErrIntegrationConflict
	}
	if err := integrationLock(ctx, true); err != nil {
		return err
	}
	raw, err := json.Marshal(c.Rules)
	if err != nil {
		return err
	}
	out, err := scanIntegrationCredential(r.db(ctx).QueryRow(ctx, `INSERT INTO integration_credentials(id,principal_id,prefix,digest,purpose,status,rules,not_before_ms,expires_at_ms,created_by_admin_id) SELECT $1,p.id,$3,$4,$5,$6,$7,$8,$9,$10 FROM integration_principals p WHERE p.id=$2 AND p.status='active' RETURNING `+integrationCredentialColumns, c.ID, c.PrincipalID, c.Prefix, c.Digest[:], c.Purpose, c.Status, raw, c.NotBeforeMS, c.ExpiresAtMS, c.CreatedByAdminID))
	if err == nil {
		*c = out
	}
	return err
}
func (r *PgIntegrationRepository) GetIntegrationCredential(ctx context.Context, id string, lock bool) (IntegrationCredentialRecord, error) {
	if err := integrationLock(ctx, lock); err != nil {
		return IntegrationCredentialRecord{}, err
	}
	if err := r.managed(ctx); err != nil {
		return IntegrationCredentialRecord{}, err
	}
	q := `SELECT ` + integrationCredentialColumns + ` FROM integration_credentials WHERE id=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	return scanIntegrationCredential(r.db(ctx).QueryRow(ctx, q, id))
}
func (r *PgIntegrationRepository) ListIntegrationCredentials(ctx context.Context, pid, after string, n int) ([]IntegrationCredentialRecord, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT `+integrationCredentialColumns+` FROM integration_credentials WHERE principal_id=$1 AND id>$2 ORDER BY id LIMIT $3`, pid, after, integrationLimit(n))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IntegrationCredentialRecord{}
	for rows.Next() {
		c, err := scanIntegrationCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (r *PgIntegrationRepository) UpdateIntegrationCredential(ctx context.Context, c *IntegrationCredentialRecord, version int64) (bool, error) {
	if c == nil || !validIntegrationCredential(*c) {
		return false, ErrIntegrationConflict
	}
	if err := integrationLock(ctx, true); err != nil {
		return false, err
	}
	raw, err := json.Marshal(c.Rules)
	if err != nil {
		return false, err
	}
	out, err := scanIntegrationCredential(r.db(ctx).QueryRow(ctx, `UPDATE integration_credentials SET status=$2,rules=$3 WHERE id=$1 AND facts_version=$4 AND status='active' AND principal_id=$5 AND prefix=$6 AND digest=$7 AND purpose=$8 AND not_before_ms=$9 AND expires_at_ms=$10 AND created_by_admin_id=$11 AND created_at=$12 RETURNING `+integrationCredentialColumns, c.ID, c.Status, raw, version, c.PrincipalID, c.Prefix, c.Digest[:], c.Purpose, c.NotBeforeMS, c.ExpiresAtMS, c.CreatedByAdminID, c.CreatedAt))
	if errors.Is(err, ErrIntegrationNotFound) {
		return false, nil
	}
	if err == nil {
		*c = out
	}
	return err == nil, err
}
