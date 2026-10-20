package plugin

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	storage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestV12RecoveryRestoredSecretsPackagesAndObjects(t *testing.T) {
	dsn, dataRoot, encodedKey := os.Getenv("CAMPUSOS_V12_BACKUP_DSN"), os.Getenv("CAMPUSOS_RECOVERY_DATA_ROOT"), os.Getenv("CAMPUSOS_V12_BACKUP_KEY")
	if dsn == "" && dataRoot == "" && encodedKey == "" {
		t.Skip("requires owned08b recovered data and externally held key")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || cfg.ConnConfig.Database != "campusos_v12_08b_backup_restored" || cfg.ConnConfig.Host != "127.0.0.1" || !filepath.IsAbs(dataRoot) {
		t.Fatal("requires exact owned recovered database and data root")
	}
	key, e := hex.DecodeString(encodedKey)
	if e != nil || len(key) != 32 {
		t.Fatal("requires independent external key")
	}
	defer clear(key)
	ctx := context.Background()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	var fixture v12BackupFixture
	raw, e := os.ReadFile(filepath.Join(dataRoot, "config/v12-backup-fixture.json"))
	if e != nil || json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("restored fixture metadata")
	}
	var cipherRows []byte
	if e = pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(s) ORDER BY id)::text FROM plugin_secret_values s WHERE plugin_id=8808001`).Scan(&cipherRows); e != nil || v12BackupHash(cipherRows) != fixture.SecretRows {
		t.Fatal("Secret ciphertext/history lost", e)
	}
	store := NewPgAuthorizationStore(pool)
	correct, e := NewSecretService(store, key, "owned08b-v1")
	if e != nil {
		t.Fatal(e)
	}
	wrongKey := append([]byte(nil), key...)
	wrongKey[0] ^= 0xff
	wrong, e := NewSecretService(store, wrongKey, "owned08b-v1")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"CAMPUSOS_SECRET_ACTIVE_KEY_ID", "CAMPUSOS_SECRET_ENCRYPTION_KEYS", "CAMPUSOS_PLUGIN_SECRET_KEY", "CAMPUSOS_PLUGIN_SECRET_KEY_VERSION"} {
		t.Setenv(name, "")
	}
	if _, e = NewSecretServiceFromEnv(store); e == nil {
		t.Fatal("restored secrets accepted missing key")
	}
	for _, secret := range fixture.Secrets {
		if secret.Status != "active" {
			continue
		}
		value, e := correct.Resolve(ctx, 8808001, secret.Owner, secret.Name)
		if e != nil || v12BackupHash([]byte(value)) != secret.Hash {
			t.Fatal("independent key did not recover exact Secret")
		}
		if _, e = wrong.Resolve(ctx, 8808001, secret.Owner, secret.Name); e == nil {
			t.Fatal("wrong key accepted")
		}
		other := int64(2801001)
		if secret.Owner != nil && *secret.Owner == other {
			other = 2801002
		}
		if _, e = correct.Resolve(ctx, 8808001, &other, secret.Name); e == nil {
			t.Fatal("Secret crossed owner")
		}
	}
	adapter, e := storage.NewLocalAdapterWithQuotaRepository(filepath.Join(dataRoot, "personal-space"), 1<<20, storage.NewPgQuotaRepository(pool))
	if e != nil {
		t.Fatal(e)
	}
	objects, e := storage.NewObjectService(adapter, adapter, storage.NewPgObjectRepository(pool), 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	for _, object := range fixture.Objects {
		reader, e := objects.Open(ctx, object.Owner, object.ID)
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(io.LimitReader(reader.Reader, object.Size+1))
		reader.Reader.Close()
		if e != nil || v12BackupHash(body) != object.Hash {
			t.Fatal("restored Object bytes mismatch")
		}
	}
	files, e := NewV5ManagedFiles(filepath.Join(dataRoot, "plugin_data/v5"))
	if e != nil {
		t.Fatal(e)
	}
	defer files.Close()
	archive, e := files.Package(fixture.PackageDigest)
	if e != nil {
		t.Fatal(e)
	}
	var envelope []byte
	if e = pool.QueryRow(ctx, `SELECT (state->'release')::text FROM plugin_package_releases WHERE record_id=$1`, fixture.Coordinate.id()).Scan(&envelope); e != nil {
		t.Fatal(e)
	}
	trust, e := os.ReadFile(filepath.Join(dataRoot, "config/v12-backup-public-trust.json"))
	if e != nil {
		t.Fatal(e)
	}
	if verified, e := VerifyPluginV5Release(archive, envelope, trust); e != nil || verified.Digest != fixture.PackageDigest {
		t.Fatal("restored signed package identity mismatch")
	}
	var active int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM plugin_v5_admin_grants WHERE (state->>'approved')::boolean)+(SELECT count(*) FROM plugin_v5_user_consents WHERE (state->>'accepted')::boolean)+(SELECT count(*) FROM plugin_installations WHERE state->>'desired'='enabled')`).Scan(&active); e != nil || active != 0 {
		t.Fatal("recovered plugin authority revived")
	}
	t.Log("V12_RESTORED_FACTS signed package; 5 unchanged encrypted history rows; correct external key; missing/wrong/cross-owner denied; exact Object bytes; plugin authority quarantined")
}
