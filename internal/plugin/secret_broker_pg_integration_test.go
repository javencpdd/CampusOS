package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	pgSecretBrokerPluginID  int64 = 99127001
	pgSecretBrokerVersionID int64 = 99127002
	pgSecretBrokerOwnerID   int64 = 99127003
	pgSecretBrokerOtherID   int64 = 99127004
	pgSecretBrokerTarget          = "https://mailer.example.edu/v1/send"
	pgSecretBrokerOrigin          = "https://mailer.example.edu"
	pgSecretBrokerSystem          = "synthetic-system-credential-fixture"
	pgSecretBrokerUser            = "synthetic-user-credential-fixture"
)

type pgSecretBrokerEgress struct {
	calls []security.EgressRequest
	err   error
}

func (sender *pgSecretBrokerEgress) Do(_ context.Context, request security.EgressRequest) (security.EgressResponse, error) {
	request.Headers = request.Headers.Clone()
	request.Body = append([]byte(nil), request.Body...)
	sender.calls = append(sender.calls, request)
	if sender.err != nil {
		return security.EgressResponse{}, sender.err
	}
	// Deliberately echo a credential in the remote response. The Broker must
	// return only its status, never these headers or this body.
	return security.EgressResponse{StatusCode: http.StatusNoContent,
		Headers: http.Header{"X-Remote-Secret": {pgSecretBrokerSystem}}, Body: []byte(pgSecretBrokerSystem)}, nil
}

// Wrap the real PostgreSQL adapter so failure injection does not replace
// successful persistence with an in-memory fake.
type pgSecretBrokerFaultAudit struct {
	store         *reliability.PostgreSQLStore
	startFailure  bool
	updateFailure bool
	starts        int
	updates       int
	operationID   string
}

func (audit *pgSecretBrokerFaultAudit) StartOperation(ctx context.Context, operation reliability.Operation) (*reliability.Operation, error) {
	audit.starts++
	if audit.startFailure {
		return nil, errors.New(pgSecretBrokerSystem)
	}
	started, err := audit.store.StartOperation(ctx, operation)
	if err == nil && started != nil {
		audit.operationID = started.ID
	}
	return started, err
}

func (audit *pgSecretBrokerFaultAudit) UpdateOperation(ctx context.Context, operation reliability.Operation) error {
	audit.updates++
	if audit.updateFailure {
		return errors.New(pgSecretBrokerSystem)
	}
	return audit.store.UpdateOperation(ctx, operation)
}

type pgSecretBrokerBudgetProvider struct{ budget *security.PurposeBudget }

func (provider *pgSecretBrokerBudgetProvider) CurrentSecretPurposeBudget(_ context.Context, versionID int64, purpose string) (*security.PurposeBudget, error) {
	if versionID != pgSecretBrokerVersionID || purpose != "notify-course" {
		return nil, ErrSecretUseDenied
	}
	return provider.budget, nil
}

type pgSecretBrokerFixture struct {
	auth    *AuthorizationService
	config  *PgPluginV5ConfigStore
	secrets *SecretService
	broker  *SecretUseBroker
	egress  *pgSecretBrokerEgress
	budget  *security.PurposeBudget
	system  SecretUseRequest
	user    SecretUseRequest
}

func pgSecretBrokerResources() security.ResourceSet {
	return security.ResourceSet{NetworkTargets: []string{pgSecretBrokerOrigin}, StorageBytes: 1024,
		CPUMillis: 500, MemoryMB: 64, MaxConcurrency: 2, TimeoutMS: 2000}
}

func pgSecretBrokerResourceMap(t *testing.T) map[string]interface{} {
	t.Helper()
	encoded, err := json.Marshal(pgSecretBrokerResources())
	if err != nil {
		t.Fatal(err)
	}
	var mapped map[string]interface{}
	if err := json.Unmarshal(encoded, &mapped); err != nil {
		t.Fatal(err)
	}
	return mapped
}

func pgSecretBrokerManifest(t *testing.T) []byte {
	t.Helper()
	var manifest map[string]interface{}
	if err := json.Unmarshal(testPluginV5Manifest(), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["host_resources"] = pgSecretBrokerResourceMap(t)
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func newPGSecretBrokerFixture(t *testing.T, pool *pgxpool.Pool) pgSecretBrokerFixture {
	t.Helper()
	return newPGSecretBrokerFixtureWithAudit(t, pool, reliability.NewPostgreSQLStore(pool))
}

func newPGSecretBrokerFixtureWithAudit(t *testing.T, pool *pgxpool.Pool, audit SecretUseAuditStore) pgSecretBrokerFixture {
	t.Helper()
	store := NewPgAuthorizationStore(pool)
	auth := NewAuthorizationService(store, NewPgPluginRepository(pool), func(string) bool { return true })
	visible := func(_ context.Context, _ int64, _ *int64, kind, value string) (bool, error) {
		return kind == "profile" && value == "public", nil
	}
	config := NewPgPluginV5ConfigStore(pool, visible)
	secrets, err := NewSecretService(store, []byte("0123456789abcdef0123456789abcdef"), "broker-key-v1")
	if err != nil {
		t.Fatal(err)
	}
	resources := pgSecretBrokerResources()
	provider, err := NewSecretGrantResourceProvider(store, resources, resources)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := security.NewPurposeBudget("notify-course", security.PurposeBudgetLimits{
		MaxUnits: 2, MaxConcurrency: 1, MaxDuration: 2 * time.Second, ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	egress := &pgSecretBrokerEgress{}
	broker, err := NewSecretUseBroker(auth, config, secrets, provider, &pgSecretBrokerBudgetProvider{budget}, egress, audit)
	if err != nil {
		t.Fatal(err)
	}
	owner := pgSecretBrokerOwnerID
	return pgSecretBrokerFixture{
		auth: auth, config: config, secrets: secrets, broker: broker, egress: egress, budget: budget,
		system: SecretUseRequest{PluginVersionID: pgSecretBrokerVersionID, SecretName: "token",
			SecretRef: "secret-ref:system-mail", ProfileID: "public", TargetURL: pgSecretBrokerTarget,
			Purpose: "notify-course", Body: []byte(`{"event":"course"}`)},
		user: SecretUseRequest{PluginVersionID: pgSecretBrokerVersionID, OwnerUserID: &owner, SecretName: "token",
			SecretRef: "secret-ref:user-mail", ProfileID: "public", TargetURL: pgSecretBrokerTarget,
			Purpose: "notify-course", Body: []byte(`{"event":"personal"}`)},
	}
}

func pgSecretBrokerGrantScope(t *testing.T, request SecretUseRequest, scope string) map[string]interface{} {
	t.Helper()
	return map[string]interface{}{"scope": scope, "secret_bindings": []interface{}{secretUseBinding(request)},
		"host_resources": pgSecretBrokerResourceMap(t)}
}

func pgSecretBrokerConsentScope(request SecretUseRequest) map[string]interface{} {
	return map[string]interface{}{"scope": "self", "secret_bindings": []interface{}{secretUseBinding(request)}}
}

func seedPGSecretBroker(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture pgSecretBrokerFixture) {
	t.Helper()
	for _, user := range []struct {
		id   int64
		name string
	}{{pgSecretBrokerOwnerID, "v12_secret_broker_owner"}, {pgSecretBrokerOtherID, "v12_secret_broker_other"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email)
			VALUES($1,$2,$3,$4)`, user.id, user.name, user.name, user.name+"@example.invalid"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO plugins(id,name,display_name,version,runtime)
		VALUES($1,'v12-01b-secret-broker','V12 Secret Broker','1.0.0','grpc')`, pgSecretBrokerPluginID); err != nil {
		t.Fatal(err)
	}
	// Assemble the immutable declaration collection while staged, then publish.
	// A direct SQL v5 fixture is necessary until the 03a/03b installer exists.
	if _, err := pool.Exec(ctx, `INSERT INTO plugin_versions
		(id,plugin_id,version,package_digest,manifest_api_version,host_api_version,
		 permission_fingerprint,manifest,lifecycle_status)
		VALUES($1,$2,'1.0.0',$3,'campusos.plugin/v5','v5',$4,$5::jsonb,'staged')`,
		pgSecretBrokerVersionID, pgSecretBrokerPluginID, strings.Repeat("a", 64), strings.Repeat("b", 64), pgSecretBrokerManifest(t)); err != nil {
		t.Fatal(err)
	}
	for index, declaration := range []struct{ code, scope string }{{"secret.system.read", "system"}, {"secret.self.read", "self"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO plugin_capability_declarations
			(id,plugin_version_id,capability_code,purpose,risk_level,required,resource_scope,data_classification)
			VALUES($1,$2,$3,'发送课程通知','high',true,$4::jsonb,'restricted')`,
			pgSecretBrokerVersionID+int64(10+index), pgSecretBrokerVersionID, declaration.code,
			fmt.Sprintf(`{"scope":%q}`, declaration.scope)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE plugin_versions SET lifecycle_status='active',activated_at=NOW() WHERE id=$1`, pgSecretBrokerVersionID); err != nil {
		t.Fatal(err)
	}
	update := PluginV5ConfigUpdate{ExpectedRevision: 1,
		Values:     json.RawMessage(`{"locale":"zh-CN","profile_id":"public","max_results":10}`),
		SecretRefs: map[string]string{"token": fixture.system.SecretRef}}
	if _, err := fixture.config.SavePluginV5Config(ctx, pgSecretBrokerVersionID, nil, update, "admin:broker-fixture"); err != nil {
		t.Fatalf("save system binding: %v", err)
	}
	owner, other := pgSecretBrokerOwnerID, pgSecretBrokerOtherID
	update.SecretRefs = map[string]string{"token": fixture.user.SecretRef}
	for _, ownerID := range []*int64{&owner, &other} {
		if _, err := fixture.config.SavePluginV5Config(ctx, pgSecretBrokerVersionID, ownerID, update, "admin:broker-fixture"); err != nil {
			t.Fatalf("save user binding: %v", err)
		}
	}
	for _, secret := range []struct {
		owner *int64
		value string
	}{{nil, pgSecretBrokerSystem}, {&owner, pgSecretBrokerUser}, {&other, "synthetic-other-credential-fixture"}} {
		if _, err := fixture.secrets.Put(ctx, pgSecretBrokerPluginID, secret.owner, "token", secret.value, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []struct {
		code, scope string
		request     SecretUseRequest
	}{{"secret.system.read", "system", fixture.system}, {"secret.self.read", "self", fixture.user}} {
		if _, err := fixture.auth.SetAdminGrant(ctx, pgSecretBrokerVersionID, grant.code, "granted",
			"approve exact fixture", pgSecretBrokerGrantScope(t, grant.request, grant.scope), "fixture", nil); err != nil {
			t.Fatalf("set %s grant: %v", grant.code, err)
		}
	}
}

func assertPGSecretBrokerDenied(t *testing.T, fixture *pgSecretBrokerFixture, request SecretUseRequest) {
	t.Helper()
	before := len(fixture.egress.calls)
	result, err := fixture.broker.Use(context.Background(), request)
	if !errors.Is(err, ErrSecretUseDenied) || result != (SecretUseResult{}) || len(fixture.egress.calls) != before ||
		strings.Contains(fmt.Sprint(err), "credential-fixture") {
		t.Fatalf("denied use escaped or leaked: result=%+v err=%v calls=%d", result, err, len(fixture.egress.calls))
	}
}

func assertPGSecretBrokerSend(t *testing.T, fixture *pgSecretBrokerFixture, request SecretUseRequest, expectedSecret string) {
	t.Helper()
	before := len(fixture.egress.calls)
	result, err := fixture.broker.Use(context.Background(), request)
	if err != nil || result.StatusCode != http.StatusNoContent || len(fixture.egress.calls) != before+1 {
		t.Fatalf("approved use failed: status=%d err=%v sends=%d", result.StatusCode, err, len(fixture.egress.calls))
	}
	sent := fixture.egress.calls[before]
	if sent.URL != request.TargetURL || sent.Method != http.MethodPost || !sent.DenyRedirects || sent.Headers.Get("Authorization") != "Bearer "+expectedSecret ||
		strings.Contains(string(sent.Body), expectedSecret) || strings.Contains(fmt.Sprint(result), expectedSecret) {
		t.Fatal("credential was not confined to the exact authenticated host request")
	}
}

func assertPGSecretBrokerAuditSafe(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var decisions, allowed, denied, unsafe int64
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE outcome='allow'), count(*) FILTER (WHERE outcome='deny'), count(*) FILTER
		(WHERE resource_scope::text LIKE '%credential-fixture%' OR context::text LIKE '%credential-fixture%')
		FROM plugin_authorization_decisions WHERE plugin_version_id=$1`, pgSecretBrokerVersionID).Scan(&decisions, &allowed, &denied, &unsafe); err != nil || decisions < 1 || allowed < 1 || denied < 1 || unsafe != 0 {
		t.Fatalf("authorization decision audit not safe: rows=%d allowed=%d denied=%d unsafe=%d err=%v", decisions, allowed, denied, unsafe, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_command_audits
		WHERE command_code='plugin.config.update' AND resource_id=$1 AND details::text LIKE '%credential-fixture%'`,
		fmt.Sprint(pgSecretBrokerVersionID)).Scan(&unsafe); err != nil || unsafe != 0 {
		t.Fatalf("configuration command audit contains credential: count=%d err=%v", unsafe, err)
	}
}

func countPGSecretUseAudits(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_operation_runs
		WHERE kind='plugin.secret.use' AND details->>'plugin_version_id'=$1`,
		fmt.Sprint(pgSecretBrokerVersionID)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertPGSecretUseOutcomes(t *testing.T, ctx context.Context, pool *pgxpool.Pool, successes int64) {
	t.Helper()
	var success, unknown, denied int64
	if err := pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE status='succeeded' AND details->>'outcome'='success'
			AND details->>'reserved_units'='1' AND details->>'charged_units'='1'
			AND details->>'charge_known'='true' AND details->>'status_code'='204'
			AND details->>'error_code'=''),
		count(*) FILTER (WHERE status='failed' AND details->>'outcome'='unknown'
			AND details->>'reserved_units'='1' AND details->>'charged_units'='1'
			AND details->>'charge_known'='false' AND details->>'status_code'='0'
			AND details->>'error_code'='egress_failed'),
		count(*) FILTER (WHERE status='failed' AND details->>'outcome'='denied'
			AND details->>'charged_units'='0' AND details->>'charge_known'='true')
		FROM platform_operation_runs WHERE kind='plugin.secret.use' AND details->>'plugin_version_id'=$1`,
		fmt.Sprint(pgSecretBrokerVersionID)).Scan(&success, &unknown, &denied); err != nil ||
		success < successes || unknown < 1 || denied < 1 {
		t.Fatalf("usage audit outcome or charge absent: success=%d unknown=%d denied=%d err=%v", success, unknown, denied, err)
	}
	rows, err := pool.Query(ctx, `SELECT id,subject_type,subject_id,COALESCE(actor_id,''),COALESCE(idempotency_key,''),details,COALESCE(error_message,'')
		FROM platform_operation_runs WHERE kind='plugin.secret.use' AND details->>'plugin_version_id'=$1`,
		fmt.Sprint(pgSecretBrokerVersionID))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	allowed := map[string]bool{"plugin_version_id": true, "owner_user_id": true, "purpose": true,
		"outcome": true, "reserved_units": true, "charged_units": true, "charge_known": true,
		"status_code": true, "error_code": true}
	for rows.Next() {
		var id, subjectType, subjectID, actor, key, message string
		var raw []byte
		if err := rows.Scan(&id, &subjectType, &subjectID, &actor, &key, &raw, &message); err != nil {
			t.Fatal(err)
		}
		var details map[string]interface{}
		if err := json.Unmarshal(raw, &details); err != nil {
			t.Fatal(err)
		}
		for field := range details {
			if !allowed[field] {
				t.Fatalf("unexpected usage audit field %q", field)
			}
		}
		if len(details) != len(allowed) {
			t.Fatalf("usage audit omits stable outcome fields: fields=%d", len(details))
		}
		encoded := strings.Join([]string{id, subjectType, subjectID, actor, key, string(raw), message}, " ")
		for _, forbidden := range []string{"credential-fixture", "secret-ref:", "token", "backup", "https://", "mailer.example.edu", "public"} {
			if strings.Contains(encoded, forbidden) {
				t.Fatalf("usage audit exposed sensitive fixture category %q", forbidden)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func assertPGSecretUseAuditFaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	before := countPGSecretUseAudits(t, ctx, pool)
	startFault := &pgSecretBrokerFaultAudit{store: reliability.NewPostgreSQLStore(pool), startFailure: true}
	beforeDispatch := newPGSecretBrokerFixtureWithAudit(t, pool, startFault)
	if result, err := beforeDispatch.broker.Use(ctx, beforeDispatch.system); !errors.Is(err, ErrSecretUseFailed) ||
		result != (SecretUseResult{}) || strings.Contains(fmt.Sprint(err), "credential-fixture") {
		t.Fatalf("audit start failure escaped or leaked: result=%+v err=%v", result, err)
	}
	if snapshot := beforeDispatch.budget.Snapshot(); snapshot.ConsumedUnits != 0 || snapshot.InFlight != 0 ||
		len(beforeDispatch.egress.calls) != 0 || startFault.starts != 1 || startFault.updates != 0 ||
		countPGSecretUseAudits(t, ctx, pool) != before {
		t.Fatalf("audit start failure did not stop before reservation and network: %+v", snapshot)
	}
	updateFault := &pgSecretBrokerFaultAudit{store: reliability.NewPostgreSQLStore(pool), updateFailure: true}
	afterDispatch := newPGSecretBrokerFixtureWithAudit(t, pool, updateFault)
	if result, err := afterDispatch.broker.Use(ctx, afterDispatch.system); !errors.Is(err, ErrSecretUseFailed) ||
		result != (SecretUseResult{}) || strings.Contains(fmt.Sprint(err), "credential-fixture") {
		t.Fatalf("audit finalization failure escaped or leaked: result=%+v err=%v", result, err)
	}
	// A completed remote send cannot be withdrawn because the audit update
	// failed. The call is made once and its charged unit stays consumed. No
	// retry is performed; the surviving running row requires reconciliation.
	if snapshot := afterDispatch.budget.Snapshot(); snapshot.ConsumedUnits != 1 || snapshot.InFlight != 0 ||
		len(afterDispatch.egress.calls) != 1 || updateFault.starts != 1 || updateFault.updates != 1 ||
		updateFault.operationID == "" || countPGSecretUseAudits(t, ctx, pool) != before+1 {
		t.Fatalf("audit finalization failure lost dispatch or charge: %+v", snapshot)
	}
	var running int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_operation_runs
		WHERE id=$1 AND status='running' AND details->>'outcome'='running'
		AND details->>'reserved_units'='0' AND details->'charged_units'='null'::jsonb
		AND details->>'charge_known'='false'`, updateFault.operationID).Scan(&running); err != nil || running != 1 {
		t.Fatalf("failed update did not leave a visible uncertain audit row: rows=%d err=%v", running, err)
	}
}

func TestPostgresSecretUseBrokerRestore(t *testing.T) {
	if os.Getenv("CAMPUSOS_SECRET_BROKER_TEST_ISOLATED") != "1" {
		t.Skip("requires owned V12-01b Secret Broker PostgreSQL database")
	}
	phase := os.Getenv("CAMPUSOS_SECRET_BROKER_TEST_PHASE")
	if phase != "seed" && phase != "restore" {
		t.Fatal("CAMPUSOS_SECRET_BROKER_TEST_PHASE must be seed or restore")
	}
	dsn := os.Getenv("CAMPUSOS_PG_INTEGRATION_DSN")
	if dsn == "" {
		t.Fatal("CAMPUSOS_PG_INTEGRATION_DSN is required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var database string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(database, "campusos_v12_01b_secret_broker") {
		t.Fatalf("refusing non-isolated database %q", database)
	}
	fixture := newPGSecretBrokerFixture(t, pool)
	if phase == "seed" {
		seedPGSecretBroker(t, ctx, pool, fixture)
		assertPGSecretBrokerDenied(t, &fixture, fixture.user) // consent absent
		other := pgSecretBrokerOtherID
		otherRequest := fixture.user
		otherRequest.OwnerUserID = &other
		assertPGSecretBrokerDenied(t, &fixture, otherRequest) // ref exists, but the other owner has no consent
		for _, change := range []func(*SecretUseRequest){
			func(r *SecretUseRequest) { r.PluginVersionID++ },
			func(r *SecretUseRequest) { r.SecretRef = "secret-ref:other" },
			func(r *SecretUseRequest) { r.ProfileID = "other-profile" },
			func(r *SecretUseRequest) { r.TargetURL = pgSecretBrokerOrigin + "/v1/other" },
			func(r *SecretUseRequest) { r.TargetURL = "https://other.example.edu/v1/send" },
			func(r *SecretUseRequest) { r.Purpose = "export-private" },
		} {
			changed := fixture.system
			change(&changed)
			assertPGSecretBrokerDenied(t, &fixture, changed)
		}
		// Reusing one opaque ref for a different Secret name in a later config
		// revision must not reuse the original token-specific approval.
		reboundConfig := PluginV5ConfigUpdate{ExpectedRevision: 2,
			Values:     json.RawMessage(`{"locale":"zh-CN","profile_id":"public","max_results":10}`),
			SecretRefs: map[string]string{"backup": fixture.system.SecretRef}}
		if _, err := fixture.config.SavePluginV5Config(ctx, pgSecretBrokerVersionID, nil, reboundConfig, "admin:broker-fixture"); err != nil {
			t.Fatalf("rebind fixture ref to backup: %v", err)
		}
		if _, err := fixture.secrets.Put(ctx, pgSecretBrokerPluginID, nil, "backup", "synthetic-backup-credential-fixture", nil); err != nil {
			t.Fatal(err)
		}
		reboundRequest := fixture.system
		reboundRequest.SecretName = "backup"
		assertPGSecretBrokerDenied(t, &fixture, reboundRequest)
		reboundConfig.ExpectedRevision = 3
		reboundConfig.SecretRefs = map[string]string{"token": fixture.system.SecretRef}
		if _, err := fixture.config.SavePluginV5Config(ctx, pgSecretBrokerVersionID, nil, reboundConfig, "admin:broker-fixture"); err != nil {
			t.Fatalf("restore token config binding: %v", err)
		}
		consentScope := pgSecretBrokerConsentScope(fixture.user)
		for _, status := range []string{"granted", "revoked"} {
			if _, err := fixture.auth.SetUserConsent(ctx, fmt.Sprint(pgSecretBrokerOwnerID), pgSecretBrokerVersionID,
				"secret.self.read", status, consentScope); err != nil {
				t.Fatalf("set owner consent %s: %v", status, err)
			}
		}
		assertPGSecretBrokerDenied(t, &fixture, fixture.user)
		if _, err := fixture.auth.SetUserConsent(ctx, fmt.Sprint(pgSecretBrokerOwnerID), pgSecretBrokerVersionID,
			"secret.self.read", "granted", consentScope); err != nil {
			t.Fatalf("restore owner consent: %v", err)
		}
		assertPGSecretBrokerDenied(t, &fixture, otherRequest) // owner consent must not authorize another owner
		grantScope := pgSecretBrokerGrantScope(t, fixture.system, "system")
		if _, err := fixture.auth.SetAdminGrant(ctx, pgSecretBrokerVersionID, "secret.system.read", "revoked",
			"revoke fixture", grantScope, "fixture", nil); err != nil {
			t.Fatalf("revoke system grant: %v", err)
		}
		assertPGSecretBrokerDenied(t, &fixture, fixture.system)
		narrowedScope := pgSecretBrokerGrantScope(t, fixture.system, "system")
		narrowedResources := pgSecretBrokerResourceMap(t)
		narrowedResources["network_targets"] = []string{}
		narrowedScope["host_resources"] = narrowedResources
		if _, err := fixture.auth.SetAdminGrant(ctx, pgSecretBrokerVersionID, "secret.system.read", "granted",
			"remove target fixture", narrowedScope, "fixture", nil); err != nil {
			t.Fatalf("narrow system resource grant: %v", err)
		}
		assertPGSecretBrokerDenied(t, &fixture, fixture.system)
		expired := time.Now().Add(-time.Minute)
		if _, err := fixture.auth.SetAdminGrant(ctx, pgSecretBrokerVersionID, "secret.system.read", "granted",
			"expired fixture", grantScope, "fixture", &expired); err != nil {
			t.Fatalf("expire system resource grant: %v", err)
		}
		assertPGSecretBrokerDenied(t, &fixture, fixture.system)
		if _, err := fixture.auth.SetAdminGrant(ctx, pgSecretBrokerVersionID, "secret.system.read", "granted",
			"restore fixture", grantScope, "fixture", nil); err != nil {
			t.Fatalf("restore system grant: %v", err)
		}
		assertPGSecretBrokerSend(t, &fixture, fixture.system, pgSecretBrokerSystem)
		assertPGSecretBrokerSend(t, &fixture, fixture.user, pgSecretBrokerUser)
		assertPGSecretBrokerDenied(t, &fixture, fixture.system) // two-unit budget exhausted
		if snapshot := fixture.budget.Snapshot(); snapshot.ConsumedUnits != 2 || snapshot.InFlight != 0 {
			t.Fatalf("budget did not settle exact use count: %+v", snapshot)
		}
		failed := newPGSecretBrokerFixture(t, pool)
		failed.egress.err = errors.New(pgSecretBrokerSystem)
		if result, err := failed.broker.Use(ctx, failed.system); !errors.Is(err, ErrSecretUseFailed) ||
			result != (SecretUseResult{}) || strings.Contains(fmt.Sprint(err), "credential-fixture") {
			t.Fatalf("network failure escaped or leaked: result=%+v err=%v", result, err)
		}
		if snapshot := failed.budget.Snapshot(); snapshot.ConsumedUnits != 1 || snapshot.InFlight != 0 {
			t.Fatalf("unknown outcome did not charge reserved unit: %+v", snapshot)
		}
		assertPGSecretUseOutcomes(t, ctx, pool, 2)
		assertPGSecretUseAuditFaults(t, ctx, pool)
		assertPGSecretUseOutcomes(t, ctx, pool, 2)
		assertPGSecretBrokerAuditSafe(t, ctx, pool)
		return
	}
	// Restore uses new host service objects and a fresh process-local budget,
	// but inserts no fixture rows. Package installation is outside this proof.
	if bound, err := fixture.config.IsBoundPluginV5SecretRef(ctx, pgSecretBrokerVersionID, fixture.user.OwnerUserID,
		"token", fixture.user.SecretRef); err != nil || !bound {
		t.Fatalf("restored owner ref unavailable: bound=%v err=%v", bound, err)
	}
	assertPGSecretBrokerSend(t, &fixture, fixture.system, pgSecretBrokerSystem)
	assertPGSecretBrokerSend(t, &fixture, fixture.user, pgSecretBrokerUser)
	assertPGSecretUseOutcomes(t, ctx, pool, 4)
	assertPGSecretBrokerAuditSafe(t, ctx, pool)
}
