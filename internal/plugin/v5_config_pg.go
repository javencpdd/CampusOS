package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgPluginV5ConfigStore is a host-only repository. The selector verifier must
// read current host facts, rather than values supplied by a plugin request.
// Normal values and opaque Secret references are persisted in separate JSONB
// columns; this store never reads or writes plaintext Secret payloads.
type PgPluginV5ConfigStore struct {
	pool           *pgxpool.Pool
	verifySelector PluginV5SelectorVerifier
}

func NewPgPluginV5ConfigStore(pool *pgxpool.Pool, verifySelector PluginV5SelectorVerifier) *PgPluginV5ConfigStore {
	return &PgPluginV5ConfigStore{pool: pool, verifySelector: verifySelector}
}

func pluginV5ConfigOwnerValid(ownerID *int64) bool { return ownerID == nil || *ownerID > 0 }

func pluginV5ConfigActorValid(actor string) bool {
	if actor == "" || len(actor) > 64 || actor != strings.TrimSpace(actor) || !utf8.ValidString(actor) {
		return false
	}
	for _, character := range actor {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func activePluginV5ConfigDefinition(ctx context.Context, tx pgx.Tx, versionID int64, lock bool) (pluginV5ConfigDefinition, error) {
	statement := `SELECT pv.manifest FROM plugin_versions pv JOIN plugins p ON p.id=pv.plugin_id
		WHERE pv.id=$1 AND pv.lifecycle_status='active' AND p.deleted_at IS NULL`
	if lock {
		statement += ` FOR SHARE OF pv`
	}
	var manifest []byte
	if err := tx.QueryRow(ctx, statement, versionID).Scan(&manifest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pluginV5ConfigDefinition{}, ErrPluginV5ConfigInvalid
		}
		return pluginV5ConfigDefinition{}, ErrPluginV5ConfigUnavailable
	}
	return parsePluginV5ConfigDefinition(manifest)
}

func validatePluginV5ConfigDefaultsAtUse(ctx context.Context, definition pluginV5ConfigDefinition, versionID int64, ownerID *int64, verify PluginV5SelectorVerifier) error {
	if err := validatePluginV5Selectors(ctx, definition, definition.Defaults, versionID, ownerID, verify); err != nil {
		return ErrPluginV5ConfigDefaultsInvalid
	}
	return nil
}

// SavePluginV5Config compares expected_revision inside a transaction, then
// writes both columns and a value-free command audit atomically. A first write
// compares against the virtual defaults revision 1 and returns revision 2.
func (s *PgPluginV5ConfigStore) SavePluginV5Config(ctx context.Context, versionID int64, ownerID *int64, update PluginV5ConfigUpdate, actor string) (PluginV5Configuration, error) {
	if s == nil || s.pool == nil || versionID <= 0 || !pluginV5ConfigOwnerValid(ownerID) ||
		update.ExpectedRevision < 1 || !pluginV5ConfigActorValid(actor) {
		return PluginV5Configuration{}, ErrPluginV5ConfigInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PluginV5Configuration{}, ErrPluginV5ConfigUnavailable
	}
	defer tx.Rollback(ctx)
	definition, err := activePluginV5ConfigDefinition(ctx, tx, versionID, true)
	if err != nil {
		return PluginV5Configuration{}, err
	}
	if err := validatePluginV5ConfigDefaultsAtUse(ctx, definition, versionID, ownerID, s.verifySelector); err != nil {
		return PluginV5Configuration{}, err
	}
	// Serialize first insert and later updates for the same nullable owner.
	lockKey := fmt.Sprintf("v5-config:%d:system", versionID)
	if ownerID != nil {
		lockKey = fmt.Sprintf("v5-config:%d:user:%d", versionID, *ownerID)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return PluginV5Configuration{}, ErrPluginV5ConfigUnavailable
	}
	var rowID int64
	var currentRevision int64
	var currentDefinitionVersion string
	err = tx.QueryRow(ctx, `SELECT id,revision,definition_version FROM plugin_configurations
		WHERE plugin_version_id=$1 AND owner_user_id IS NOT DISTINCT FROM $2::bigint FOR UPDATE`, versionID, ownerID).Scan(&rowID, &currentRevision, &currentDefinitionVersion)
	isNew := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !isNew {
		return PluginV5Configuration{}, ErrPluginV5ConfigUnavailable
	}
	if isNew {
		currentRevision = 1
	} else if currentDefinitionVersion != definition.Version {
		return PluginV5Configuration{}, ErrPluginV5ConfigSchemaUnsupported
	}
	if update.ExpectedRevision != currentRevision {
		return PluginV5Configuration{}, ErrPluginV5ConfigRevisionConflict
	}
	if err := validatePluginV5Values(definition, update.Values); err != nil {
		return PluginV5Configuration{}, err
	}
	if err := validatePluginV5SecretRefs(definition, update.SecretRefs); err != nil {
		return PluginV5Configuration{}, err
	}
	if err := validatePluginV5Selectors(ctx, definition, update.Values, versionID, ownerID, s.verifySelector); err != nil {
		return PluginV5Configuration{}, err
	}
	refsJSON, err := json.Marshal(update.SecretRefs)
	if err != nil {
		return PluginV5Configuration{}, ErrPluginV5SecretRefInvalid
	}
	nextRevision := currentRevision + 1
	if isNew {
		_, err = tx.Exec(ctx, `INSERT INTO plugin_configurations
			(plugin_version_id,owner_user_id,definition_version,revision,values,secret_refs)
			VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb)`, versionID, ownerID, definition.Version, nextRevision, update.Values, refsJSON)
	} else {
		var updatedRevision int64
		err = tx.QueryRow(ctx, `UPDATE plugin_configurations SET revision=revision+1,values=$3::jsonb,
			secret_refs=$4::jsonb,updated_at=NOW() WHERE id=$1 AND revision=$2 RETURNING revision`,
			rowID, currentRevision, update.Values, refsJSON).Scan(&updatedRevision)
		if errors.Is(err, pgx.ErrNoRows) {
			return PluginV5Configuration{}, ErrPluginV5ConfigRevisionConflict
		}
	}
	if err != nil {
		return PluginV5Configuration{}, ErrPluginV5ConfigUnavailable
	}
	// The command audit is part of the same commit and contains no config
	// values, Secret names, refs, ciphertext or provider credentials.
	ownerScope := "system"
	if ownerID != nil {
		ownerScope = "user"
	}
	details, _ := json.Marshal(map[string]any{
		"owner_scope": ownerScope, "owner_user_id": ownerID, "previous_revision": currentRevision,
		"revision": nextRevision, "secret_ref_count": len(update.SecretRefs),
	})
	_, err = tx.Exec(ctx, `INSERT INTO platform_command_audits
		(id,command_id,command_code,actor_id,actor_type,resource_type,resource_id,details)
		VALUES ($1,$2,'plugin.config.update',$3,'operator','plugin_version',$4,$5::jsonb)`,
		fmt.Sprint(idgen.New()), fmt.Sprint(idgen.New()), actor, fmt.Sprint(versionID), details)
	if err != nil {
		return PluginV5Configuration{}, ErrPluginV5ConfigUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return PluginV5Configuration{}, ErrPluginV5ConfigUnavailable
	}
	return PluginV5Configuration{PluginVersionID: versionID, OwnerUserID: ownerID,
		DefinitionVersion: definition.Version, Revision: nextRevision,
		Values: append(json.RawMessage(nil), update.Values...), SecretRefs: clonePluginV5SecretRefs(update.SecretRefs)}, nil
}

func clonePluginV5SecretRefs(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for name, ref := range source {
		result[name] = ref
	}
	return result
}

// ReadPluginV5Config verifies the active release, immutable definition and
// current selector visibility on every read. Missing config resolves to the
// validated defaults at virtual revision 1, never to an older plugin version.
func (s *PgPluginV5ConfigStore) ReadPluginV5Config(ctx context.Context, versionID int64, ownerID *int64) (PluginV5Configuration, error) {
	if s == nil || s.pool == nil || versionID <= 0 || !pluginV5ConfigOwnerValid(ownerID) {
		return PluginV5Configuration{}, ErrPluginV5ConfigInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PluginV5Configuration{}, ErrPluginV5ConfigUnavailable
	}
	defer tx.Rollback(ctx)
	definition, err := activePluginV5ConfigDefinition(ctx, tx, versionID, false)
	if err != nil {
		return PluginV5Configuration{}, err
	}
	result := PluginV5Configuration{PluginVersionID: versionID, OwnerUserID: ownerID,
		DefinitionVersion: definition.Version, Revision: 1, Values: append(json.RawMessage(nil), definition.Defaults...),
		SecretRefs: map[string]string{}}
	var values, refs []byte
	err = tx.QueryRow(ctx, `SELECT definition_version,revision,values,secret_refs FROM plugin_configurations
		WHERE plugin_version_id=$1 AND owner_user_id IS NOT DISTINCT FROM $2::bigint`, versionID, ownerID).Scan(
		&result.DefinitionVersion, &result.Revision, &values, &refs)
	if err == nil {
		if result.DefinitionVersion != definition.Version || result.Revision < 1 ||
			validatePluginV5Values(definition, values) != nil || json.Unmarshal(refs, &result.SecretRefs) != nil ||
			validatePluginV5SecretRefs(definition, result.SecretRefs) != nil {
			return PluginV5Configuration{}, ErrPluginV5ConfigInvalid
		}
		result.Values = values
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return PluginV5Configuration{}, ErrPluginV5ConfigUnavailable
	}
	if err := validatePluginV5ConfigDefaultsAtUse(ctx, definition, versionID, ownerID, s.verifySelector); err != nil {
		return PluginV5Configuration{}, err
	}
	if err := validatePluginV5Selectors(ctx, definition, result.Values, versionID, ownerID, s.verifySelector); err != nil {
		return PluginV5Configuration{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PluginV5Configuration{}, ErrPluginV5ConfigUnavailable
	}
	return result, nil
}

// IsBoundPluginV5SecretRef proves only exact active-version configuration
// binding. Callers must independently authorize the plugin, owner, purpose and
// target before using the host-managed Secret via a Broker.
func (s *PgPluginV5ConfigStore) IsBoundPluginV5SecretRef(ctx context.Context, versionID int64, ownerID *int64, secretName, ref string) (bool, error) {
	if !pluginV5ConfigNamePattern.MatchString(secretName) || !pluginV5SecretRefPattern.MatchString(ref) {
		return false, nil
	}
	configuration, err := s.ReadPluginV5Config(ctx, versionID, ownerID)
	if err != nil {
		return false, err
	}
	return configuration.SecretRefs[secretName] == ref, nil
}
