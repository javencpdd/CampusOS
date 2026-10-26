package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	storage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

type v12BackupObject struct {
	Owner string `json:"owner_id"`
	ID    string `json:"object_id"`
	Hash  string `json:"sha256"`
	Size  int64  `json:"size_bytes"`
}

type v12BackupSecret struct {
	Owner  *int64 `json:"owner_id,omitempty"`
	ID     int64  `json:"secret_id"`
	Name   string `json:"secret_name"`
	Hash   string `json:"plaintext_sha256"`
	Status string `json:"status"`
}

// Only public identities and hashes cross the backup boundary. Neither the
// signing private key nor Secret decryption material is written under ROOT.
type v12BackupFixture struct {
	Schema        string                    `json:"schema"`
	Coordinate    V5Coordinate              `json:"coordinate"`
	PackageDigest string                    `json:"package_digest"`
	Installation  map[string]V5Installation `json:"installations"`
	Configuration map[string]string         `json:"configuration_sha256"`
	Checkpoints   map[string]V5Checkpoint   `json:"checkpoints"`
	Prepared      map[string]string         `json:"prepared_sha256"`
	Private       map[string]string         `json:"private_sha256"`
	GrantHash     string                    `json:"grant_sha256"`
	SecretRows    string                    `json:"secret_rows_sha256"`
	Secrets       []v12BackupSecret         `json:"secrets"`
	Objects       []v12BackupObject         `json:"objects"`
}

func v12BackupHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func v12BackupJSONHash(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal("encode backup fixture hash", err)
	}
	return v12BackupHash(raw)
}

// This is an offline data-recovery test. It deliberately never starts a
// Server, Runner, coordinator or Observe, and does not authorize reopening
// traffic or claim restored Grant facts preserve post-backup revocations.
func TestV12BackupRestoreOffline(t *testing.T) {
	dsn, root, phase, encodedKey := os.Getenv("CAMPUSOS_V12_BACKUP_DSN"), os.Getenv("CAMPUSOS_V12_BACKUP_ROOT"), os.Getenv("CAMPUSOS_V12_BACKUP_PHASE"), os.Getenv("CAMPUSOS_V12_BACKUP_KEY")
	if dsn == "" && root == "" && phase == "" && encodedKey == "" {
		t.Skip("requires owned08b seed/restored PostgreSQL, file root, phase and externally held key")
	}
	key, err := hex.DecodeString(encodedKey)
	if dsn == "" || !filepath.IsAbs(root) || filepath.Clean(root) == "/" || root != filepath.Clean(root) || len(key) != 32 || err != nil || phase != "seed" && phase != "restore" {
		t.Fatal("requires complete owned08b backup environment and 32-byte hexadecimal external key")
	}
	defer clear(key)
	cfg, err := pgxpool.ParseConfig(dsn)
	expectedDB := "campusos_v12_08b_backup_seed"
	if phase == "restore" {
		expectedDB = "campusos_v12_08b_backup_restored"
	}
	if err != nil || cfg.ConnConfig.Database != expectedDB || cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" {
		t.Fatal("requires exact owned08b loopback database for this phase")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("connect owned08b database", err)
	}
	defer pool.Close()
	var database string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil || database != expectedDB {
		t.Fatal("owned08b actual database mismatch")
	}
	configDir := filepath.Join(root, "data", "config")
	fixturePath := filepath.Join(configDir, "v12-backup-fixture.json")
	trustPath := filepath.Join(configDir, "v12-backup-public-trust.json")
	read := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal("read owned backup artifact", err)
		}
		return raw
	}
	writeJSON := func(name string, value any) {
		t.Helper()
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal("encode public backup fixture", err)
		}
		if err := os.WriteFile(name, append(raw, '\n'), 0600); err != nil {
			t.Fatal("write public backup fixture", err)
		}
	}
	var fixture v12BackupFixture
	var trust, archive, envelope []byte
	if phase == "seed" {
		if _, err := os.Stat(fixturePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("seed requires a fresh owned file root")
		}
		if err := os.MkdirAll(configDir, 0700); err != nil {
			t.Fatal(err)
		}
		v := newV5TestPlatform(t)
		v.manifest.Key = "backup-offline"
		v.manifest.Data.PrivateKVBytes = 4096
		v.manifest.Configuration = json.RawMessage(`{"contract":"campusos.plugin-config/v1","version":"v1","schema":{"type":"object","additionalProperties":false,"properties":{"locale":{"type":"string","title":"Locale","enum":["en-US","zh-CN"]}},"required":["locale"]},"defaults":{"locale":"en-US"},"selectors":[],"secret_names":[]}`)
		raw, err := json.Marshal(v.manifest)
		if err != nil || os.WriteFile(filepath.Join(v.dir, PluginV5ManifestFile), raw, 0600) != nil {
			t.Fatal("write owned package manifest")
		}
		var inspected *PluginV5Package
		archive, inspected, err = BuildPluginV5Package(v.dir)
		if err != nil {
			t.Fatal("build owned backup package", err)
		}
		privateKey, publicTrust := testV5Trust(t)
		trust = publicTrust
		envelope, err = SignPluginV5Package(archive, "test-key", privateKey)
		if err != nil {
			t.Fatal("sign owned backup package", err)
		}
		clear(privateKey)
		fixture = v12BackupFixture{Schema: "campusos.v12-08b-backup-fixture/v1", Coordinate: V5Coordinate{inspected.Manifest.Publisher, inspected.Manifest.Key, inspected.Manifest.Version}, PackageDigest: inspected.Digest, Installation: map[string]V5Installation{}, Configuration: map[string]string{}, Checkpoints: map[string]V5Checkpoint{}, Prepared: map[string]string{}, Private: map[string]string{}}
		if err := os.WriteFile(trustPath, trust, 0600); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := json.Unmarshal(read(fixturePath), &fixture); err != nil || fixture.Schema != "campusos.v12-08b-backup-fixture/v1" || !fixture.Coordinate.valid() {
			t.Fatal("invalid restored public fixture")
		}
		trust = read(trustPath)
	}
	files, err := NewV5ManagedFiles(filepath.Join(root, "data", "plugin_data", "v5"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	catalog, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	platform := NewV5Platform(NewPgV5StateStore(pool), files, catalog, reliable, nil)
	platform.Users = v5ChatPGUsers{repository.NewPgUserRepository(pool)}
	c := fixture.Coordinate
	owners := []string{"2801001", "2801002"}
	if phase == "seed" {
		admins, admin, _ := v5PGAdmin(t, ctx, pool)
		platform.admins = admins
		if _, err := platform.Import(ctx, archive, envelope, "backup-import", admin); err != nil {
			t.Fatal("real PG import", err)
		}
		mutate := func(owner string, request V5Mutation, proof identity.SessionPrincipalResolver) {
			t.Helper()
			request.Version = c.Version
			request.IdempotencyKey = "backup-" + request.Kind + "-" + v5OwnerName(owner)
			if _, err := platform.Mutate(ctx, c.Publisher, c.Key, request, owner == "", proof); err != nil {
				t.Fatal("real PG backup mutation", request.Kind, owner, err)
			}
		}
		mutate("", V5Mutation{Kind: "install"}, admin)
		mutate("", V5Mutation{Kind: "grant", Approved: true, Consumes: []PluginV5Consume{}}, admin)
		proofs := map[string]identity.SessionPrincipalResolver{"": admin}
		for _, owner := range owners {
			_, proof, _ := v5ChatPGSessionFor(t, ctx, pool, reliable, owner)
			proofs[owner] = proof
			mutate(owner, V5Mutation{Kind: "install"}, proof)
		}
		for _, owner := range append([]string{""}, owners...) {
			mutate(owner, V5Mutation{Kind: "configure", Values: json.RawMessage(`{"locale":"zh-CN"}`)}, proofs[owner])
			i, err := platform.installation(ctx, c, owner)
			if err != nil || i.Desired != "disabled" || i.ActiveGeneration != 0 {
				t.Fatal("backup seed must stay disabled and inactive", err)
			}
			fixture.Installation[owner] = i
			cfg, err := platform.configuration(ctx, c, owner, nil)
			if err != nil {
				t.Fatal(err)
			}
			fixture.Configuration[owner] = v12BackupJSONHash(t, cfg)
			view, err := files.dataView(i, 0)
			if err != nil {
				t.Fatal(err)
			}
			payload := []byte("owned08b-private-" + v5OwnerName(owner))
			if err := view.WriteFile("state.bin", payload, 0600); err != nil {
				t.Fatal(err)
			}
			view.Close()
			fixture.Private[owner] = v12BackupHash(payload)
			fixture.Prepared[owner], err = files.Prepare(c.Publisher, c.Key, owner, i.Generation, []byte(`{"offline_backup_fixture":true}`))
			if err != nil {
				t.Fatal(err)
			}
			fixture.Checkpoints[owner], err = platform.captureCheckpoint(ctx, i, v5RandomID())
			if err != nil {
				t.Fatal("capture actual managed checkpoint", err)
			}
			// Materialize an independent recovery view without switching any
			// durable installation pointer or claiming a running instance.
			release, err := platform.release(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			m, err := ParsePluginV5Manifest(release.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := platform.restoreCheckpoint(ctx, &i, V5Operation{Restore: fixture.Checkpoints[owner]}, m); err != nil {
				t.Fatal("materialize actual independent restore view", err)
			}
		}
	}
	var release V5PackageRelease
	if err := platform.store.load(ctx, v5Release, c.id(), &release); err != nil || release.Digest != fixture.PackageDigest {
		t.Fatal("package release did not survive", err)
	}
	archive, err = files.Package(release.Digest)
	if err != nil {
		t.Fatal("restored immutable package", err)
	}
	verified, err := VerifyPluginV5Release(archive, release.Release, trust)
	if err != nil || verified.Digest != fixture.PackageDigest || verified.Manifest.Key != c.Key {
		t.Fatal("restored signature/package identity mismatch", err)
	}
	var identityDigest string
	if err := pool.QueryRow(ctx, `SELECT package_digest FROM plugin_package_identities WHERE publisher=$1 AND plugin_key=$2 AND version=$3`, c.Publisher, c.Key, c.Version).Scan(&identityDigest); err != nil || identityDigest != fixture.PackageDigest {
		t.Fatal("restored permanent PG package identity mismatch")
	}
	grant, err := platform.grant(ctx, c)
	if err != nil || !grant.Approved {
		t.Fatal("offline grant fact missing", err)
	}
	if phase == "seed" {
		fixture.GrantHash = v12BackupJSONHash(t, grant)
	} else if v12BackupJSONHash(t, grant) != fixture.GrantHash {
		t.Fatal("offline restored grant fact changed")
	}
	for _, owner := range append([]string{""}, owners...) {
		i, err := platform.installation(ctx, c, owner)
		if err != nil || !reflect.DeepEqual(i, fixture.Installation[owner]) || i.Desired != "disabled" || i.ActiveGeneration != 0 {
			t.Fatal("offline restored installation mismatch", owner, err)
		}
		cfg, err := platform.configuration(ctx, c, owner, verified.Manifest)
		if err != nil || v12BackupJSONHash(t, cfg) != fixture.Configuration[owner] {
			t.Fatal("offline restored configuration mismatch", owner, err)
		}
		if err := files.VerifyPrepared(c.Publisher, c.Key, owner, i.Generation, fixture.Prepared[owner]); err != nil {
			t.Fatal("restored prepared file mismatch", owner, err)
		}
		body, err := platform.checkpoint(ctx, i, fixture.Checkpoints[owner])
		if err != nil || v12BackupHash(body.Files["state.bin"]) != fixture.Private[owner] {
			t.Fatal("restored checkpoint identity/private bytes mismatch", owner, err)
		}
		for _, generation := range []int64{0, i.Generation} {
			view, err := files.dataView(i, generation)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := view.ReadFile("state.bin")
			view.Close()
			if err != nil || v12BackupHash(raw) != fixture.Private[owner] {
				t.Fatal("restored private/orphan recovery view bytes mismatch", owner, err)
			}
		}
	}
	quotas := storage.NewPgQuotaRepository(pool)
	adapter, err := storage.NewLocalAdapterWithQuotaRepository(filepath.Join(root, "data", "personal-space"), 1<<20, quotas)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := storage.NewObjectService(adapter, adapter, storage.NewPgObjectRepository(pool), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	secretStore := NewPgAuthorizationStore(pool)
	secrets, err := NewSecretService(secretStore, key, "owned08b-v1")
	if err != nil {
		t.Fatal(err)
	}
	const secretPluginID int64 = 8808001
	if phase == "seed" {
		if _, err := pool.Exec(ctx, `INSERT INTO plugins(id,name,display_name,version,runtime) VALUES($1,'v12-08b-secret-fixture','Offline backup Secret','1.0.0','grpc')`, secretPluginID); err != nil {
			t.Fatal(err)
		}
		for _, owner := range owners {
			if _, err := quotas.Upsert(ctx, storage.QuotaRecord{UserID: owner, QuotaBytes: 1 << 20}); err != nil {
				t.Fatal(err)
			}
			payload := []byte("owned08b-object-" + owner)
			object, err := objects.Put(ctx, owner, storage.PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "backup.txt", MimeType: "text/plain", SizeHint: int64(len(payload)), Reader: bytes.NewReader(payload)})
			if err != nil {
				t.Fatal("seed actual ObjectPort", err)
			}
			fixture.Objects = append(fixture.Objects, v12BackupObject{owner, object.ID, object.SHA256, object.SizeBytes})
		}
		for _, owner := range append([]string{""}, owners...) {
			var ownerID *int64
			if owner != "" {
				id, _ := strconv.ParseInt(owner, 10, 64)
				ownerID = &id
			}
			name := "backup." + v5OwnerName(owner)
			put := func(name, status string) {
				t.Helper()
				random := make([]byte, 32)
				if _, err := rand.Read(random); err != nil {
					t.Fatal(err)
				}
				value := hex.EncodeToString(random)
				meta, err := secrets.Put(ctx, secretPluginID, ownerID, name, value, nil)
				if err != nil {
					t.Fatal("seed actual encrypted Secret", err)
				}
				fixture.Secrets = append(fixture.Secrets, v12BackupSecret{ownerID, meta.ID, name, v12BackupHash([]byte(value)), status})
			}
			if owner == "" {
				put(name, "rotated")
			}
			put(name, "active")
			if owner == owners[0] {
				put(name+".revoked", "revoked")
				if err := secrets.Revoke(ctx, secretPluginID, ownerID, name+".revoked"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if len(fixture.Objects) != 2 || len(fixture.Secrets) != 5 || len(fixture.Installation) != 3 {
		t.Fatal("offline backup fixture coverage incomplete")
	}
	for _, object := range fixture.Objects {
		reader, err := objects.Open(ctx, object.Owner, object.ID)
		if err != nil {
			t.Fatal("restored owner ObjectPort open", err)
		}
		raw, err := io.ReadAll(io.LimitReader(reader.Reader, object.Size+1))
		reader.Reader.Close()
		if err != nil || int64(len(raw)) != object.Size || v12BackupHash(raw) != object.Hash || reader.Object.SHA256 != object.Hash {
			t.Fatal("restored Object metadata/physical bytes mismatch")
		}
		other := owners[0]
		if object.Owner == other {
			other = owners[1]
		}
		if denied, err := objects.Open(ctx, other, object.ID); err == nil {
			denied.Reader.Close()
			t.Fatal("restored Object crossed owner boundary")
		}
	}
	for _, name := range []string{"CAMPUSOS_SECRET_ACTIVE_KEY_ID", "CAMPUSOS_SECRET_ENCRYPTION_KEYS", "CAMPUSOS_PLUGIN_SECRET_KEY", "CAMPUSOS_PLUGIN_SECRET_KEY_VERSION"} {
		t.Setenv(name, "")
	}
	if _, err := NewSecretServiceFromEnv(secretStore); err == nil {
		t.Fatal("restored Secret accepted missing external deployment key")
	}
	wrongKey := append([]byte(nil), key...)
	wrongKey[0] ^= 0xff
	wrongSecrets, err := NewSecretService(secretStore, wrongKey, "owned08b-v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range fixture.Secrets {
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM plugin_secret_values WHERE id=$1`, secret.ID).Scan(&status); err != nil || status != secret.Status {
			t.Fatal("restored Secret history/revocation identity mismatch")
		}
		if secret.Status != "active" {
			continue
		}
		value, err := secrets.Resolve(ctx, secretPluginID, secret.Owner, secret.Name)
		if err != nil || v12BackupHash([]byte(value)) != secret.Hash {
			t.Fatal("correct externally held key did not recover exact Secret")
		}
		if value, err := wrongSecrets.Resolve(ctx, secretPluginID, secret.Owner, secret.Name); err == nil || value != "" {
			t.Fatal("restored Secret accepted wrong key")
		}
		otherID := int64(2801001)
		if secret.Owner != nil && *secret.Owner == otherID {
			otherID = 2801002
		}
		if value, err := secrets.Resolve(ctx, secretPluginID, &otherID, secret.Name); err == nil || value != "" {
			t.Fatal("restored Secret crossed owner boundary")
		}
	}
	var secretRows []byte
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(s) ORDER BY id)::text FROM plugin_secret_values s WHERE plugin_id=$1`, secretPluginID).Scan(&secretRows); err != nil {
		t.Fatal("hash restored encrypted Secret rows", err)
	}
	if phase == "seed" {
		fixture.SecretRows = v12BackupHash(secretRows)
		writeJSON(fixturePath, fixture)
		writeJSON(filepath.Join(configDir, "v12-backup-coordinate.json"), c)
	} else if v12BackupHash(secretRows) != fixture.SecretRows {
		t.Fatal("restored Secret ciphertext/history rows changed")
	}
	proof, err := json.Marshal(map[string]any{"schema": "campusos.v12-08b-backup-offline-proof/v1", "phase": phase, "database": expectedDB, "package_digest": fixture.PackageDigest, "installations": len(fixture.Installation), "configurations": len(fixture.Configuration), "prepared_files": len(fixture.Prepared), "checkpoints": len(fixture.Checkpoints), "original_and_independent_views": 2 * len(fixture.Private), "objects": len(fixture.Objects), "secret_rows": len(fixture.Secrets), "secret_rows_sha256": fixture.SecretRows, "correct_external_key": "passed", "missing_external_key": "denied", "wrong_external_key": "denied", "cross_owner_object": "denied", "cross_owner_secret": "denied", "server_started": false, "runtime_started": false, "observe_called": false, "authentication_generation_rotated": false, "safe_to_reopen_traffic": false, "status": "passed"})
	if err != nil || strings.Contains(string(proof), encodedKey) {
		t.Fatal("unsafe offline proof encoding")
	}
	fmt.Printf("V12_BACKUP_PROOF %s\n", proof)
}
