package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgMarketStore persists v9 plugin-market state. It remains inside the plugin
// platform boundary; external plugins only use the MarketService/Host API.
type PgMarketStore struct{ pool *pgxpool.Pool }

func NewPgMarketStore(pool *pgxpool.Pool) *PgMarketStore { return &PgMarketStore{pool: pool} }

func (r *PgMarketStore) CreateRecord(ctx context.Context, record ManagedRecord) (ManagedRecord, error) {
	if record.ID == 0 {
		record.ID = idgen.New()
	}
	data, err := json.Marshal(record.Data)
	if err != nil {
		return ManagedRecord{}, err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO plugin_records
        (id, plugin_name, owner_type, owner_id, collection, record_key, data, search_text, version, created_at, updated_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11)`,
		record.ID, record.PluginName, record.OwnerType, record.OwnerID, record.Collection, record.RecordKey,
		string(data), record.SearchText, record.Version, record.CreatedAt, record.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "uk_plugin_records_active_key") {
			return ManagedRecord{}, ErrMarketConflict
		}
		return ManagedRecord{}, err
	}
	return record, nil
}

func (r *PgMarketStore) GetRecord(ctx context.Context, pluginName, ownerType, ownerID, collection, key string) (ManagedRecord, error) {
	return scanRecord(r.pool.QueryRow(ctx, `SELECT id, plugin_name, owner_type, owner_id, collection, record_key, data, search_text, version, created_at, updated_at
        FROM plugin_records WHERE plugin_name=$1 AND owner_type=$2 AND owner_id=$3 AND collection=$4 AND record_key=$5 AND deleted_at IS NULL`, pluginName, ownerType, ownerID, collection, key))
}

func (r *PgMarketStore) UpdateRecord(ctx context.Context, record ManagedRecord) (ManagedRecord, error) {
	data, err := json.Marshal(record.Data)
	if err != nil {
		return ManagedRecord{}, err
	}
	updated, err := scanRecord(r.pool.QueryRow(ctx, `UPDATE plugin_records SET data=$1::jsonb, search_text=$2, version=version+1, updated_at=$3
        WHERE plugin_name=$4 AND owner_type=$5 AND owner_id=$6 AND collection=$7 AND record_key=$8 AND version=$9 AND deleted_at IS NULL
        RETURNING id, plugin_name, owner_type, owner_id, collection, record_key, data, search_text, version, created_at, updated_at`,
		string(data), record.SearchText, record.UpdatedAt, record.PluginName, record.OwnerType, record.OwnerID, record.Collection, record.RecordKey, record.Version))
	if errors.Is(err, ErrMarketNotFound) {
		return ManagedRecord{}, ErrMarketVersionMismatch
	}
	return updated, err
}

func (r *PgMarketStore) DeleteRecord(ctx context.Context, pluginName, ownerType, ownerID, collection, key string, version int64) error {
	result, err := r.pool.Exec(ctx, `UPDATE plugin_records SET deleted_at=NOW() WHERE plugin_name=$1 AND owner_type=$2 AND owner_id=$3 AND collection=$4 AND record_key=$5 AND version=$6 AND deleted_at IS NULL`, pluginName, ownerType, ownerID, collection, key, version)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrMarketVersionMismatch
	}
	return nil
}

func (r *PgMarketStore) ListRecords(ctx context.Context, query RecordQuery) (RecordPage, error) {
	where, args := recordWhere(query)
	countQuery := "SELECT count(*) FROM plugin_records " + where
	var total int
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return RecordPage{}, err
	}
	page, size := query.Page, query.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	args = append(args, size, (page-1)*size)
	rows, err := r.pool.Query(ctx, `SELECT id, plugin_name, owner_type, owner_id, collection, record_key, data, search_text, version, created_at, updated_at FROM plugin_records `+where+fmt.Sprintf(" ORDER BY updated_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return RecordPage{}, err
	}
	defer rows.Close()
	items := []ManagedRecord{}
	for rows.Next() {
		item, scanErr := scanRecord(rows)
		if scanErr != nil {
			return RecordPage{}, scanErr
		}
		items = append(items, item)
	}
	return RecordPage{Items: items, Total: total, Page: page, Size: size}, rows.Err()
}

func (r *PgMarketStore) RecordUsage(ctx context.Context, pluginName, ownerType, ownerID string) (int64, error) {
	var bytes int64
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(sum(octet_length(data::text)),0) FROM plugin_records WHERE plugin_name=$1 AND owner_type=$2 AND owner_id=$3 AND deleted_at IS NULL`, pluginName, ownerType, ownerID).Scan(&bytes)
	return bytes, err
}

func (r *PgMarketStore) DeleteOwnerRecords(ctx context.Context, pluginName, ownerID string) (int, error) {
	result, err := r.pool.Exec(ctx, `UPDATE plugin_records SET deleted_at=NOW() WHERE plugin_name=$1 AND owner_type='user' AND owner_id=$2 AND deleted_at IS NULL`, pluginName, ownerID)
	if err != nil {
		return 0, err
	}
	return int(result.RowsAffected()), nil
}

func recordWhere(query RecordQuery) (string, []interface{}) {
	clauses := []string{"WHERE deleted_at IS NULL"}
	args := []interface{}{}
	add := func(clause string, values ...interface{}) {
		for _, value := range values {
			args = append(args, value)
		}
		clauses = append(clauses, fmt.Sprintf(clause, len(args)-len(values)+1, len(args)-len(values)+2))
	}
	if query.PluginName != "" {
		add("plugin_name=$%d", query.PluginName)
	}
	if query.OwnerType != "" {
		add("owner_type=$%d", query.OwnerType)
	}
	if query.OwnerID != "" {
		add("owner_id=$%d", query.OwnerID)
	}
	if query.Collection != "" {
		add("collection=$%d", query.Collection)
	}
	if query.Keyword != "" {
		add("search_text ILIKE '%%' || $%d || '%%'", query.Keyword)
	}
	for field, value := range query.Filters {
		args = append(args, field, value)
		clauses = append(clauses, fmt.Sprintf("data ->> $%d = $%d", len(args)-1, len(args)))
	}
	return strings.Join(clauses, " AND "), args
}

func (r *PgMarketStore) UpsertGrant(ctx context.Context, grant UserGrant) (UserGrant, error) {
	if grant.ID == 0 {
		grant.ID = idgen.New()
	}
	permissions, err := json.Marshal(uniqueStrings(grant.Permissions))
	if err != nil {
		return UserGrant{}, err
	}
	return scanGrant(r.pool.QueryRow(ctx, `INSERT INTO plugin_user_grants (id,plugin_name,user_id,version,permissions,status,granted_at,revoked_at,updated_at)
        VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)
        ON CONFLICT (plugin_name,user_id) DO UPDATE SET version=EXCLUDED.version,permissions=EXCLUDED.permissions,status=EXCLUDED.status,granted_at=EXCLUDED.granted_at,revoked_at=EXCLUDED.revoked_at,updated_at=EXCLUDED.updated_at
        RETURNING id,plugin_name,user_id,version,permissions,status,granted_at,revoked_at,updated_at`, grant.ID, grant.PluginName, grant.UserID, grant.Version, string(permissions), grant.Status, grant.GrantedAt, grant.RevokedAt, grant.UpdatedAt))
}

func (r *PgMarketStore) GetGrant(ctx context.Context, pluginName, userID string) (UserGrant, error) {
	return scanGrant(r.pool.QueryRow(ctx, `SELECT id,plugin_name,user_id,version,permissions,status,granted_at,revoked_at,updated_at FROM plugin_user_grants WHERE plugin_name=$1 AND user_id=$2`, pluginName, userID))
}

func (r *PgMarketStore) ListGrants(ctx context.Context, userID string) ([]UserGrant, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,plugin_name,user_id,version,permissions,status,granted_at,revoked_at,updated_at FROM plugin_user_grants WHERE ($1='' OR user_id=$1) ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []UserGrant{}
	for rows.Next() {
		item, scanErr := scanGrant(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgMarketStore) SaveFile(ctx context.Context, file PluginFile) (PluginFile, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO plugin_file_metadata (id,plugin_name,owner_id,original_name,stored_name,content_type,size_bytes,storage_key,retention,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, file.ID, file.PluginName, file.OwnerID, file.OriginalName, file.StoredName, file.ContentType, file.Size, file.StorageKey, file.Retention, file.CreatedAt)
	return file, err
}

func (r *PgMarketStore) GetFile(ctx context.Context, pluginName, ownerID, fileID string) (PluginFile, error) {
	id, err := strconv.ParseInt(fileID, 10, 64)
	if err != nil {
		return PluginFile{}, ErrMarketNotFound
	}
	return scanFile(r.pool.QueryRow(ctx, `SELECT id,plugin_name,owner_id,original_name,stored_name,content_type,size_bytes,storage_key,retention,created_at,deleted_at FROM plugin_file_metadata WHERE id=$1 AND plugin_name=$2 AND owner_id=$3 AND deleted_at IS NULL`, id, pluginName, ownerID))
}

func (r *PgMarketStore) ListFiles(ctx context.Context, pluginName, ownerID string) ([]PluginFile, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,plugin_name,owner_id,original_name,stored_name,content_type,size_bytes,storage_key,retention,created_at,deleted_at FROM plugin_file_metadata WHERE plugin_name=$1 AND owner_id=$2 AND deleted_at IS NULL ORDER BY created_at DESC`, pluginName, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PluginFile{}
	for rows.Next() {
		item, scanErr := scanFile(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgMarketStore) DeleteFile(ctx context.Context, pluginName, ownerID, fileID string) (PluginFile, error) {
	id, err := strconv.ParseInt(fileID, 10, 64)
	if err != nil {
		return PluginFile{}, ErrMarketNotFound
	}
	return scanFile(r.pool.QueryRow(ctx, `UPDATE plugin_file_metadata SET deleted_at=NOW() WHERE id=$1 AND plugin_name=$2 AND owner_id=$3 AND deleted_at IS NULL RETURNING id,plugin_name,owner_id,original_name,stored_name,content_type,size_bytes,storage_key,retention,created_at,deleted_at`, id, pluginName, ownerID))
}

func (r *PgMarketStore) FileUsage(ctx context.Context, pluginName, ownerID string) (int64, error) {
	var result int64
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(sum(size_bytes),0) FROM plugin_file_metadata WHERE plugin_name=$1 AND owner_id=$2 AND deleted_at IS NULL`, pluginName, ownerID).Scan(&result)
	return result, err
}

func (r *PgMarketStore) UpsertCatalog(ctx context.Context, entry CatalogEntry) (CatalogEntry, error) {
	capabilities, err := json.Marshal(uniqueStrings(entry.DataCapabilities))
	if err != nil {
		return CatalogEntry{}, err
	}
	permissions, err := json.Marshal(entry.UserPermissions)
	if err != nil {
		return CatalogEntry{}, err
	}
	return scanCatalog(r.pool.QueryRow(ctx, `INSERT INTO plugin_catalog_entries (plugin_name,display_name,description,version,runtime,visibility,package_checksum,risk_level,data_capabilities,user_permissions,updated_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11)
        ON CONFLICT (plugin_name) DO UPDATE SET display_name=EXCLUDED.display_name,description=EXCLUDED.description,version=EXCLUDED.version,runtime=EXCLUDED.runtime,visibility=CASE WHEN EXCLUDED.visibility='draft' THEN plugin_catalog_entries.visibility ELSE EXCLUDED.visibility END,package_checksum=EXCLUDED.package_checksum,risk_level=EXCLUDED.risk_level,data_capabilities=EXCLUDED.data_capabilities,user_permissions=EXCLUDED.user_permissions,updated_at=EXCLUDED.updated_at
        RETURNING plugin_name,display_name,description,version,runtime,visibility,package_checksum,risk_level,data_capabilities,user_permissions,updated_at`, entry.PluginName, entry.DisplayName, entry.Description, entry.Version, entry.Runtime, entry.Visibility, entry.PackageChecksum, entry.RiskLevel, string(capabilities), string(permissions), entry.UpdatedAt))
}

func (r *PgMarketStore) ListCatalog(ctx context.Context, visibility string) ([]CatalogEntry, error) {
	rows, err := r.pool.Query(ctx, `SELECT plugin_name,display_name,description,version,runtime,visibility,package_checksum,risk_level,data_capabilities,user_permissions,updated_at FROM plugin_catalog_entries WHERE ($1='' OR visibility=$1) ORDER BY plugin_name`, visibility)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CatalogEntry{}
	for rows.Next() {
		item, scanErr := scanCatalog(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgMarketStore) CreateInstallRequest(ctx context.Context, request InstallRequest) (InstallRequest, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO plugin_install_requests (id,plugin_name,user_id,message,status,created_at) VALUES ($1,$2,$3,$4,$5,$6)`, request.ID, request.PluginName, request.UserID, request.Message, request.Status, request.CreatedAt)
	if err != nil && strings.Contains(err.Error(), "uk_plugin_install_request_pending") {
		return InstallRequest{}, ErrMarketConflict
	}
	return request, err
}

func (r *PgMarketStore) ListInstallRequests(ctx context.Context, status string) ([]InstallRequest, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,plugin_name,user_id,message,status,reviewed_by,created_at,reviewed_at FROM plugin_install_requests WHERE ($1='' OR status=$1) ORDER BY created_at DESC`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []InstallRequest{}
	for rows.Next() {
		item, scanErr := scanInstallRequest(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgMarketStore) ReviewInstallRequest(ctx context.Context, id int64, reviewer, status string) (InstallRequest, error) {
	return scanInstallRequest(r.pool.QueryRow(ctx, `UPDATE plugin_install_requests SET status=$1,reviewed_by=$2,reviewed_at=NOW() WHERE id=$3 AND status='pending' RETURNING id,plugin_name,user_id,message,status,reviewed_by,created_at,reviewed_at`, status, reviewer, id))
}

func (r *PgMarketStore) SaveRelease(ctx context.Context, release PluginRelease) (PluginRelease, error) {
	if release.ID == 0 {
		release.ID = idgen.New()
	}
	return scanRelease(r.pool.QueryRow(ctx, `INSERT INTO plugin_releases (id,plugin_name,version,checksum,signature_state,channel,rollout_state,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (plugin_name,version,checksum) DO UPDATE SET signature_state=EXCLUDED.signature_state,channel=EXCLUDED.channel,rollout_state=EXCLUDED.rollout_state
		RETURNING id,plugin_name,version,checksum,signature_state,channel,rollout_state,created_at`, release.ID, release.PluginName, release.Version, release.Checksum, release.SignatureState, release.Channel, release.RolloutState, release.CreatedAt))
}

func (r *PgMarketStore) ListReleases(ctx context.Context, pluginName string) ([]PluginRelease, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,plugin_name,version,checksum,signature_state,channel,rollout_state,created_at FROM plugin_releases WHERE plugin_name=$1 ORDER BY created_at DESC`, pluginName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PluginRelease{}
	for rows.Next() {
		item, scanErr := scanRelease(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgMarketStore) AppendAudit(ctx context.Context, audit MarketAudit) error {
	if audit.ID == 0 {
		audit.ID = idgen.New()
	}
	metadata, err := json.Marshal(audit.Metadata)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO plugin_market_audits (id,plugin_name,actor_id,action,outcome,metadata,created_at) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)`, audit.ID, audit.PluginName, audit.ActorID, audit.Action, audit.Outcome, string(metadata), audit.CreatedAt)
	return err
}

func (r *PgMarketStore) ListAudits(ctx context.Context, pluginName string, limit int) ([]MarketAudit, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,plugin_name,actor_id,action,outcome,metadata,created_at FROM plugin_market_audits WHERE ($1='' OR plugin_name=$1) ORDER BY created_at DESC LIMIT $2`, pluginName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MarketAudit{}
	for rows.Next() {
		item, scanErr := scanMarketAudit(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgMarketStore) Metrics(ctx context.Context, pluginName string) (MarketMetrics, error) {
	metrics := MarketMetrics{PluginName: pluginName}
	var recordUpdated *time.Time
	err := r.pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(octet_length(data::text)),0), count(DISTINCT owner_id) FILTER (WHERE owner_type='user'), max(updated_at) FROM plugin_records WHERE plugin_name=$1 AND deleted_at IS NULL`, pluginName).Scan(&metrics.RecordCount, &metrics.RecordBytes, &metrics.UserCount, &recordUpdated)
	if err != nil {
		return MarketMetrics{}, err
	}
	var fileUsers int
	var fileUpdated *time.Time
	err = r.pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(size_bytes),0), count(DISTINCT owner_id), max(created_at) FROM plugin_file_metadata WHERE plugin_name=$1 AND deleted_at IS NULL`, pluginName).Scan(&metrics.FileCount, &metrics.FileBytes, &fileUsers, &fileUpdated)
	if err != nil {
		return MarketMetrics{}, err
	}
	if fileUsers > metrics.UserCount {
		metrics.UserCount = fileUsers
	}
	metrics.LastUpdated = newestTime(recordUpdated, fileUpdated)
	return metrics, nil
}

func (r *PgMarketStore) UserMetrics(ctx context.Context, pluginName, ownerID string) (MarketMetrics, error) {
	metrics := MarketMetrics{PluginName: pluginName}
	var recordUpdated *time.Time
	if err := r.pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(octet_length(data::text)),0), max(updated_at) FROM plugin_records WHERE plugin_name=$1 AND owner_type='user' AND owner_id=$2 AND deleted_at IS NULL`, pluginName, ownerID).Scan(&metrics.RecordCount, &metrics.RecordBytes, &recordUpdated); err != nil {
		return MarketMetrics{}, err
	}
	var fileUpdated *time.Time
	if err := r.pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(size_bytes),0), max(created_at) FROM plugin_file_metadata WHERE plugin_name=$1 AND owner_id=$2 AND deleted_at IS NULL`, pluginName, ownerID).Scan(&metrics.FileCount, &metrics.FileBytes, &fileUpdated); err != nil {
		return MarketMetrics{}, err
	}
	metrics.LastUpdated = newestTime(recordUpdated, fileUpdated)
	if metrics.RecordCount > 0 || metrics.FileCount > 0 {
		metrics.UserCount = 1
	}
	return metrics, nil
}

type rowScanner interface{ Scan(...interface{}) error }

func scanRecord(row rowScanner) (ManagedRecord, error) {
	var record ManagedRecord
	var data []byte
	err := row.Scan(&record.ID, &record.PluginName, &record.OwnerType, &record.OwnerID, &record.Collection, &record.RecordKey, &data, &record.SearchText, &record.Version, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		return ManagedRecord{}, normalizeMarketRowError(err)
	}
	if err := json.Unmarshal(data, &record.Data); err != nil {
		return ManagedRecord{}, err
	}
	return record, nil
}
func scanGrant(row rowScanner) (UserGrant, error) {
	var grant UserGrant
	var permissions []byte
	err := row.Scan(&grant.ID, &grant.PluginName, &grant.UserID, &grant.Version, &permissions, &grant.Status, &grant.GrantedAt, &grant.RevokedAt, &grant.UpdatedAt)
	if err != nil {
		return UserGrant{}, normalizeMarketRowError(err)
	}
	if err := json.Unmarshal(permissions, &grant.Permissions); err != nil {
		return UserGrant{}, err
	}
	return grant, nil
}
func scanFile(row rowScanner) (PluginFile, error) {
	var file PluginFile
	err := row.Scan(&file.ID, &file.PluginName, &file.OwnerID, &file.OriginalName, &file.StoredName, &file.ContentType, &file.Size, &file.StorageKey, &file.Retention, &file.CreatedAt, &file.DeletedAt)
	if err != nil {
		return PluginFile{}, normalizeMarketRowError(err)
	}
	return file, nil
}
func scanCatalog(row rowScanner) (CatalogEntry, error) {
	var entry CatalogEntry
	var capabilities []byte
	var permissions []byte
	err := row.Scan(&entry.PluginName, &entry.DisplayName, &entry.Description, &entry.Version, &entry.Runtime, &entry.Visibility, &entry.PackageChecksum, &entry.RiskLevel, &capabilities, &permissions, &entry.UpdatedAt)
	if err != nil {
		return CatalogEntry{}, normalizeMarketRowError(err)
	}
	if err := json.Unmarshal(capabilities, &entry.DataCapabilities); err != nil {
		return CatalogEntry{}, err
	}
	if err := json.Unmarshal(permissions, &entry.UserPermissions); err != nil {
		return CatalogEntry{}, err
	}
	return entry, nil
}
func scanInstallRequest(row rowScanner) (InstallRequest, error) {
	var request InstallRequest
	err := row.Scan(&request.ID, &request.PluginName, &request.UserID, &request.Message, &request.Status, &request.ReviewedBy, &request.CreatedAt, &request.ReviewedAt)
	if err != nil {
		return InstallRequest{}, normalizeMarketRowError(err)
	}
	return request, nil
}
func scanRelease(row rowScanner) (PluginRelease, error) {
	var release PluginRelease
	err := row.Scan(&release.ID, &release.PluginName, &release.Version, &release.Checksum, &release.SignatureState, &release.Channel, &release.RolloutState, &release.CreatedAt)
	if err != nil {
		return PluginRelease{}, normalizeMarketRowError(err)
	}
	return release, nil
}
func scanMarketAudit(row rowScanner) (MarketAudit, error) {
	var audit MarketAudit
	var metadata []byte
	if err := row.Scan(&audit.ID, &audit.PluginName, &audit.ActorID, &audit.Action, &audit.Outcome, &metadata, &audit.CreatedAt); err != nil {
		return MarketAudit{}, normalizeMarketRowError(err)
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &audit.Metadata); err != nil {
			return MarketAudit{}, err
		}
	}
	return audit, nil
}

func newestTime(values ...*time.Time) *time.Time {
	var newest *time.Time
	for _, value := range values {
		if value != nil && (newest == nil || value.After(*newest)) {
			copy := *value
			newest = &copy
		}
	}
	return newest
}
func normalizeMarketRowError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMarketNotFound
	}
	return err
}

var _ MarketStore = (*PgMarketStore)(nil)

// Keep time imported in this file for interface stability when pgx generated
// scan code changes; release and record models intentionally expose timestamps.
var _ = time.Time{}
