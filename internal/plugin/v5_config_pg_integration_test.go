package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	v5ConfigFixturePluginID  int64 = 990007101
	v5ConfigFixtureVersionID int64 = 990007102
	v5ConfigFixtureOwnerID   int64 = 990007103
	v5ConfigFixtureOtherID   int64 = 990007104
	v5ConfigFixtureStagedID  int64 = 990007105
)

func TestPostgresPluginV5ConfigRepositoryRestore(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_PG_INTEGRATION_DSN")
	if os.Getenv("CAMPUSOS_V5_CONFIG_TEST_ISOLATED") != "1" ||
		!strings.Contains(dsn, "campusos_v12_01b_config") {
		t.Skip("requires owned V12-01b config PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	visible := func(_ context.Context, _ int64, _ *int64, kind, value string) (bool, error) {
		return kind == "profile" && value == "public", nil
	}
	store := NewPgPluginV5ConfigStore(pool, visible)
	owner := v5ConfigFixtureOwnerID
	if os.Getenv("CAMPUSOS_V5_CONFIG_TEST_PHASE") == "restore" {
		config, err := store.ReadPluginV5Config(ctx, v5ConfigFixtureVersionID, &owner)
		if err != nil || config.Revision != 3 || config.SecretRefs["token"] != "secret-ref:approved-1" {
			t.Fatalf("restored configuration is unavailable: %+v %v", config, err)
		}
		bound, err := store.IsBoundPluginV5SecretRef(ctx, v5ConfigFixtureVersionID, &owner, "token", "secret-ref:approved-1")
		if err != nil || !bound {
			t.Fatalf("restored binding unavailable: bound=%v err=%v", bound, err)
		}
		assertPluginV5ConfigAuditOwners(t, pool)
		return
	}
	if os.Getenv("CAMPUSOS_V5_CONFIG_TEST_PHASE") != "seed" {
		t.Fatal("set CAMPUSOS_V5_CONFIG_TEST_PHASE=seed|restore")
	}
	for _, user := range []struct {
		id    int64
		name  string
		email string
	}{
		{v5ConfigFixtureOwnerID, "v12_config_owner", "v12-config-owner@example.invalid"},
		{v5ConfigFixtureOtherID, "v12_config_other", "v12-config-other@example.invalid"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES($1,$2,$2,$3)`, user.id, user.name, user.email); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO plugins(id,name,display_name,version,runtime)
		VALUES($1,'v12-01b-v5-config','V12 V5 Config','1.0.0','grpc')`, v5ConfigFixturePluginID); err != nil {
		t.Fatal(err)
	}
	for _, version := range []struct {
		id      int64
		status  string
		version string
		digest  string
	}{
		{v5ConfigFixtureVersionID, "active", "1.0.0", strings.Repeat("a", 64)},
		{v5ConfigFixtureStagedID, "staged", "0.9.0", strings.Repeat("b", 64)},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO plugin_versions
			(id,plugin_id,version,package_digest,manifest_api_version,host_api_version,
			 permission_fingerprint,manifest,lifecycle_status,activated_at)
			VALUES($1,$2,$3,$4,'campusos.plugin/v5','v5',$5,$6::jsonb,$7,
			 CASE WHEN $7::varchar='active' THEN NOW() ELSE NULL END)`,
			version.id, v5ConfigFixturePluginID, version.version, version.digest,
			strings.Repeat("c", 64), testPluginV5Manifest(), version.status); err != nil {
			t.Fatal(err)
		}
	}
	config, err := store.ReadPluginV5Config(ctx, v5ConfigFixtureVersionID, &owner)
	if err != nil || config.Revision != 1 || len(config.SecretRefs) != 0 {
		t.Fatalf("default configuration incorrect: %+v %v", config, err)
	}
	if _, err := NewPgPluginV5ConfigStore(pool, nil).ReadPluginV5Config(ctx, v5ConfigFixtureVersionID, &owner); !errors.Is(err, ErrPluginV5ConfigDefaultsInvalid) {
		t.Fatalf("selector without host verifier was accepted: %v", err)
	}
	update := PluginV5ConfigUpdate{
		ExpectedRevision: 1,
		Values:           json.RawMessage(`{"locale":"en-US","profile_id":"public","max_results":20}`),
		SecretRefs:       map[string]string{"token": "secret-ref:approved-1", "backup": "secret-ref:approved-2"},
	}
	config, err = store.SavePluginV5Config(ctx, v5ConfigFixtureVersionID, &owner, update, "admin:config-fixture")
	if err != nil || config.Revision != 2 {
		t.Fatalf("save failed: %+v %v", config, err)
	}
	systemUpdate := update
	systemUpdate.SecretRefs = map[string]string{"token": "secret-ref:system-1"}
	if systemConfig, err := store.SavePluginV5Config(ctx, v5ConfigFixtureVersionID, nil, systemUpdate, "admin:config-fixture"); err != nil || systemConfig.Revision != 2 {
		t.Fatalf("system save failed: %+v %v", systemConfig, err)
	}
	for _, testCase := range []struct {
		name  string
		owner *int64
		ref   string
		want  bool
	}{
		{"exact", &owner, "secret-ref:approved-1", true},
		{"wrong-ref", &owner, "secret-ref:approved-2", false},
		{"wrong-owner", ptrV5ConfigID(v5ConfigFixtureOtherID), "secret-ref:approved-1", false},
		{"system", nil, "secret-ref:approved-1", false},
	} {
		bound, err := store.IsBoundPluginV5SecretRef(ctx, v5ConfigFixtureVersionID, testCase.owner, "token", testCase.ref)
		if err != nil || bound != testCase.want {
			t.Fatalf("%s binding: bound=%v err=%v", testCase.name, bound, err)
		}
	}
	if bound, err := store.IsBoundPluginV5SecretRef(ctx, v5ConfigFixtureVersionID, &owner, "backup", "secret-ref:approved-1"); err != nil || bound {
		t.Fatalf("cross-name binding accepted: %v %v", bound, err)
	}
	if _, err := store.IsBoundPluginV5SecretRef(ctx, v5ConfigFixtureStagedID, &owner, "token", "secret-ref:approved-1"); !errors.Is(err, ErrPluginV5ConfigInvalid) {
		t.Fatalf("staged version accepted: %v", err)
	}
	if _, err := store.SavePluginV5Config(ctx, v5ConfigFixtureVersionID, &owner, update, "admin:config-fixture"); !errors.Is(err, ErrPluginV5ConfigRevisionConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	duplicate := update
	duplicate.ExpectedRevision = 2
	duplicate.SecretRefs = map[string]string{"token": "secret-ref:approved-1", "backup": "secret-ref:approved-1"}
	if _, err := store.SavePluginV5Config(ctx, v5ConfigFixtureVersionID, &owner, duplicate, "admin:config-fixture"); !errors.Is(err, ErrPluginV5SecretRefInvalid) {
		t.Fatalf("duplicate ref accepted: %v", err)
	}
	for _, invalid := range []string{
		`{"token":"plain-provider-secret"}`,
		`{"token":"secret-ref:approved-1","backup":"secret-ref:approved-1"}`,
	} {
		if _, err := pool.Exec(ctx, `UPDATE plugin_configurations SET secret_refs=$1::jsonb
			WHERE plugin_version_id=$2 AND owner_user_id=$3`, invalid, v5ConfigFixtureVersionID, owner); err == nil {
			t.Fatalf("direct SQL accepted invalid refs: %s", invalid)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE plugin_configurations SET values='{"locale":{"nested":true}}'::jsonb
		WHERE plugin_version_id=$1 AND owner_user_id=$2`, v5ConfigFixtureVersionID, owner); err == nil {
		t.Fatal("direct SQL accepted nested ordinary config")
	}
	var outcomes [2]error
	var wait sync.WaitGroup
	for index := range outcomes {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			competing := update
			competing.ExpectedRevision = 2
			_, outcomes[index] = store.SavePluginV5Config(ctx, v5ConfigFixtureVersionID, &owner, competing, "admin:config-fixture")
		}(index)
	}
	wait.Wait()
	wins, conflicts := 0, 0
	for _, outcome := range outcomes {
		if outcome == nil {
			wins++
		} else if errors.Is(outcome, ErrPluginV5ConfigRevisionConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent result: %v", outcome)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("CAS results: wins=%d conflicts=%d", wins, conflicts)
	}
	assertPluginV5ConfigAuditOwners(t, pool)
	denied := func(_ context.Context, _ int64, _ *int64, _, _ string) (bool, error) { return false, nil }
	if _, err := NewPgPluginV5ConfigStore(pool, denied).ReadPluginV5Config(ctx, v5ConfigFixtureVersionID, &owner); !errors.Is(err, ErrPluginV5ConfigDefaultsInvalid) {
		t.Fatalf("removed selector accepted on read: %v", err)
	}
	if _, err := NewPgPluginV5ConfigStore(pool, denied).IsBoundPluginV5SecretRef(ctx, v5ConfigFixtureVersionID, &owner, "token", "secret-ref:approved-1"); !errors.Is(err, ErrPluginV5ConfigDefaultsInvalid) {
		t.Fatalf("removed selector accepted on call: %v", err)
	}
}

func assertPluginV5ConfigAuditOwners(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var total, userTargets, systemTargets, unsafeAudit int
	err := pool.QueryRow(context.Background(), `SELECT count(*),
		count(*) FILTER (WHERE details->>'owner_scope'='user' AND details->>'owner_user_id'=$2
			AND jsonb_typeof(details->'owner_user_id')='number'),
		count(*) FILTER (WHERE details->>'owner_scope'='system' AND details->'owner_user_id'='null'::jsonb),
		count(*) FILTER (WHERE details::text LIKE '%secret-ref:%' OR details::text LIKE '%token%')
		FROM platform_command_audits WHERE command_code='plugin.config.update' AND resource_id=$1`,
		"990007102", "990007103").Scan(&total, &userTargets, &systemTargets, &unsafeAudit)
	if err != nil || total != 3 || userTargets != 2 || systemTargets != 1 || unsafeAudit != 0 {
		t.Fatalf("audit target mismatch: total=%d user=%d system=%d unsafe=%d err=%v", total, userTargets, systemTargets, unsafeAudit, err)
	}
}

func ptrV5ConfigID(id int64) *int64 { return &id }
