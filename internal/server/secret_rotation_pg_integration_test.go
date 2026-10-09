package server

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresSecretRotationServerLifecycle(t *testing.T) {
	if os.Getenv("CAMPUSOS_SECRET_ROTATION_TEST_ISOLATED") != "1" {
		t.Skip("requires owned rotation database")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, os.Getenv("CAMPUSOS_PG_INTEGRATION_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var database string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil || !strings.HasPrefix(database, "campusos_v12_01b_rotation") {
		t.Fatal("refusing non-isolated database")
	}
	const id int64 = 99261099
	if _, err := pool.Exec(ctx, `INSERT INTO plugins(id,name,display_name,version,runtime) VALUES($1,'rotation-server','Server rotation','1.0.0','grpc')`, id); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM plugins WHERE id=$1`, id) }()
	store := plugin.NewPgAuthorizationStore(pool)
	oldKey, newKey := []byte("0123456789abcdef0123456789abcdef"), []byte("abcdef0123456789abcdef0123456789")
	old, err := plugin.NewSecretService(store, oldKey, "old-v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Put(ctx, id, nil, "server.token", "server-value", nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAMPUSOS_SECRET_ROTATION_ENABLED", "true")
	t.Setenv("CAMPUSOS_SECRET_ROTATION_INTERVAL", "1s")
	t.Setenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID", "new-v2")
	t.Setenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS", "old-v1:"+hex.EncodeToString(oldKey)+",new-v2:"+hex.EncodeToString(newKey))
	stop, err := startSecretRotation(&infrastructureBootstrap{database: pool})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	row, err := store.ActiveSecret(ctx, id, nil, "server.token")
	if err != nil || row.KeyVersion != "old-v1" {
		t.Fatal("startup implicitly rewrote row before first tick")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		row, err = store.ActiveSecret(ctx, id, nil, "server.token")
		if err == nil && row.KeyVersion == "new-v2" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("periodic server worker did not rotate owned fixture")
}
