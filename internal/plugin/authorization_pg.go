package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgAuthorizationStore struct{ pool *pgxpool.Pool }

func NewPgAuthorizationStore(pool *pgxpool.Pool) *PgAuthorizationStore {
	return &PgAuthorizationStore{pool: pool}
}

func (s *PgAuthorizationStore) SyncVersion(ctx context.Context, pluginID int64, manifest *Manifest, digest, fingerprint string, declarations []CapabilityDeclaration, actor *int64) (PluginVersion, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PluginVersion{}, err
	}
	defer tx.Rollback(ctx)
	var existingID int64
	var existingDigest string
	var existingFingerprint string
	err = tx.QueryRow(ctx, `SELECT id,package_digest,permission_fingerprint FROM plugin_versions WHERE plugin_id=$1 AND version=$2`, pluginID, manifest.Version).Scan(&existingID, &existingDigest, &existingFingerprint)
	isNew := errors.Is(err, pgx.ErrNoRows)
	if err == nil && (existingDigest != digest || existingFingerprint != fingerprint) {
		return PluginVersion{}, fmt.Errorf("plugin version %s is immutable: package digest or capability fingerprint changed", manifest.Version)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return PluginVersion{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE plugin_versions SET lifecycle_status='retired',retired_at=COALESCE(retired_at,NOW()) WHERE plugin_id=$1 AND lifecycle_status='active' AND ($2::bigint=0 OR id<>$2)`, pluginID, existingID); err != nil {
		return PluginVersion{}, err
	}
	manifestJSON, _ := json.Marshal(manifest)
	if existingID == 0 {
		existingID = idgen.New()
		_, err = tx.Exec(ctx, `INSERT INTO plugin_versions (id,plugin_id,version,package_digest,signature_state,channel,lifecycle_status,manifest_api_version,host_api_version,permission_fingerprint,manifest,metadata,created_by,created_at,activated_at) VALUES ($1,$2,$3,$4,'unsigned','stable','active',$5,$6,$7,$8::jsonb,'{}'::jsonb,$9,NOW(),NOW())`, existingID, pluginID, manifest.Version, digest, manifest.APIVersion, manifest.HostAPIVersion, fingerprint, string(manifestJSON), actor)
	} else {
		_, err = tx.Exec(ctx, `UPDATE plugin_versions SET lifecycle_status='active',retired_at=NULL,activated_at=COALESCE(activated_at,NOW()) WHERE id=$1`, existingID)
	}
	if err != nil {
		return PluginVersion{}, err
	}
	if isNew {
		for _, declaration := range declarations {
			scopeJSON, _ := json.Marshal(nonNilMap(declaration.ResourceScope))
			_, err = tx.Exec(ctx, `INSERT INTO plugin_capability_declarations (id,plugin_version_id,capability_code,purpose,risk_level,required,resource_scope,data_classification,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,NOW())`, idgen.New(), existingID, declaration.CapabilityCode, declaration.Purpose, declaration.RiskLevel, declaration.Required, string(scopeJSON), declaration.DataClassification)
			if err != nil {
				return PluginVersion{}, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return PluginVersion{}, err
	}
	return s.VersionByID(ctx, existingID)
}

func scanPluginVersion(row pgx.Row) (PluginVersion, error) {
	var value PluginVersion
	var manifest []byte
	err := row.Scan(&value.ID, &value.PluginID, &value.PluginName, &value.Version, &value.PackageDigest, &value.SignatureState, &value.Channel, &value.LifecycleStatus, &value.ManifestAPIVersion, &value.HostAPIVersion, &value.PermissionFingerprint, &manifest, &value.CreatedAt)
	if err != nil {
		return PluginVersion{}, normalizeAuthorizationRowError(err)
	}
	if len(manifest) > 0 {
		_ = json.Unmarshal(manifest, &value.Manifest)
	}
	return value, nil
}

const versionSelect = `SELECT pv.id,pv.plugin_id,p.name,pv.version,pv.package_digest,pv.signature_state,pv.channel,pv.lifecycle_status,pv.manifest_api_version,pv.host_api_version,pv.permission_fingerprint,pv.manifest,pv.created_at FROM plugin_versions pv JOIN plugins p ON p.id=pv.plugin_id WHERE `

func (s *PgAuthorizationStore) ActiveVersion(ctx context.Context, name string) (PluginVersion, error) {
	return scanPluginVersion(s.pool.QueryRow(ctx, versionSelect+`p.name=$1 AND p.deleted_at IS NULL AND pv.lifecycle_status='active'`, name))
}
func (s *PgAuthorizationStore) VersionByID(ctx context.Context, id int64) (PluginVersion, error) {
	return scanPluginVersion(s.pool.QueryRow(ctx, versionSelect+`pv.id=$1`, id))
}

func (s *PgAuthorizationStore) ListDeclarations(ctx context.Context, versionID int64) ([]CapabilityDeclaration, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,plugin_version_id,capability_code,purpose,risk_level,required,resource_scope,data_classification,created_at FROM plugin_capability_declarations WHERE plugin_version_id=$1 ORDER BY capability_code`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CapabilityDeclaration{}
	for rows.Next() {
		var value CapabilityDeclaration
		var scope []byte
		if err := rows.Scan(&value.ID, &value.PluginVersionID, &value.CapabilityCode, &value.Purpose, &value.RiskLevel, &value.Required, &scope, &value.DataClassification, &value.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(scope, &value.ResourceScope)
		result = append(result, value)
	}
	return result, rows.Err()
}

func scanAdminGrant(row pgx.Row) (AdminGrant, error) {
	var value AdminGrant
	var scope []byte
	err := row.Scan(&value.ID, &value.PluginVersionID, &value.CapabilityCode, &value.Status, &scope, &value.PolicyRevision, &value.DecidedBy, &value.Reason, &value.ExpiresAt, &value.SupersededAt, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return AdminGrant{}, normalizeAuthorizationRowError(err)
	}
	_ = json.Unmarshal(scope, &value.GrantedScope)
	return value, nil
}

const adminGrantSelect = `SELECT id,plugin_version_id,capability_code,status,granted_scope,policy_revision,decided_by,reason,expires_at,superseded_at,created_at,updated_at FROM plugin_admin_grants `

func (s *PgAuthorizationStore) CurrentAdminGrant(ctx context.Context, versionID int64, code string) (AdminGrant, error) {
	return scanAdminGrant(s.pool.QueryRow(ctx, adminGrantSelect+`WHERE plugin_version_id=$1 AND capability_code=$2 AND superseded_at IS NULL`, versionID, code))
}
func (s *PgAuthorizationStore) ListAdminGrants(ctx context.Context, versionID int64) ([]AdminGrant, error) {
	rows, err := s.pool.Query(ctx, adminGrantSelect+`WHERE plugin_version_id=$1 AND superseded_at IS NULL ORDER BY capability_code`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AdminGrant{}
	for rows.Next() {
		value, scanErr := scanAdminGrant(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
func (s *PgAuthorizationStore) SetAdminGrant(ctx context.Context, value AdminGrant) (AdminGrant, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AdminGrant{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("admin:%d:%s", value.PluginVersionID, value.CapabilityCode)); err != nil {
		return AdminGrant{}, err
	}
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(policy_revision),0)+1 FROM plugin_admin_grants WHERE plugin_version_id=$1 AND capability_code=$2`, value.PluginVersionID, value.CapabilityCode).Scan(&revision); err != nil {
		return AdminGrant{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE plugin_admin_grants SET superseded_at=NOW(),updated_at=NOW() WHERE plugin_version_id=$1 AND capability_code=$2 AND superseded_at IS NULL`, value.PluginVersionID, value.CapabilityCode); err != nil {
		return AdminGrant{}, err
	}
	scope, _ := json.Marshal(nonNilMap(value.GrantedScope))
	value.PolicyRevision = revision
	created, err := scanAdminGrant(tx.QueryRow(ctx, `INSERT INTO plugin_admin_grants (id,plugin_version_id,capability_code,status,granted_scope,policy_revision,decided_by,reason,expires_at,created_at,updated_at) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,NOW(),NOW()) RETURNING id,plugin_version_id,capability_code,status,granted_scope,policy_revision,decided_by,reason,expires_at,superseded_at,created_at,updated_at`, value.ID, value.PluginVersionID, value.CapabilityCode, value.Status, string(scope), value.PolicyRevision, value.DecidedBy, value.Reason, value.ExpiresAt))
	if err != nil {
		return AdminGrant{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AdminGrant{}, err
	}
	return created, nil
}

func scanConsent(row pgx.Row) (UserConsent, error) {
	var value UserConsent
	var scope, metadata []byte
	err := row.Scan(&value.ID, &value.UserID, &value.PluginVersionID, &value.CapabilityCode, &value.Status, &scope, &value.PurposeHash, &value.PolicyRevision, &value.DecidedAt, &value.ExpiresAt, &value.RevokedAt, &value.SupersededAt, &metadata)
	if err != nil {
		return UserConsent{}, normalizeAuthorizationRowError(err)
	}
	_ = json.Unmarshal(scope, &value.ConsentScope)
	_ = json.Unmarshal(metadata, &value.Metadata)
	return value, nil
}

const consentSelect = `SELECT id,user_id,plugin_version_id,capability_code,status,consent_scope,purpose_hash,policy_revision,decided_at,expires_at,revoked_at,superseded_at,metadata FROM plugin_user_consents `

func (s *PgAuthorizationStore) CurrentUserConsent(ctx context.Context, userID, versionID int64, code string) (UserConsent, error) {
	return scanConsent(s.pool.QueryRow(ctx, consentSelect+`WHERE user_id=$1 AND plugin_version_id=$2 AND capability_code=$3 AND superseded_at IS NULL`, userID, versionID, code))
}
func (s *PgAuthorizationStore) ListUserConsents(ctx context.Context, userID, versionID int64) ([]UserConsent, error) {
	rows, err := s.pool.Query(ctx, consentSelect+`WHERE user_id=$1 AND plugin_version_id=$2 AND superseded_at IS NULL ORDER BY capability_code`, userID, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []UserConsent{}
	for rows.Next() {
		value, scanErr := scanConsent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
func (s *PgAuthorizationStore) SetUserConsent(ctx context.Context, value UserConsent) (UserConsent, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return UserConsent{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("consent:%d:%d:%s", value.UserID, value.PluginVersionID, value.CapabilityCode)); err != nil {
		return UserConsent{}, err
	}
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(policy_revision),0)+1 FROM plugin_user_consents WHERE user_id=$1 AND plugin_version_id=$2 AND capability_code=$3`, value.UserID, value.PluginVersionID, value.CapabilityCode).Scan(&revision); err != nil {
		return UserConsent{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE plugin_user_consents SET superseded_at=NOW() WHERE user_id=$1 AND plugin_version_id=$2 AND capability_code=$3 AND superseded_at IS NULL`, value.UserID, value.PluginVersionID, value.CapabilityCode); err != nil {
		return UserConsent{}, err
	}
	scope, _ := json.Marshal(nonNilMap(value.ConsentScope))
	metadata, _ := json.Marshal(nonNilMap(value.Metadata))
	value.PolicyRevision = revision
	created, err := scanConsent(tx.QueryRow(ctx, `INSERT INTO plugin_user_consents (id,user_id,plugin_version_id,capability_code,status,consent_scope,purpose_hash,policy_revision,decided_at,expires_at,revoked_at,metadata) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,NOW(),$9,$10,$11::jsonb) RETURNING id,user_id,plugin_version_id,capability_code,status,consent_scope,purpose_hash,policy_revision,decided_at,expires_at,revoked_at,superseded_at,metadata`, value.ID, value.UserID, value.PluginVersionID, value.CapabilityCode, value.Status, string(scope), value.PurposeHash, value.PolicyRevision, value.ExpiresAt, value.RevokedAt, string(metadata)))
	if err != nil {
		return UserConsent{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return UserConsent{}, err
	}
	return created, nil
}

func scanDelegation(row pgx.Row) (Delegation, error) {
	var value Delegation
	var capabilities, scope []byte
	err := row.Scan(&value.ID, &value.PluginVersionID, &value.SubjectUserID, &value.TokenDigest, &capabilities, &scope, &value.Status, &value.NotBefore, &value.ExpiresAt, &value.RevokedAt, &value.CreatedBy, &value.CreatedAt)
	if err != nil {
		return Delegation{}, normalizeAuthorizationRowError(err)
	}
	_ = json.Unmarshal(capabilities, &value.GrantedCapabilities)
	_ = json.Unmarshal(scope, &value.ResourceScope)
	return value, nil
}
func (s *PgAuthorizationStore) CreateDelegation(ctx context.Context, value Delegation) (Delegation, error) {
	caps, _ := json.Marshal(value.GrantedCapabilities)
	scope, _ := json.Marshal(nonNilMap(value.ResourceScope))
	return scanDelegation(s.pool.QueryRow(ctx, `INSERT INTO plugin_delegations (id,plugin_version_id,subject_user_id,token_digest,granted_capabilities,resource_scope,status,not_before,expires_at,created_by,created_at) VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9,$10,$11) RETURNING id,plugin_version_id,subject_user_id,token_digest,granted_capabilities,resource_scope,status,not_before,expires_at,revoked_at,created_by,created_at`, value.ID, value.PluginVersionID, value.SubjectUserID, value.TokenDigest, string(caps), string(scope), value.Status, value.NotBefore, value.ExpiresAt, value.CreatedBy, value.CreatedAt))
}
func (s *PgAuthorizationStore) DelegationByDigest(ctx context.Context, digest string) (Delegation, error) {
	return scanDelegation(s.pool.QueryRow(ctx, `SELECT id,plugin_version_id,subject_user_id,token_digest,granted_capabilities,resource_scope,status,not_before,expires_at,revoked_at,created_by,created_at FROM plugin_delegations WHERE token_digest=$1`, digest))
}
func (s *PgAuthorizationStore) RevokeDelegation(ctx context.Context, id, userID int64) error {
	result, err := s.pool.Exec(ctx, `UPDATE plugin_delegations SET status='revoked',revoked_at=NOW() WHERE id=$1 AND subject_user_id=$2 AND status='active'`, id, userID)
	if err == nil && result.RowsAffected() == 0 {
		return ErrMarketNotFound
	}
	return err
}

func (s *PgAuthorizationStore) SaveAuthorizationDecision(ctx context.Context, value AuthorizationDecision) error {
	scope, _ := json.Marshal(nonNilMap(value.ResourceScope))
	detail, _ := json.Marshal(nonNilMap(value.Context))
	_, err := s.pool.Exec(ctx, `INSERT INTO plugin_authorization_decisions (id,request_id,plugin_version_id,user_id,capability_code,operation_code,resource_scope,admin_grant_id,user_consent_id,delegation_id,outcome,reason_code,policy_revision,trace_id,context,created_at) VALUES ($1,$2::uuid,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,$16)`, value.ID, value.RequestID, value.PluginVersionID, value.UserID, value.CapabilityCode, value.OperationCode, string(scope), value.AdminGrantID, value.UserConsentID, value.DelegationID, value.Outcome, value.ReasonCode, value.PolicyRevision, value.TraceID, string(detail), value.CreatedAt)
	return err
}
func scanDecision(row pgx.Row) (AuthorizationDecision, error) {
	var value AuthorizationDecision
	var scope, detail []byte
	err := row.Scan(&value.ID, &value.RequestID, &value.PluginVersionID, &value.UserID, &value.CapabilityCode, &value.OperationCode, &scope, &value.AdminGrantID, &value.UserConsentID, &value.DelegationID, &value.Outcome, &value.ReasonCode, &value.PolicyRevision, &value.TraceID, &detail, &value.CreatedAt)
	if err != nil {
		return AuthorizationDecision{}, normalizeAuthorizationRowError(err)
	}
	_ = json.Unmarshal(scope, &value.ResourceScope)
	_ = json.Unmarshal(detail, &value.Context)
	return value, nil
}
func (s *PgAuthorizationStore) ListAuthorizationDecisions(ctx context.Context, versionID int64, limit int) ([]AuthorizationDecision, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT id,request_id::text,plugin_version_id,user_id,capability_code,operation_code,resource_scope,admin_grant_id,user_consent_id,delegation_id,outcome,reason_code,policy_revision,trace_id,context,created_at FROM plugin_authorization_decisions WHERE plugin_version_id=$1 ORDER BY created_at DESC LIMIT $2`, versionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AuthorizationDecision{}
	for rows.Next() {
		value, scanErr := scanDecision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func normalizeAuthorizationRowError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMarketNotFound
	}
	return err
}

var _ AuthorizationStore = (*PgAuthorizationStore)(nil)
var _ = time.Time{}
