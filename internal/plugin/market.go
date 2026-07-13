package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/core/storage"
	"github.com/campusos/CampusOS/pkg/idgen"
)

const (
	ManagedDataResource  = "managed_data"
	PluginFileResource   = "plugin_file"
	PluginSearchResource = "plugin_search"

	GrantEnabled = "enabled"
	GrantRevoked = "revoked"

	CatalogDraft     = "draft"
	CatalogPublished = "published"
	CatalogHidden    = "hidden"

	RequestPending  = "pending"
	RequestApproved = "approved"
	RequestRejected = "rejected"
)

var (
	ErrMarketNotFound        = errors.New("plugin market resource not found")
	ErrMarketDenied          = errors.New("plugin market access denied")
	ErrMarketConflict        = errors.New("plugin market version conflict")
	ErrMarketUnsupported     = errors.New("plugin does not declare this v2 capability")
	ErrMarketInvalidInput    = errors.New("invalid plugin market input")
	ErrMarketQuotaExceeded   = errors.New("plugin market quota exceeded")
	ErrMarketVersionMismatch = errors.New("plugin market record version mismatch")
)

// ManagedRecord is a host-owned structured record. Plugins never receive a
// database table name or connection; plugin, owner and collection form its
// immutable namespace.
type ManagedRecord struct {
	ID         int64                  `json:"id"`
	PluginName string                 `json:"plugin_name"`
	OwnerType  string                 `json:"owner_type"`
	OwnerID    string                 `json:"owner_id"`
	Collection string                 `json:"collection"`
	RecordKey  string                 `json:"record_key"`
	Data       map[string]interface{} `json:"data"`
	SearchText string                 `json:"-"`
	Version    int64                  `json:"version"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

type RecordInput struct {
	RecordKey string                 `json:"record_key,omitempty"`
	Data      map[string]interface{} `json:"data"`
	Version   int64                  `json:"version,omitempty"`
}

type RecordQuery struct {
	PluginName string            `json:"plugin_name"`
	OwnerType  string            `json:"owner_type"`
	OwnerID    string            `json:"owner_id"`
	Collection string            `json:"collection"`
	Filters    map[string]string `json:"filters,omitempty"`
	Keyword    string            `json:"keyword,omitempty"`
	Page       int               `json:"page,omitempty"`
	PageSize   int               `json:"page_size,omitempty"`
}

type RecordPage struct {
	Items []ManagedRecord `json:"items"`
	Total int             `json:"total"`
	Page  int             `json:"page"`
	Size  int             `json:"page_size"`
}

type PluginFile struct {
	ID           int64      `json:"id"`
	PluginName   string     `json:"plugin_name"`
	OwnerID      string     `json:"owner_id"`
	OriginalName string     `json:"original_name"`
	StoredName   string     `json:"stored_name"`
	ContentType  string     `json:"content_type"`
	Size         int64      `json:"size"`
	StorageKey   string     `json:"storage_key"`
	Retention    string     `json:"retention"`
	CreatedAt    time.Time  `json:"created_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

type UserGrant struct {
	ID          int64      `json:"id"`
	PluginName  string     `json:"plugin_name"`
	UserID      string     `json:"user_id"`
	Version     string     `json:"version"`
	Permissions []string   `json:"permissions"`
	Status      string     `json:"status"`
	GrantedAt   time.Time  `json:"granted_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CatalogEntry struct {
	PluginName       string           `json:"plugin_name"`
	DisplayName      string           `json:"display_name"`
	Description      string           `json:"description"`
	Version          string           `json:"version"`
	Runtime          string           `json:"runtime"`
	Visibility       string           `json:"visibility"`
	PackageChecksum  string           `json:"package_checksum,omitempty"`
	RiskLevel        string           `json:"risk_level,omitempty"`
	DataCapabilities []string         `json:"data_capabilities,omitempty"`
	UserPermissions  []UserPermission `json:"user_permissions,omitempty"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

type InstallRequest struct {
	ID         int64      `json:"id"`
	PluginName string     `json:"plugin_name"`
	UserID     string     `json:"user_id"`
	Message    string     `json:"message,omitempty"`
	Status     string     `json:"status"`
	ReviewedBy string     `json:"reviewed_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
}

type PluginRelease struct {
	ID             int64     `json:"id"`
	PluginName     string    `json:"plugin_name"`
	Version        string    `json:"version"`
	Checksum       string    `json:"checksum"`
	SignatureState string    `json:"signature_state"`
	Channel        string    `json:"channel"`
	RolloutState   string    `json:"rollout_state"`
	CreatedAt      time.Time `json:"created_at"`
}

type MarketAudit struct {
	ID         int64                  `json:"id"`
	PluginName string                 `json:"plugin_name"`
	ActorID    string                 `json:"actor_id"`
	Action     string                 `json:"action"`
	Outcome    string                 `json:"outcome"`
	Metadata   map[string]interface{} `json:"metadata"`
	CreatedAt  time.Time              `json:"created_at"`
}

type MarketMetrics struct {
	PluginName    string     `json:"plugin_name"`
	RecordCount   int        `json:"record_count"`
	RecordBytes   int64      `json:"record_bytes"`
	FileCount     int        `json:"file_count"`
	FileBytes     int64      `json:"file_bytes"`
	UserCount     int        `json:"user_count"`
	SearchEnabled bool       `json:"search_enabled"`
	LastUpdated   *time.Time `json:"last_updated,omitempty"`
}

// MarketStore is the persistence port for v9 plugin data. Its PostgreSQL
// adapter is owned by the plugin platform; the memory adapter is used by the
// existing no-PostgreSQL development profile and unit tests.
type MarketStore interface {
	CreateRecord(ctx context.Context, record ManagedRecord) (ManagedRecord, error)
	GetRecord(ctx context.Context, pluginName, ownerType, ownerID, collection, key string) (ManagedRecord, error)
	UpdateRecord(ctx context.Context, record ManagedRecord) (ManagedRecord, error)
	DeleteRecord(ctx context.Context, pluginName, ownerType, ownerID, collection, key string, version int64) error
	ListRecords(ctx context.Context, query RecordQuery) (RecordPage, error)
	RecordUsage(ctx context.Context, pluginName, ownerType, ownerID string) (int64, error)
	DeleteOwnerRecords(ctx context.Context, pluginName, ownerID string) (int, error)
	UpsertGrant(ctx context.Context, grant UserGrant) (UserGrant, error)
	GetGrant(ctx context.Context, pluginName, userID string) (UserGrant, error)
	ListGrants(ctx context.Context, userID string) ([]UserGrant, error)
	SaveFile(ctx context.Context, file PluginFile) (PluginFile, error)
	GetFile(ctx context.Context, pluginName, ownerID, fileID string) (PluginFile, error)
	ListFiles(ctx context.Context, pluginName, ownerID string) ([]PluginFile, error)
	DeleteFile(ctx context.Context, pluginName, ownerID, fileID string) (PluginFile, error)
	FileUsage(ctx context.Context, pluginName, ownerID string) (int64, error)
	UpsertCatalog(ctx context.Context, entry CatalogEntry) (CatalogEntry, error)
	ListCatalog(ctx context.Context, visibility string) ([]CatalogEntry, error)
	CreateInstallRequest(ctx context.Context, request InstallRequest) (InstallRequest, error)
	ListInstallRequests(ctx context.Context, status string) ([]InstallRequest, error)
	ReviewInstallRequest(ctx context.Context, id int64, reviewer, status string) (InstallRequest, error)
	SaveRelease(ctx context.Context, release PluginRelease) (PluginRelease, error)
	ListReleases(ctx context.Context, pluginName string) ([]PluginRelease, error)
	AppendAudit(ctx context.Context, audit MarketAudit) error
	ListAudits(ctx context.Context, pluginName string, limit int) ([]MarketAudit, error)
	Metrics(ctx context.Context, pluginName string) (MarketMetrics, error)
	UserMetrics(ctx context.Context, pluginName, ownerID string) (MarketMetrics, error)
}

type ManifestResolver func(name string) (*Manifest, bool)
type PluginActiveResolver func(name string) bool

type MarketService struct {
	store     MarketStore
	storage   corestorage.Port
	manifests ManifestResolver
	active    PluginActiveResolver
	now       func() time.Time
}

func NewMarketService(store MarketStore, storage corestorage.Port, manifests ManifestResolver) *MarketService {
	return &MarketService{store: store, storage: storage, manifests: manifests, now: time.Now}
}

// SetPluginActiveResolver binds user-facing runtime calls to the external
// plugin lifecycle without exposing Manager or runtime internals to the
// managed-data service. Retained-data export and deletion deliberately do not
// use this gate.
func (s *MarketService) SetPluginActiveResolver(resolver PluginActiveResolver) {
	s.active = resolver
}

func (s *MarketService) Available() bool {
	return s != nil && s.store != nil && s.storage != nil && s.manifests != nil
}

func (s *MarketService) SyncCatalog(ctx context.Context, plugins []*Plugin) error {
	if !s.Available() {
		return ErrMarketUnsupported
	}
	existing, err := s.store.ListCatalog(ctx, "")
	if err != nil {
		return err
	}
	visibilityByPlugin := make(map[string]string, len(existing))
	for _, entry := range existing {
		visibilityByPlugin[entry.PluginName] = entry.Visibility
	}
	for _, installed := range plugins {
		if installed == nil || installed.Manifest == nil || installed.Manifest.Runtime == "builtin" || !installed.Manifest.IsV2() {
			continue
		}
		manifest := installed.Manifest
		capabilities := []string{}
		if len(manifest.ManagedData.Collections) > 0 {
			capabilities = append(capabilities, "managed-data")
		}
		if manifest.Files.Enabled {
			capabilities = append(capabilities, "user-files")
		}
		for _, collection := range manifest.ManagedData.Collections {
			if collection.Owner == OwnerUser && len(collection.Searchable) > 0 {
				capabilities = append(capabilities, "search")
				break
			}
		}
		if len(manifest.Permissions.User) > 0 {
			capabilities = append(capabilities, "user-consent")
		}
		entry := CatalogEntry{
			PluginName: manifest.Name, DisplayName: manifest.DisplayName, Description: manifest.Description,
			Version: manifest.Version, Runtime: manifest.Runtime, Visibility: CatalogDraft,
			PackageChecksum: installed.Checksum, DataCapabilities: capabilities, UserPermissions: manifest.Permissions.User, UpdatedAt: s.now(),
		}
		if visibility, ok := visibilityByPlugin[manifest.Name]; ok {
			entry.Visibility = visibility
		}
		if _, err := s.store.UpsertCatalog(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

func (s *MarketService) SetCatalogVisibility(ctx context.Context, pluginName, visibility, actorID string) (CatalogEntry, error) {
	if visibility != CatalogDraft && visibility != CatalogPublished && visibility != CatalogHidden {
		return CatalogEntry{}, fmt.Errorf("%w: unsupported catalog visibility", ErrMarketInvalidInput)
	}
	entries, err := s.store.ListCatalog(ctx, "")
	if err != nil {
		return CatalogEntry{}, err
	}
	for _, entry := range entries {
		if entry.PluginName == pluginName {
			entry.Visibility, entry.UpdatedAt = visibility, s.now()
			saved, saveErr := s.store.UpsertCatalog(ctx, entry)
			if saveErr == nil {
				s.audit(ctx, pluginName, actorID, "catalog.visibility", "success", map[string]interface{}{"visibility": visibility})
			}
			return saved, saveErr
		}
	}
	return CatalogEntry{}, ErrMarketNotFound
}

func (s *MarketService) Catalog(ctx context.Context, publishedOnly bool) ([]CatalogEntry, error) {
	visibility := ""
	if publishedOnly {
		visibility = CatalogPublished
	}
	return s.store.ListCatalog(ctx, visibility)
}

func (s *MarketService) Grant(ctx context.Context, pluginName, userID string, permissions []string) (UserGrant, error) {
	if err := s.requirePublished(ctx, pluginName); err != nil {
		s.audit(ctx, pluginName, userID, "consent.grant", "denied", map[string]interface{}{"reason": "catalog_not_published"})
		return UserGrant{}, err
	}
	manifest, err := s.userManifest(pluginName)
	if err != nil {
		return UserGrant{}, err
	}
	allowed := declaredUserPermissions(manifest)
	// nil is reserved for internal bootstrap/test callers that deliberately ask
	// for every declared item. A user-facing HTTP request always carries an
	// explicit list, so an empty list means no personal-data permission.
	if permissions == nil {
		for permission := range allowed {
			permissions = append(permissions, permission)
		}
	}
	permissions = uniqueStrings(permissions)
	for _, permission := range permissions {
		if !allowed[permission] {
			return UserGrant{}, fmt.Errorf("%w: plugin does not declare user permission %s", ErrMarketDenied, permission)
		}
	}
	grant, err := s.store.UpsertGrant(ctx, UserGrant{
		PluginName: pluginName, UserID: userID, Version: manifest.Version, Permissions: permissions,
		Status: GrantEnabled, GrantedAt: s.now(), UpdatedAt: s.now(),
	})
	if err == nil {
		s.audit(ctx, pluginName, userID, "consent.grant", "success", map[string]interface{}{"permissions": permissions, "version": manifest.Version})
	}
	return grant, err
}

func (s *MarketService) Revoke(ctx context.Context, pluginName, userID string) (UserGrant, error) {
	grant, err := s.store.GetGrant(ctx, pluginName, userID)
	if err != nil {
		return UserGrant{}, err
	}
	now := s.now()
	grant.Status, grant.RevokedAt, grant.UpdatedAt = GrantRevoked, &now, now
	grant, err = s.store.UpsertGrant(ctx, grant)
	if err == nil {
		s.audit(ctx, pluginName, userID, "consent.revoke", "success", nil)
	}
	return grant, err
}

func (s *MarketService) MyGrants(ctx context.Context, userID string) ([]UserGrant, error) {
	return s.store.ListGrants(ctx, userID)
}

func (s *MarketService) CreateUserRecord(ctx context.Context, pluginName, userID, collection string, input RecordInput) (ManagedRecord, error) {
	manifest, definition, err := s.userCollection(ctx, pluginName, userID, collection, "write")
	if err != nil {
		return ManagedRecord{}, err
	}
	if err := validateRecordData(definition, input.Data); err != nil {
		return ManagedRecord{}, err
	}
	if input.RecordKey == "" {
		input.RecordKey = fmt.Sprintf("rec-%d", idgen.New())
	}
	if !isManifestIdentifier(input.RecordKey) {
		return ManagedRecord{}, fmt.Errorf("%w: record_key is invalid", ErrMarketInvalidInput)
	}
	data, searchText, err := normalizedRecordData(definition, input.Data)
	if err != nil {
		return ManagedRecord{}, err
	}
	if definition.MaxRecordByte > 0 && int64(len(mustJSON(data))) > definition.MaxRecordByte {
		return ManagedRecord{}, ErrMarketQuotaExceeded
	}
	if err := s.checkRecordQuota(ctx, manifest, pluginName, OwnerUser, userID, 0, int64(len(mustJSON(data)))); err != nil {
		return ManagedRecord{}, err
	}
	if definition.MaxRecords > 0 {
		page, listErr := s.store.ListRecords(ctx, RecordQuery{PluginName: pluginName, OwnerType: OwnerUser, OwnerID: userID, Collection: collection, Page: 1, PageSize: definition.MaxRecords + 1})
		if listErr != nil {
			return ManagedRecord{}, listErr
		}
		if page.Total >= definition.MaxRecords {
			return ManagedRecord{}, ErrMarketQuotaExceeded
		}
	}
	record, err := s.store.CreateRecord(ctx, ManagedRecord{
		PluginName: pluginName, OwnerType: OwnerUser, OwnerID: userID, Collection: collection,
		RecordKey: input.RecordKey, Data: data, SearchText: searchText, Version: 1, CreatedAt: s.now(), UpdatedAt: s.now(),
	})
	if err == nil {
		s.audit(ctx, pluginName, userID, "record.create", "success", map[string]interface{}{"collection": collection, "record_key": input.RecordKey})
	}
	return record, err
}

// CreateSystemRecord is the Host API path for a v2 plugin's system-owned
// collection. User-owned collections never use this method because a plugin
// process must not be allowed to choose a user identity itself.
func (s *MarketService) CreateSystemRecord(ctx context.Context, pluginName, collection string, input RecordInput) (ManagedRecord, error) {
	manifest, definition, err := s.systemCollection(pluginName, collection)
	if err != nil {
		return ManagedRecord{}, err
	}
	if err := validateRecordData(definition, input.Data); err != nil {
		return ManagedRecord{}, err
	}
	if input.RecordKey == "" {
		input.RecordKey = fmt.Sprintf("rec-%d", idgen.New())
	}
	if !isManifestIdentifier(input.RecordKey) {
		return ManagedRecord{}, fmt.Errorf("%w: record_key is invalid", ErrMarketInvalidInput)
	}
	data, searchText, err := normalizedRecordData(definition, input.Data)
	if err != nil {
		return ManagedRecord{}, err
	}
	if definition.MaxRecordByte > 0 && int64(len(mustJSON(data))) > definition.MaxRecordByte {
		return ManagedRecord{}, ErrMarketQuotaExceeded
	}
	if definition.MaxRecords > 0 {
		page, listErr := s.store.ListRecords(ctx, RecordQuery{PluginName: pluginName, OwnerType: OwnerSystem, OwnerID: "system", Collection: collection, Page: 1, PageSize: 1})
		if listErr != nil {
			return ManagedRecord{}, listErr
		}
		if page.Total >= definition.MaxRecords {
			return ManagedRecord{}, ErrMarketQuotaExceeded
		}
	}
	if err := s.checkRecordQuota(ctx, manifest, pluginName, OwnerSystem, "system", 0, int64(len(mustJSON(data)))); err != nil {
		return ManagedRecord{}, err
	}
	record, err := s.store.CreateRecord(ctx, ManagedRecord{PluginName: pluginName, OwnerType: OwnerSystem, OwnerID: "system", Collection: collection, RecordKey: input.RecordKey, Data: data, SearchText: searchText, Version: 1, CreatedAt: s.now(), UpdatedAt: s.now()})
	if err == nil {
		s.audit(ctx, pluginName, "plugin:"+pluginName, "record.system.create", "success", map[string]interface{}{"collection": collection})
	}
	return record, err
}

func (s *MarketService) GetSystemRecord(ctx context.Context, pluginName, collection, key string) (ManagedRecord, error) {
	if _, _, err := s.systemCollection(pluginName, collection); err != nil {
		return ManagedRecord{}, err
	}
	return s.store.GetRecord(ctx, pluginName, OwnerSystem, "system", collection, key)
}

func (s *MarketService) ListSystemRecords(ctx context.Context, pluginName, collection string, page, pageSize int) (RecordPage, error) {
	if _, definition, err := s.systemCollection(pluginName, collection); err != nil {
		return RecordPage{}, err
	} else if err := validateRecordQuery(definition, RecordQuery{Page: page, PageSize: pageSize}); err != nil {
		return RecordPage{}, err
	}
	return s.store.ListRecords(ctx, RecordQuery{PluginName: pluginName, OwnerType: OwnerSystem, OwnerID: "system", Collection: collection, Page: page, PageSize: pageSize})
}

func (s *MarketService) UpdateSystemRecord(ctx context.Context, pluginName, collection, key string, input RecordInput) (ManagedRecord, error) {
	manifest, definition, err := s.systemCollection(pluginName, collection)
	if err != nil {
		return ManagedRecord{}, err
	}
	if input.Version <= 0 {
		return ManagedRecord{}, fmt.Errorf("%w: version is required", ErrMarketInvalidInput)
	}
	if err := validateRecordData(definition, input.Data); err != nil {
		return ManagedRecord{}, err
	}
	data, searchText, err := normalizedRecordData(definition, input.Data)
	if err != nil {
		return ManagedRecord{}, err
	}
	if definition.MaxRecordByte > 0 && int64(len(mustJSON(data))) > definition.MaxRecordByte {
		return ManagedRecord{}, ErrMarketQuotaExceeded
	}
	current, err := s.store.GetRecord(ctx, pluginName, OwnerSystem, "system", collection, key)
	if err != nil {
		return ManagedRecord{}, err
	}
	if err := s.checkRecordQuota(ctx, manifest, pluginName, OwnerSystem, "system", int64(len(mustJSON(current.Data))), int64(len(mustJSON(data)))); err != nil {
		return ManagedRecord{}, err
	}
	return s.store.UpdateRecord(ctx, ManagedRecord{PluginName: pluginName, OwnerType: OwnerSystem, OwnerID: "system", Collection: collection, RecordKey: key, Data: data, SearchText: searchText, Version: input.Version, UpdatedAt: s.now()})
}

func (s *MarketService) DeleteSystemRecord(ctx context.Context, pluginName, collection, key string, version int64) error {
	if _, _, err := s.systemCollection(pluginName, collection); err != nil {
		return err
	}
	if version <= 0 {
		return fmt.Errorf("%w: version is required", ErrMarketInvalidInput)
	}
	return s.store.DeleteRecord(ctx, pluginName, OwnerSystem, "system", collection, key, version)
}

func (s *MarketService) GetUserRecord(ctx context.Context, pluginName, userID, collection, key string) (ManagedRecord, error) {
	if _, _, err := s.userCollection(ctx, pluginName, userID, collection, "read"); err != nil {
		return ManagedRecord{}, err
	}
	return s.store.GetRecord(ctx, pluginName, OwnerUser, userID, collection, key)
}

func (s *MarketService) UpdateUserRecord(ctx context.Context, pluginName, userID, collection, key string, input RecordInput) (ManagedRecord, error) {
	manifest, definition, err := s.userCollection(ctx, pluginName, userID, collection, "write")
	if err != nil {
		return ManagedRecord{}, err
	}
	if input.Version <= 0 {
		return ManagedRecord{}, fmt.Errorf("%w: version is required", ErrMarketInvalidInput)
	}
	if err := validateRecordData(definition, input.Data); err != nil {
		return ManagedRecord{}, err
	}
	data, searchText, err := normalizedRecordData(definition, input.Data)
	if err != nil {
		return ManagedRecord{}, err
	}
	if definition.MaxRecordByte > 0 && int64(len(mustJSON(data))) > definition.MaxRecordByte {
		return ManagedRecord{}, ErrMarketQuotaExceeded
	}
	current, err := s.store.GetRecord(ctx, pluginName, OwnerUser, userID, collection, key)
	if err != nil {
		return ManagedRecord{}, err
	}
	if err := s.checkRecordQuota(ctx, manifest, pluginName, OwnerUser, userID, int64(len(mustJSON(current.Data))), int64(len(mustJSON(data)))); err != nil {
		return ManagedRecord{}, err
	}
	record, err := s.store.UpdateRecord(ctx, ManagedRecord{PluginName: pluginName, OwnerType: OwnerUser, OwnerID: userID, Collection: collection, RecordKey: key, Data: data, SearchText: searchText, Version: input.Version, UpdatedAt: s.now()})
	if err == nil {
		s.audit(ctx, pluginName, userID, "record.update", "success", map[string]interface{}{"collection": collection, "record_key": key})
	}
	return record, err
}

func (s *MarketService) DeleteUserRecord(ctx context.Context, pluginName, userID, collection, key string, version int64) error {
	if _, _, err := s.userCollection(ctx, pluginName, userID, collection, "delete"); err != nil {
		return err
	}
	if version <= 0 {
		return fmt.Errorf("%w: version is required", ErrMarketInvalidInput)
	}
	err := s.store.DeleteRecord(ctx, pluginName, OwnerUser, userID, collection, key, version)
	if err == nil {
		s.audit(ctx, pluginName, userID, "record.delete", "success", map[string]interface{}{"collection": collection, "record_key": key})
	}
	return err
}

func (s *MarketService) ListUserRecords(ctx context.Context, query RecordQuery) (RecordPage, error) {
	if _, definition, err := s.userCollection(ctx, query.PluginName, query.OwnerID, query.Collection, "read"); err != nil {
		return RecordPage{}, err
	} else if err := validateRecordQuery(definition, query); err != nil {
		return RecordPage{}, err
	}
	query.OwnerType = OwnerUser
	return s.store.ListRecords(ctx, query)
}

// SearchMyRecords is deliberately limited to one user-owned, manifest-
// declared collection. It never scans legacy SQLite, arbitrary files or other
// plugins, so search cannot become a cross-plugin data-discovery channel.
func (s *MarketService) SearchMyRecords(ctx context.Context, pluginName, userID, collection, keyword string, page, pageSize int) (RecordPage, error) {
	if err := s.requireUserGrant(ctx, pluginName, userID, PluginSearchResource+":read"); err != nil {
		return RecordPage{}, err
	}
	return s.ListUserRecords(ctx, RecordQuery{
		PluginName: pluginName,
		OwnerID:    userID,
		Collection: collection,
		Keyword:    strings.TrimSpace(keyword),
		Page:       page,
		PageSize:   pageSize,
	})
}

func (s *MarketService) UploadUserFile(ctx context.Context, pluginName, userID, name, contentType string, body []byte) (PluginFile, error) {
	manifest, err := s.userManifest(pluginName)
	if err != nil {
		return PluginFile{}, err
	}
	if err := s.requireUserGrant(ctx, pluginName, userID, PluginFileResource+":write"); err != nil {
		return PluginFile{}, err
	}
	if !manifest.Files.Enabled {
		return PluginFile{}, ErrMarketUnsupported
	}
	if len(body) == 0 || (manifest.Files.MaxFileBytes > 0 && int64(len(body)) > manifest.Files.MaxFileBytes) {
		return PluginFile{}, ErrMarketQuotaExceeded
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" || strings.ContainsAny(name, `/\\`) {
		return PluginFile{}, fmt.Errorf("%w: unsafe file name", ErrMarketInvalidInput)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if len(manifest.Files.AllowedExts) > 0 && !containsString(manifest.Files.AllowedExts, ext) {
		return PluginFile{}, fmt.Errorf("%w: file extension is not declared", ErrMarketDenied)
	}
	detected := normalizeMediaType(http.DetectContentType(body))
	declared := normalizeMediaType(contentType)
	if len(manifest.Files.AllowedMIMEs) > 0 {
		if !allowedDetectedMIME(manifest.Files.AllowedMIMEs, declared, detected) {
			return PluginFile{}, fmt.Errorf("%w: file content does not match a declared MIME type", ErrMarketDenied)
		}
		if declared != "" && declared != "application/octet-stream" && !containsMediaType(manifest.Files.AllowedMIMEs, declared) {
			return PluginFile{}, fmt.Errorf("%w: declared file MIME type is not allowed", ErrMarketDenied)
		}
	}
	contentType = detected
	if declared != "" && declared != "application/octet-stream" && (declared == detected || detected == "text/plain") {
		contentType = declared
	}
	usage, err := s.store.FileUsage(ctx, pluginName, userID)
	if err != nil {
		return PluginFile{}, err
	}
	if manifest.Files.QuotaBytes > 0 && usage+int64(len(body)) > manifest.Files.QuotaBytes {
		return PluginFile{}, ErrMarketQuotaExceeded
	}
	if quota, ok := s.storage.(corestorage.Quota); ok {
		if err := quota.CheckQuota(userID, int64(len(body))); err != nil {
			return PluginFile{}, err
		}
	}
	dir, err := s.storage.Path(userID, "plugins", pluginName)
	if err != nil {
		return PluginFile{}, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return PluginFile{}, err
	}
	id := idgen.New()
	stored := fmt.Sprintf("asset-%d%s", id, ext)
	path := filepath.Join(dir, stored)
	if err := os.WriteFile(path, body, 0o640); err != nil {
		return PluginFile{}, err
	}
	file, err := s.store.SaveFile(ctx, PluginFile{ID: id, PluginName: pluginName, OwnerID: userID, OriginalName: name, StoredName: stored, ContentType: contentType, Size: int64(len(body)), StorageKey: filepath.ToSlash(filepath.Join("plugins", pluginName, stored)), Retention: defaultRetention(manifest.Files.Retention), CreatedAt: s.now()})
	if err != nil {
		_ = os.Remove(path)
		return PluginFile{}, err
	}
	s.audit(ctx, pluginName, userID, "file.upload", "success", map[string]interface{}{"file_id": file.ID, "size": file.Size})
	return file, nil
}

func (s *MarketService) ListUserFiles(ctx context.Context, pluginName, userID string) ([]PluginFile, error) {
	if _, err := s.userManifest(pluginName); err != nil {
		return nil, err
	}
	if err := s.requireUserGrant(ctx, pluginName, userID, PluginFileResource+":read"); err != nil {
		return nil, err
	}
	return s.store.ListFiles(ctx, pluginName, userID)
}

func (s *MarketService) UserFilePath(ctx context.Context, pluginName, userID, fileID string) (PluginFile, string, error) {
	if _, err := s.userManifest(pluginName); err != nil {
		return PluginFile{}, "", err
	}
	if err := s.requireUserGrant(ctx, pluginName, userID, PluginFileResource+":read"); err != nil {
		return PluginFile{}, "", err
	}
	file, err := s.store.GetFile(ctx, pluginName, userID, fileID)
	if err != nil {
		return PluginFile{}, "", err
	}
	path, err := s.storage.Path(userID, "plugins", pluginName, file.StoredName)
	if err != nil {
		return PluginFile{}, "", err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PluginFile{}, "", ErrMarketNotFound
		}
		return PluginFile{}, "", err
	}
	return file, path, nil
}

func (s *MarketService) DeleteUserFile(ctx context.Context, pluginName, userID, fileID string) error {
	if _, err := s.userManifest(pluginName); err != nil {
		return err
	}
	if err := s.requireUserGrant(ctx, pluginName, userID, PluginFileResource+":delete"); err != nil {
		return err
	}
	file, err := s.store.GetFile(ctx, pluginName, userID, fileID)
	if err != nil {
		return err
	}
	if err := s.deleteStoredFile(ctx, file); err != nil {
		return err
	}
	s.audit(ctx, pluginName, userID, "file.delete", "success", map[string]interface{}{"file_id": file.ID})
	return nil
}

func (s *MarketService) DeleteMyData(ctx context.Context, pluginName, userID string) (int, int, error) {
	grant, err := s.retainedGrant(ctx, pluginName, userID)
	if err != nil {
		return 0, 0, err
	}
	canDeleteRecords := containsString(grant.Permissions, ManagedDataResource+":delete")
	canDeleteFiles := containsString(grant.Permissions, PluginFileResource+":delete")
	if !canDeleteRecords && !canDeleteFiles {
		return 0, 0, ErrMarketDenied
	}
	records := 0
	if canDeleteRecords {
		records, err = s.store.DeleteOwnerRecords(ctx, pluginName, userID)
		if err != nil {
			return 0, 0, err
		}
	}
	files := []PluginFile{}
	if canDeleteFiles {
		files, err = s.store.ListFiles(ctx, pluginName, userID)
		if err != nil {
			return records, 0, err
		}
	}
	deletedFiles := 0
	for _, file := range files {
		if err := s.deleteStoredFile(ctx, file); err != nil {
			return records, deletedFiles, err
		}
		deletedFiles++
	}
	if _, err := s.Revoke(ctx, pluginName, userID); err != nil {
		return records, deletedFiles, err
	}
	s.audit(ctx, pluginName, userID, "data.delete", "success", map[string]interface{}{"records": records, "files": deletedFiles})
	return records, deletedFiles, nil
}

func (s *MarketService) ExportMyData(ctx context.Context, pluginName, userID string) (map[string]interface{}, error) {
	grant, err := s.retainedGrant(ctx, pluginName, userID)
	if err != nil {
		return nil, err
	}
	canReadRecords := containsString(grant.Permissions, ManagedDataResource+":read")
	canReadFiles := containsString(grant.Permissions, PluginFileResource+":read")
	if !canReadRecords && !canReadFiles {
		return nil, ErrMarketDenied
	}
	items := RecordPage{Items: []ManagedRecord{}}
	if canReadRecords {
		items, err = s.store.ListRecords(ctx, RecordQuery{PluginName: pluginName, OwnerType: OwnerUser, OwnerID: userID, Page: 1, PageSize: 1000})
		if err != nil {
			return nil, err
		}
	}
	files := []PluginFile{}
	if canReadFiles {
		files, err = s.store.ListFiles(ctx, pluginName, userID)
		if err != nil {
			return nil, err
		}
	}
	s.audit(ctx, pluginName, userID, "data.export", "success", nil)
	return map[string]interface{}{"plugin_name": pluginName, "exported_at": s.now(), "records": items.Items, "files": files, "secrets_included": false}, nil
}

func (s *MarketService) MyUsage(ctx context.Context, userID string) ([]MarketMetrics, error) {
	grants, err := s.store.ListGrants(ctx, userID)
	if err != nil {
		return nil, err
	}
	items := make([]MarketMetrics, 0, len(grants))
	for _, grant := range grants {
		metrics, metricErr := s.store.UserMetrics(ctx, grant.PluginName, userID)
		if metricErr != nil {
			return nil, metricErr
		}
		if manifest, ok := s.manifests(grant.PluginName); ok && manifest != nil {
			for _, collection := range manifest.ManagedData.Collections {
				if collection.Owner == OwnerUser && len(collection.Searchable) > 0 {
					metrics.SearchEnabled = containsString(grant.Permissions, PluginSearchResource+":read") && grant.Status == GrantEnabled && grant.Version == manifest.Version
					break
				}
			}
		}
		items = append(items, metrics)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PluginName < items[j].PluginName })
	return items, nil
}

func (s *MarketService) Metrics(ctx context.Context, pluginName string) (MarketMetrics, error) {
	return s.store.Metrics(ctx, pluginName)
}

func (s *MarketService) RequestInstall(ctx context.Context, pluginName, userID, message string) (InstallRequest, error) {
	pluginName = strings.TrimSpace(pluginName)
	if err := ValidatePluginName(pluginName); err != nil {
		return InstallRequest{}, fmt.Errorf("%w: invalid requested plugin id", ErrMarketInvalidInput)
	}
	message = strings.TrimSpace(message)
	if len(message) > 1000 {
		return InstallRequest{}, fmt.Errorf("%w: request message is too long", ErrMarketInvalidInput)
	}
	request, err := s.store.CreateInstallRequest(ctx, InstallRequest{ID: idgen.New(), PluginName: pluginName, UserID: userID, Message: message, Status: RequestPending, CreatedAt: s.now()})
	if err == nil {
		s.audit(ctx, pluginName, userID, "catalog.request", "success", nil)
	}
	return request, err
}

func (s *MarketService) ReviewInstallRequest(ctx context.Context, id int64, actorID, status string) (InstallRequest, error) {
	if status != RequestApproved && status != RequestRejected {
		return InstallRequest{}, fmt.Errorf("%w: invalid request status", ErrMarketInvalidInput)
	}
	request, err := s.store.ReviewInstallRequest(ctx, id, actorID, status)
	if err == nil {
		s.audit(ctx, request.PluginName, actorID, "catalog.request.review", "success", map[string]interface{}{"request_id": id, "status": status})
	}
	return request, err
}

func (s *MarketService) InstallRequests(ctx context.Context, status string) ([]InstallRequest, error) {
	return s.store.ListInstallRequests(ctx, status)
}

func (s *MarketService) SaveRelease(ctx context.Context, release PluginRelease, actorID string) (PluginRelease, error) {
	if release.PluginName == "" || release.Version == "" || release.Checksum == "" {
		return PluginRelease{}, fmt.Errorf("%w: release plugin, version and checksum are required", ErrMarketInvalidInput)
	}
	if _, err := s.userManifest(release.PluginName); err != nil {
		return PluginRelease{}, err
	}
	if release.SignatureState == "verified" {
		return PluginRelease{}, fmt.Errorf("%w: verified signatures can only be recorded by package import", ErrMarketInvalidInput)
	}
	if release.SignatureState == "" {
		release.SignatureState = "pending"
	}
	if release.SignatureState != "pending" && release.SignatureState != "unsigned" && release.SignatureState != "invalid" && release.SignatureState != "untrusted" {
		return PluginRelease{}, fmt.Errorf("%w: invalid signature state", ErrMarketInvalidInput)
	}
	if release.Channel == "" {
		release.Channel = "stable"
	}
	if release.Channel != "stable" && release.Channel != "beta" && release.Channel != "canary" {
		return PluginRelease{}, fmt.Errorf("%w: invalid release channel", ErrMarketInvalidInput)
	}
	if release.RolloutState == "" {
		release.RolloutState = "pending"
	}
	if release.RolloutState != "pending" && release.RolloutState != "published" && release.RolloutState != "paused" && release.RolloutState != "rolled_back" {
		return PluginRelease{}, fmt.Errorf("%w: invalid rollout state", ErrMarketInvalidInput)
	}
	release.CreatedAt = s.now()
	saved, err := s.store.SaveRelease(ctx, release)
	if err == nil {
		s.audit(ctx, release.PluginName, actorID, "release.save", "success", map[string]interface{}{"version": release.Version, "signature_state": release.SignatureState})
	}
	return saved, err
}

// RecordImportedRelease is the only path that may persist a verified
// signature state. Its inputs come from the host package precheck, never from
// an administrator-supplied release form.
func (s *MarketService) RecordImportedRelease(ctx context.Context, manifest *Manifest, checksum, signatureState, actorID string) (PluginRelease, error) {
	if manifest == nil || !manifest.IsV2() || manifest.Runtime == "builtin" || checksum == "" {
		return PluginRelease{}, fmt.Errorf("%w: imported release metadata is incomplete", ErrMarketInvalidInput)
	}
	if signatureState != "verified" && signatureState != "unsigned" && signatureState != "untrusted" && signatureState != "invalid" {
		return PluginRelease{}, fmt.Errorf("%w: invalid imported signature state", ErrMarketInvalidInput)
	}
	channel := strings.TrimSpace(manifest.Release.Channel)
	if channel == "" {
		channel = "stable"
	}
	release := PluginRelease{
		PluginName: manifest.Name, Version: manifest.Version, Checksum: checksum,
		SignatureState: signatureState, Channel: channel, RolloutState: "imported", CreatedAt: s.now(),
	}
	saved, err := s.store.SaveRelease(ctx, release)
	if err == nil {
		s.audit(ctx, manifest.Name, actorID, "release.import", "success", map[string]interface{}{"version": manifest.Version, "signature_state": signatureState})
	}
	return saved, err
}

func (s *MarketService) Releases(ctx context.Context, pluginName string) ([]PluginRelease, error) {
	return s.store.ListReleases(ctx, pluginName)
}

func (s *MarketService) Audits(ctx context.Context, pluginName string, limit int) ([]MarketAudit, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.store.ListAudits(ctx, strings.TrimSpace(pluginName), limit)
}

func (s *MarketService) userManifest(pluginName string) (*Manifest, error) {
	if !s.Available() {
		return nil, ErrMarketUnsupported
	}
	manifest, ok := s.manifests(pluginName)
	if !ok || manifest == nil {
		return nil, ErrMarketNotFound
	}
	if !manifest.IsV2() || manifest.Runtime == "builtin" {
		return nil, ErrMarketUnsupported
	}
	return manifest, nil
}

func (s *MarketService) userCollection(ctx context.Context, pluginName, userID, collection, action string) (*Manifest, DataCollection, error) {
	manifest, err := s.userManifest(pluginName)
	if err != nil {
		return nil, DataCollection{}, err
	}
	definition, ok := manifest.Collection(collection)
	if !ok || definition.Owner != OwnerUser {
		return nil, DataCollection{}, ErrMarketUnsupported
	}
	if err := s.requireUserGrant(ctx, pluginName, userID, ManagedDataResource+":"+action); err != nil {
		return nil, DataCollection{}, err
	}
	return manifest, definition, nil
}

func (s *MarketService) systemCollection(pluginName, collection string) (*Manifest, DataCollection, error) {
	manifest, err := s.userManifest(pluginName)
	if err != nil {
		return nil, DataCollection{}, err
	}
	definition, ok := manifest.Collection(collection)
	if !ok || definition.Owner != OwnerSystem {
		return nil, DataCollection{}, ErrMarketUnsupported
	}
	return manifest, definition, nil
}

func (s *MarketService) requireUserGrant(ctx context.Context, pluginName, userID, permission string) error {
	if err := s.requirePublished(ctx, pluginName); err != nil {
		s.audit(ctx, pluginName, userID, "authorization.check", "denied", map[string]interface{}{"permission": permission, "reason": "catalog_not_published"})
		return ErrMarketDenied
	}
	grant, err := s.store.GetGrant(ctx, pluginName, userID)
	if err != nil {
		s.audit(ctx, pluginName, userID, "authorization.check", "denied", map[string]interface{}{"permission": permission, "reason": "grant_missing"})
		return ErrMarketDenied
	}
	manifest, err := s.userManifest(pluginName)
	if err != nil || grant.Status != GrantEnabled || grant.Version != manifest.Version || !containsString(grant.Permissions, permission) {
		reason := "permission_missing"
		if grant.Status != GrantEnabled {
			reason = "grant_revoked"
		} else if err == nil && grant.Version != manifest.Version {
			reason = "version_changed"
		}
		s.audit(ctx, pluginName, userID, "authorization.check", "denied", map[string]interface{}{"permission": permission, "reason": reason})
		return ErrMarketDenied
	}
	return nil
}

func (s *MarketService) requirePublished(ctx context.Context, pluginName string) error {
	if s.active != nil && !s.active(pluginName) {
		s.audit(ctx, pluginName, "system", "catalog.access", "denied", map[string]interface{}{"reason": "plugin_not_running"})
		return ErrMarketDenied
	}
	entries, err := s.store.ListCatalog(ctx, CatalogPublished)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.PluginName == pluginName {
			return nil
		}
	}
	return ErrMarketDenied
}

// retainedGrant intentionally does not require an installed manifest or a
// published catalog entry. A user must still be able to export or delete
// retained personal data after an administrator hides or uninstalls code.
func (s *MarketService) retainedGrant(ctx context.Context, pluginName, userID string) (UserGrant, error) {
	grant, err := s.store.GetGrant(ctx, pluginName, userID)
	if err != nil || grant.Status != GrantEnabled {
		return UserGrant{}, ErrMarketDenied
	}
	return grant, nil
}

func (s *MarketService) checkRecordQuota(ctx context.Context, manifest *Manifest, pluginName, ownerType, ownerID string, previousBytes, nextBytes int64) error {
	if manifest == nil || manifest.ManagedData.DefaultQuotaByte <= 0 {
		return nil
	}
	usage, err := s.store.RecordUsage(ctx, pluginName, ownerType, ownerID)
	if err != nil {
		return err
	}
	projected := usage + nextBytes
	if previousBytes > 0 && previousBytes <= usage {
		projected -= previousBytes
	}
	if previousBytes < 0 || nextBytes < 0 || projected > manifest.ManagedData.DefaultQuotaByte {
		return ErrMarketQuotaExceeded
	}
	return nil
}

func (s *MarketService) deleteStoredFile(ctx context.Context, file PluginFile) error {
	path, err := s.storage.Path(file.OwnerID, "plugins", file.PluginName, file.StoredName)
	if err != nil {
		return err
	}
	staged := path + ".deleting"
	renamed := false
	if err := os.Rename(path, staged); err == nil {
		renamed = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := s.store.DeleteFile(ctx, file.PluginName, file.OwnerID, fmt.Sprint(file.ID)); err != nil {
		if renamed {
			_ = os.Rename(staged, path)
		}
		return err
	}
	if renamed {
		if err := os.Remove(staged); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *MarketService) audit(ctx context.Context, pluginName, actorID, action, outcome string, metadata map[string]interface{}) {
	if s == nil || s.store == nil {
		return
	}
	_ = s.store.AppendAudit(ctx, MarketAudit{ID: idgen.New(), PluginName: pluginName, ActorID: actorID, Action: action, Outcome: outcome, Metadata: metadata, CreatedAt: s.now()})
}

func declaredUserPermissions(manifest *Manifest) map[string]bool {
	allowed := map[string]bool{}
	for _, permission := range manifest.Permissions.User {
		for _, action := range permission.Actions {
			allowed[permission.Resource+":"+action] = true
		}
	}
	return allowed
}

func validateRecordData(collection DataCollection, data map[string]interface{}) error {
	if data == nil {
		return fmt.Errorf("%w: data is required", ErrMarketInvalidInput)
	}
	declared := make(map[string]bool, len(collection.Fields))
	for _, field := range collection.Fields {
		declared[field.Name] = true
		value, found := data[field.Name]
		if field.Required && !found {
			return fmt.Errorf("%w: required field %s is missing", ErrMarketInvalidInput, field.Name)
		}
		if found && !validFieldValue(field.Type, value) {
			return fmt.Errorf("%w: field %s has invalid type", ErrMarketInvalidInput, field.Name)
		}
	}
	for field := range data {
		if !declared[field] {
			return fmt.Errorf("%w: field %s is not declared by the collection schema", ErrMarketInvalidInput, field)
		}
	}
	return nil
}

func validFieldValue(kind string, value interface{}) bool {
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case float64, float32, int, int64, int32, json.Number:
			return true
		default:
			return false
		}
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "array":
		_, ok := value.([]interface{})
		return ok
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	default:
		return false
	}
}

func normalizedRecordData(collection DataCollection, data map[string]interface{}) (map[string]interface{}, string, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, "", fmt.Errorf("%w: record data cannot be encoded", ErrMarketInvalidInput)
	}
	var copy map[string]interface{}
	if err := json.Unmarshal(encoded, &copy); err != nil {
		return nil, "", err
	}
	parts := make([]string, 0, len(collection.Searchable))
	for _, field := range collection.Searchable {
		if value, ok := copy[field]; ok {
			parts = append(parts, fmt.Sprint(value))
		}
	}
	return copy, strings.ToLower(strings.Join(parts, " ")), nil
}

func validateRecordQuery(collection DataCollection, query RecordQuery) error {
	for field := range query.Filters {
		if !containsString(collection.Filterable, field) {
			return fmt.Errorf("%w: filter field %s is not declared", ErrMarketDenied, field)
		}
	}
	if query.Keyword != "" && len(collection.Searchable) == 0 {
		return fmt.Errorf("%w: collection is not searchable", ErrMarketDenied)
	}
	if query.Page < 0 || query.PageSize < 0 || query.PageSize > 100 {
		return fmt.Errorf("%w: invalid pagination", ErrMarketInvalidInput)
	}
	return nil
}

func mustJSON(value interface{}) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func uniqueStrings(input []string) []string {
	seen := map[string]bool{}
	output := make([]string, 0, len(input))
	for _, value := range input {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			output = append(output, value)
		}
	}
	sort.Strings(output)
	return output
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func normalizeMediaType(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return value
	}
	return mediaType
}

func allowedDetectedMIME(allowed []string, declared, detected string) bool {
	for _, value := range allowed {
		value = normalizeMediaType(value)
		if value == detected {
			return true
		}
		// net/http deliberately sniffs JSON, XML and other textual formats as
		// text/plain. In that narrow case the browser declaration may refine the
		// type, but it cannot turn binary content into an allowed text file.
		if detected == "text/plain" && declared == value && (strings.HasSuffix(value, "+json") || strings.HasSuffix(value, "+xml") || value == "application/json" || value == "application/xml") {
			return true
		}
	}
	return false
}

func containsMediaType(values []string, expected string) bool {
	for _, value := range values {
		if normalizeMediaType(value) == expected {
			return true
		}
	}
	return false
}

func defaultRetention(value string) string {
	if value == "" {
		return "user-deletable"
	}
	return value
}

// MemoryMarketStore is intentionally complete, rather than a test-only stub,
// so the existing no-PostgreSQL development profile has the same policy and
// ownership semantics as PostgreSQL.
type MemoryMarketStore struct {
	mu       sync.RWMutex
	records  map[string]ManagedRecord
	grants   map[string]UserGrant
	files    map[string]PluginFile
	catalog  map[string]CatalogEntry
	requests map[int64]InstallRequest
	releases map[string][]PluginRelease
	audits   []MarketAudit
}

func NewMemoryMarketStore() *MemoryMarketStore {
	return &MemoryMarketStore{records: map[string]ManagedRecord{}, grants: map[string]UserGrant{}, files: map[string]PluginFile{}, catalog: map[string]CatalogEntry{}, requests: map[int64]InstallRequest{}, releases: map[string][]PluginRelease{}}
}

func recordStoreKey(pluginName, ownerType, ownerID, collection, key string) string {
	return strings.Join([]string{pluginName, ownerType, ownerID, collection, key}, "\x00")
}
func grantStoreKey(pluginName, userID string) string { return pluginName + "\x00" + userID }
func fileStoreKey(pluginName, ownerID, fileID string) string {
	return pluginName + "\x00" + ownerID + "\x00" + fileID
}

func (m *MemoryMarketStore) CreateRecord(_ context.Context, record ManagedRecord) (ManagedRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := recordStoreKey(record.PluginName, record.OwnerType, record.OwnerID, record.Collection, record.RecordKey)
	if _, found := m.records[key]; found {
		return ManagedRecord{}, ErrMarketConflict
	}
	record.ID = idgen.New()
	m.records[key] = cloneRecord(record)
	return cloneRecord(record), nil
}

func (m *MemoryMarketStore) GetRecord(_ context.Context, pluginName, ownerType, ownerID, collection, key string) (ManagedRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, found := m.records[recordStoreKey(pluginName, ownerType, ownerID, collection, key)]
	if !found {
		return ManagedRecord{}, ErrMarketNotFound
	}
	return cloneRecord(record), nil
}

func (m *MemoryMarketStore) UpdateRecord(_ context.Context, record ManagedRecord) (ManagedRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := recordStoreKey(record.PluginName, record.OwnerType, record.OwnerID, record.Collection, record.RecordKey)
	current, found := m.records[key]
	if !found {
		return ManagedRecord{}, ErrMarketNotFound
	}
	if current.Version != record.Version {
		return ManagedRecord{}, ErrMarketVersionMismatch
	}
	record.ID, record.CreatedAt, record.Version = current.ID, current.CreatedAt, current.Version+1
	m.records[key] = cloneRecord(record)
	return cloneRecord(record), nil
}

func (m *MemoryMarketStore) DeleteRecord(_ context.Context, pluginName, ownerType, ownerID, collection, key string, version int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	storeKey := recordStoreKey(pluginName, ownerType, ownerID, collection, key)
	record, found := m.records[storeKey]
	if !found {
		return ErrMarketNotFound
	}
	if record.Version != version {
		return ErrMarketVersionMismatch
	}
	delete(m.records, storeKey)
	return nil
}

func (m *MemoryMarketStore) ListRecords(_ context.Context, query RecordQuery) (RecordPage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := []ManagedRecord{}
	for _, record := range m.records {
		if query.PluginName != "" && record.PluginName != query.PluginName || query.OwnerType != "" && record.OwnerType != query.OwnerType || query.OwnerID != "" && record.OwnerID != query.OwnerID || query.Collection != "" && record.Collection != query.Collection {
			continue
		}
		if query.Keyword != "" && !strings.Contains(record.SearchText, strings.ToLower(query.Keyword)) {
			continue
		}
		matches := true
		for field, expected := range query.Filters {
			if fmt.Sprint(record.Data[field]) != expected {
				matches = false
				break
			}
		}
		if matches {
			items = append(items, cloneRecord(record))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return pageRecords(items, query.Page, query.PageSize), nil
}

func (m *MemoryMarketStore) RecordUsage(_ context.Context, pluginName, ownerType, ownerID string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var bytes int64
	for _, record := range m.records {
		if record.PluginName == pluginName && record.OwnerType == ownerType && record.OwnerID == ownerID {
			bytes += int64(len(mustJSON(record.Data)))
		}
	}
	return bytes, nil
}

func (m *MemoryMarketStore) DeleteOwnerRecords(_ context.Context, pluginName, ownerID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for key, record := range m.records {
		if record.PluginName == pluginName && record.OwnerType == OwnerUser && record.OwnerID == ownerID {
			delete(m.records, key)
			count++
		}
	}
	return count, nil
}

func (m *MemoryMarketStore) UpsertGrant(_ context.Context, grant UserGrant) (UserGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := grantStoreKey(grant.PluginName, grant.UserID)
	if existing, found := m.grants[key]; found {
		grant.ID = existing.ID
	} else if grant.ID == 0 {
		grant.ID = idgen.New()
	}
	m.grants[key] = cloneGrant(grant)
	return cloneGrant(grant), nil
}

func (m *MemoryMarketStore) GetGrant(_ context.Context, pluginName, userID string) (UserGrant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	grant, found := m.grants[grantStoreKey(pluginName, userID)]
	if !found {
		return UserGrant{}, ErrMarketNotFound
	}
	return cloneGrant(grant), nil
}

func (m *MemoryMarketStore) ListGrants(_ context.Context, userID string) ([]UserGrant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := []UserGrant{}
	for _, grant := range m.grants {
		if userID == "" || grant.UserID == userID {
			items = append(items, cloneGrant(grant))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items, nil
}

func (m *MemoryMarketStore) SaveFile(_ context.Context, file PluginFile) (PluginFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[fileStoreKey(file.PluginName, file.OwnerID, fmt.Sprint(file.ID))] = file
	return file, nil
}

func (m *MemoryMarketStore) GetFile(_ context.Context, pluginName, ownerID, fileID string) (PluginFile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	file, found := m.files[fileStoreKey(pluginName, ownerID, fileID)]
	if !found {
		return PluginFile{}, ErrMarketNotFound
	}
	return file, nil
}

func (m *MemoryMarketStore) ListFiles(_ context.Context, pluginName, ownerID string) ([]PluginFile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := []PluginFile{}
	for _, file := range m.files {
		if file.PluginName == pluginName && file.OwnerID == ownerID && file.DeletedAt == nil {
			items = append(items, file)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (m *MemoryMarketStore) DeleteFile(_ context.Context, pluginName, ownerID, fileID string) (PluginFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fileStoreKey(pluginName, ownerID, fileID)
	file, found := m.files[key]
	if !found || file.DeletedAt != nil {
		return PluginFile{}, ErrMarketNotFound
	}
	now := time.Now()
	file.DeletedAt = &now
	m.files[key] = file
	return file, nil
}

func (m *MemoryMarketStore) FileUsage(_ context.Context, pluginName, ownerID string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var bytes int64
	for _, file := range m.files {
		if file.PluginName == pluginName && file.OwnerID == ownerID && file.DeletedAt == nil {
			bytes += file.Size
		}
	}
	return bytes, nil
}

func (m *MemoryMarketStore) UpsertCatalog(_ context.Context, entry CatalogEntry) (CatalogEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, found := m.catalog[entry.PluginName]; found && entry.Visibility == CatalogDraft {
		entry.Visibility = old.Visibility
	}
	m.catalog[entry.PluginName] = entry
	return entry, nil
}

func (m *MemoryMarketStore) ListCatalog(_ context.Context, visibility string) ([]CatalogEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := []CatalogEntry{}
	for _, entry := range m.catalog {
		if visibility == "" || entry.Visibility == visibility {
			items = append(items, entry)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PluginName < items[j].PluginName })
	return items, nil
}

func (m *MemoryMarketStore) CreateInstallRequest(_ context.Context, request InstallRequest) (InstallRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.requests {
		if existing.PluginName == request.PluginName && existing.UserID == request.UserID && existing.Status == RequestPending {
			return InstallRequest{}, ErrMarketConflict
		}
	}
	m.requests[request.ID] = request
	return request, nil
}

func (m *MemoryMarketStore) ListInstallRequests(_ context.Context, status string) ([]InstallRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := []InstallRequest{}
	for _, request := range m.requests {
		if status == "" || request.Status == status {
			items = append(items, request)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (m *MemoryMarketStore) ReviewInstallRequest(_ context.Context, id int64, reviewer, status string) (InstallRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	request, found := m.requests[id]
	if !found {
		return InstallRequest{}, ErrMarketNotFound
	}
	if request.Status != RequestPending {
		return InstallRequest{}, ErrMarketConflict
	}
	now := time.Now()
	request.Status, request.ReviewedBy, request.ReviewedAt = status, reviewer, &now
	m.requests[id] = request
	return request, nil
}

func (m *MemoryMarketStore) SaveRelease(_ context.Context, release PluginRelease) (PluginRelease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for index, existing := range m.releases[release.PluginName] {
		if existing.Version == release.Version && existing.Checksum == release.Checksum {
			release.ID = existing.ID
			release.CreatedAt = existing.CreatedAt
			m.releases[release.PluginName][index] = release
			return release, nil
		}
	}
	if release.ID == 0 {
		release.ID = idgen.New()
	}
	m.releases[release.PluginName] = append(m.releases[release.PluginName], release)
	return release, nil
}

func (m *MemoryMarketStore) ListReleases(_ context.Context, pluginName string) ([]PluginRelease, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]PluginRelease(nil), m.releases[pluginName]...), nil
}

func (m *MemoryMarketStore) AppendAudit(_ context.Context, audit MarketAudit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audits = append(m.audits, audit)
	return nil
}

func (m *MemoryMarketStore) ListAudits(_ context.Context, pluginName string, limit int) ([]MarketAudit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]MarketAudit, 0, len(m.audits))
	for index := len(m.audits) - 1; index >= 0 && len(items) < limit; index-- {
		if pluginName == "" || m.audits[index].PluginName == pluginName {
			items = append(items, m.audits[index])
		}
	}
	return items, nil
}

func (m *MemoryMarketStore) Metrics(_ context.Context, pluginName string) (MarketMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	metrics := MarketMetrics{PluginName: pluginName}
	users := map[string]bool{}
	for _, record := range m.records {
		if record.PluginName == pluginName {
			metrics.RecordCount++
			metrics.RecordBytes += int64(len(mustJSON(record.Data)))
			if record.OwnerType == OwnerUser {
				users[record.OwnerID] = true
			}
			metrics.LastUpdated = newestTime(metrics.LastUpdated, &record.UpdatedAt)
		}
	}
	for _, file := range m.files {
		if file.PluginName == pluginName && file.DeletedAt == nil {
			metrics.FileCount++
			metrics.FileBytes += file.Size
			users[file.OwnerID] = true
			metrics.LastUpdated = newestTime(metrics.LastUpdated, &file.CreatedAt)
		}
	}
	metrics.UserCount = len(users)
	return metrics, nil
}

func (m *MemoryMarketStore) UserMetrics(_ context.Context, pluginName, ownerID string) (MarketMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	metrics := MarketMetrics{PluginName: pluginName}
	for _, record := range m.records {
		if record.PluginName == pluginName && record.OwnerType == OwnerUser && record.OwnerID == ownerID {
			metrics.RecordCount++
			metrics.RecordBytes += int64(len(mustJSON(record.Data)))
			metrics.LastUpdated = newestTime(metrics.LastUpdated, &record.UpdatedAt)
		}
	}
	for _, file := range m.files {
		if file.PluginName == pluginName && file.OwnerID == ownerID && file.DeletedAt == nil {
			metrics.FileCount++
			metrics.FileBytes += file.Size
			metrics.LastUpdated = newestTime(metrics.LastUpdated, &file.CreatedAt)
		}
	}
	if metrics.RecordCount > 0 || metrics.FileCount > 0 {
		metrics.UserCount = 1
	}
	return metrics, nil
}

func cloneRecord(record ManagedRecord) ManagedRecord {
	copy := record
	copy.Data, _, _ = normalizedRecordData(DataCollection{}, record.Data)
	if copy.Data == nil {
		copy.Data = map[string]interface{}{}
	}
	return copy
}

func cloneGrant(grant UserGrant) UserGrant {
	copy := grant
	copy.Permissions = append([]string(nil), grant.Permissions...)
	return copy
}

func pageRecords(items []ManagedRecord, page, size int) RecordPage {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	start := (page - 1) * size
	if start >= len(items) {
		return RecordPage{Items: []ManagedRecord{}, Total: len(items), Page: page, Size: size}
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return RecordPage{Items: items[start:end], Total: len(items), Page: page, Size: size}
}
