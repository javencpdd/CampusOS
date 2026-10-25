package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManagedGatewayPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_AI_MANAGED_DSN")
	if dsn == "" {
		t.Skip("requires owned managed AI PG")
	}
	pc, e := pgxpool.ParseConfig(dsn)
	if e != nil || pc.ConnConfig.Host != "127.0.0.1" || !strings.HasPrefix(pc.ConnConfig.Database, "campusos_v12_14a_") {
		t.Fatal("owned PG required")
	}
	pool, e := pgxpool.NewWithConfig(t.Context(), pc)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	ctx := t.Context()
	sql := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	var calls atomic.Int32
	var mode atomic.Value
	mode.Store("success")
	started := make(chan struct{}, 8)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer LOCAL_PRIVATE_SECRET" {
			t.Error("wrong profile Secret target")
		}
		var req openAIChatRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != "local-model" || len(req.Messages) != 1 {
			t.Error("wrong model/payload")
		}
		if mode.Load() == "wait" {
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte(`{"model":"local-model","choices":[{"message":{"role":"assistant","content":"LOCAL_PRIVATE_RESULT"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	defer peer.Close()
	cfg := config.AIConfig{Enabled: true, BaseURL: "https://owned.models.test/v1", Model: "remote-model", APIKey: "REMOTE_PRIVATE_SECRET", LocalBaseURL: peer.URL + "/v1", LocalModel: "local-model", LocalAPIKey: "LOCAL_PRIVATE_SECRET", ActiveKeyID: "owned", EncryptionKeys: "owned:" + strings.Repeat("ab", 32), DefaultProfile: "local-chat", BudgetRequests: 5, MaxConcurrent: 1, MaxOutputTokens: 64, Timeout: "2s"}
	configs, keys, e := managedConfiguration(cfg)
	if e != nil {
		t.Fatal(e)
	}
	s := &ManagedService{pool: pool, reliable: reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)), enabled: func() bool { return true }, configs: configs, keys: keys, defaultProfile: "local-chat", instances: map[string]*managedInstance{}, remote: func(p security.EgressPolicy) (managedEgress, error) { return security.NewEgressBroker(p) }}
	if e = s.start(ctx); e != nil {
		t.Fatal(e)
	}
	defer s.stop()
	if s.configurationErr != nil {
		t.Fatal(s.configurationErr)
	}
	resolve := func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "101"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "owned-session"}, nil
	}
	b, e := s.binding(ctx, false)
	if e != nil {
		t.Fatal(e)
	}
	request := ChatRequest{Messages: []Message{{Role: "user", Content: "PRIVATE_PROMPT"}}}
	id := managedID("ar_")
	// The disclosure is a current, exact destination preview, never an AI call
	// or reusable permission. Even rejected/currently changing plans spend no
	// budget and must not expose partial destination data.
	profile, e := s.profile(ctx, "local-chat", false)
	if e != nil {
		t.Fatal(e)
	}
	boundedResolve := func(c context.Context) (identityport.PrincipalContext, error) {
		deadline, ok := c.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Error("disclosure planning has no five second bound")
		}
		return resolve(c)
	}
	disclosure, e := s.PlanLocalDisclosure(ctx, boundedResolve, b.Revision)
	expectedDisclosure := LocalDisclosurePlan{LocalPlan: LocalPlan{ProfileID: "local-chat", ProfileRevision: profile.Revision, BindingRevision: b.Revision}, Definition: "builtin.local-openai.text", Model: "local-model", Endpoint: peer.URL + "/v1/chat/completions", MaxTokens: 64}
	if e != nil || disclosure != expectedDisclosure {
		t.Fatal("exact local destination disclosure", disclosure, e)
	}
	disclosureJSON, e := json.Marshal(disclosure)
	if e != nil || strings.Contains(string(disclosureJSON), "SECRET") || strings.Contains(string(disclosureJSON), "SecretRef") || strings.Contains(string(disclosureJSON), "Fingerprint") {
		t.Fatal("disclosure exposed credential metadata", e)
	}
	if stale, err := s.PlanLocalDisclosure(ctx, resolve, b.Revision+1); !errors.Is(err, ErrManagedConflict) || stale != (LocalDisclosurePlan{}) {
		t.Fatal("stale disclosure allowed", stale, err)
	}
	if denied, err := s.PlanLocalDisclosure(ctx, nil, b.Revision); !errors.Is(err, ErrManagedDenied) || denied != (LocalDisclosurePlan{}) {
		t.Fatal("disclosure accepted absent current User", denied, err)
	}
	adminResolve := func(c context.Context) (identityport.PrincipalContext, error) {
		p, err := resolve(c)
		p.Actor.Kind, p.Audience = "admin", "admin"
		return p, err
	}
	if denied, err := s.PlanLocalDisclosure(ctx, adminResolve, b.Revision); !errors.Is(err, ErrManagedDenied) || denied != (LocalDisclosurePlan{}) {
		t.Fatal("same ID Admin disclosure allowed", denied, err)
	}
	for _, revokedAt := range []int{2, 3} {
		resolutions := 0
		revoking := func(c context.Context) (identityport.PrincipalContext, error) {
			resolutions++
			if resolutions >= revokedAt {
				return identityport.PrincipalContext{}, ErrManagedDenied
			}
			return resolve(c)
		}
		if denied, err := s.PlanLocalDisclosure(ctx, revoking, b.Revision); !errors.Is(err, ErrManagedDenied) || denied != (LocalDisclosurePlan{}) {
			t.Fatal("revoked User disclosed destination", revokedAt, denied, err)
		}
	}
	rollbackDisclosure := errors.New("rollback owned disclosure fixture")
	if err := transaction.NewPostgreSQL(pool).Within(ctx, func(txctx context.Context) error {
		if _, err := transaction.ExecutorFor(txctx, pool).Exec(txctx, `UPDATE ai_model_bindings SET profile_id='remote-chat',revision=nextval('ai_gateway_epoch_seq') WHERE name='chat'`); err != nil {
			return err
		}
		remoteBinding, err := s.binding(txctx, false)
		if err != nil {
			return err
		}
		if denied, err := s.PlanLocalDisclosure(txctx, resolve, remoteBinding.Revision); !errors.Is(err, ErrManagedDenied) || denied != (LocalDisclosurePlan{}) {
			t.Fatal("remote disclosure allowed", denied, err)
		}
		return rollbackDisclosure
	}); !errors.Is(err, rollbackDisclosure) {
		t.Fatal("remote disclosure fixture failed", err)
	}
	if err := transaction.NewPostgreSQL(pool).Within(ctx, func(txctx context.Context) error {
		resolutions := 0
		changing := func(c context.Context) (identityport.PrincipalContext, error) {
			resolutions++
			if resolutions == 2 {
				if _, err := transaction.ExecutorFor(c, pool).Exec(c, `UPDATE ai_model_profiles SET model='changed-model',revision=nextval('ai_gateway_epoch_seq') WHERE id='local-chat'`); err != nil {
					return identityport.PrincipalContext{}, err
				}
			}
			return resolve(c)
		}
		if stale, err := s.PlanLocalDisclosure(txctx, changing, b.Revision); !errors.Is(err, ErrManagedConflict) || stale != (LocalDisclosurePlan{}) {
			t.Fatal("changed destination returned stale disclosure", stale, err)
		}
		return rollbackDisclosure
	}); !errors.Is(err, rollbackDisclosure) {
		t.Fatal("changing disclosure fixture failed", err)
	}
	disclosureBudget, e := s.budget(ctx, "local-chat", false)
	if e != nil || disclosureBudget.Charged != 0 || disclosureBudget.Reserved != 0 || calls.Load() != 0 {
		t.Fatal("disclosure planning dispatched or charged", disclosureBudget, calls.Load(), e)
	}
	out, e := s.Invoke(ctx, resolve, id, b.Revision, request)
	if e != nil || out.Response.Content != "LOCAL_PRIVATE_RESULT" || out.Run.State != "completed" || len(out.Run.Events) != 3 || !out.Run.UsageKnown {
		t.Fatal("actual managed invoke", out.Run, e)
	}
	budget, e := s.budget(ctx, "local-chat", false)
	if e != nil || budget.Charged != 1 || budget.Reserved != 0 {
		t.Fatal("durable settlement", budget, e)
	}
	plan, e := s.PlanLocal(ctx, resolve, b.Revision)
	if e != nil || plan.ProfileID != "local-chat" || plan.BindingRevision != b.Revision {
		t.Fatal("checked local plan", plan, e)
	}
	if _, e = s.InvokeLocalChecked(ctx, resolve, managedID("ar_"), b.Revision, request, nil); !errors.Is(e, ErrManagedDenied) {
		t.Fatal("nil local source proof allowed")
	}
	if _, e = s.InvokeLocalChecked(ctx, resolve, managedID("ar_"), b.Revision, request, func(context.Context) error { return errors.New("private source revoked") }); !errors.Is(e, ErrManagedConflict) || calls.Load() != 1 {
		t.Fatal("failed source proof dispatched", e)
	}
	checks := 0
	if _, e = s.InvokeLocalChecked(ctx, resolve, managedID("ar_"), b.Revision, request, func(context.Context) error {
		checks++
		if checks > 1 {
			return ErrManagedDenied
		}
		return nil
	}); !errors.Is(e, ErrManagedConflict) || calls.Load() != 1 {
		t.Fatal("source changed before reserve was ignored", e)
	}
	budget, _ = s.budget(ctx, "local-chat", false)
	if budget.Charged != 1 || budget.Reserved != 0 {
		t.Fatal("rejected proof consumed budget", budget)
	}
	if _, e = s.Invoke(ctx, resolve, id, b.Revision, request); !errors.Is(e, ErrManagedReplay) || calls.Load() != 1 {
		t.Fatal("duplicate replay", e)
	}
	other := func(c context.Context) (identityport.PrincipalContext, error) {
		p, e := resolve(c)
		p.Actor.ID = "102"
		return p, e
	}
	if _, e = s.Run(ctx, other, id); !errors.Is(e, ErrManagedDenied) {
		t.Fatal("cross owner run metadata")
	}
	var cipher []byte
	if e = pool.QueryRow(ctx, `SELECT ciphertext FROM ai_model_secrets WHERE profile_id='local-chat'`).Scan(&cipher); e != nil || strings.Contains(string(cipher), "LOCAL_PRIVATE_SECRET") {
		t.Fatal("plaintext Secret", e)
	}
	var persisted string
	if e = pool.QueryRow(ctx, `SELECT string_agg(to_jsonb(t)::text,'') FROM ai_model_runs t`).Scan(&persisted); e != nil || strings.Contains(persisted, "PRIVATE_PROMPT") || strings.Contains(persisted, "LOCAL_PRIVATE_RESULT") {
		t.Fatal("body in execution metadata", e)
	}
	// Required audit failure rolls back quota/run/events before any network I/O.
	sql(`CREATE FUNCTION owned_ai_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.command_code='ai.call.reserve' THEN RAISE EXCEPTION 'owned fault'; END IF; RETURN NEW; END $$;CREATE TRIGGER owned_ai_audit_fault BEFORE INSERT ON platform_command_audits FOR EACH ROW EXECUTE FUNCTION owned_ai_audit_fault()`)
	if out, e = s.Invoke(ctx, resolve, managedID("ar_"), b.Revision, request); e == nil || out.Response.Content != "" || calls.Load() != 1 {
		t.Fatal("audit allowed external dispatch", e)
	}
	sql(`DROP TRIGGER owned_ai_audit_fault ON platform_command_audits;DROP FUNCTION owned_ai_audit_fault()`)
	// A lost settlement COMMIT never releases buffered text or blindly retries.
	sql(`CREATE FUNCTION owned_ai_settle_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.command_code='ai.call.settle' THEN RAISE EXCEPTION 'owned fault'; END IF; RETURN NEW; END $$;CREATE TRIGGER owned_ai_settle_fault BEFORE INSERT ON platform_command_audits FOR EACH ROW EXECUTE FUNCTION owned_ai_settle_fault()`)
	lostID := managedID("ar_")
	out, e = s.Invoke(ctx, resolve, lostID, b.Revision, request)
	if e == nil || out.Response.Content != "" || calls.Load() != 2 {
		t.Fatal("uncommitted answer released or provider replay", e)
	}
	budget, _ = s.budget(ctx, "local-chat", false)
	if budget.Reserved != 1 || budget.Charged != 1 {
		t.Fatal("unknown outcome freed quota", budget)
	}
	sql(`DROP TRIGGER owned_ai_settle_fault ON platform_command_audits;DROP FUNCTION owned_ai_settle_fault()`)
	sql(`UPDATE ai_model_runs SET lease_until_ms=1 WHERE id=$1`, lostID)
	deadline := time.Now().Add(3 * time.Second)
	for {
		r, e := s.run(ctx, lostID, "", false)
		if e == nil && r.State == "unknown" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("normal resident reconciliation did not settle expired dispatched work", r, e)
		}
		time.Sleep(20 * time.Millisecond)
	}
	budget, _ = s.budget(ctx, "local-chat", false)
	if budget.Reserved != 0 || budget.Charged != 2 || calls.Load() != 2 {
		t.Fatal("crash reconciliation replayed or lost charge", budget, calls.Load())
	}
	// Concurrent requests cannot exceed the persisted per-profile reservation.
	mode.Store("wait")
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := s.Invoke(cancelCtx, resolve, managedID("ar_"), b.Revision, request); done <- e }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("no real pending call")
	}
	if _, e = s.Invoke(ctx, resolve, managedID("ar_"), b.Revision, request); !errors.Is(e, ErrManagedBudget) {
		t.Fatal("durable concurrency exceeded", e)
	}
	cancel()
	if e = <-done; !errors.Is(e, context.Canceled) {
		t.Fatal("cancellation not returned", e)
	}
	budget, _ = s.budget(ctx, "local-chat", false)
	if budget.Reserved != 0 || budget.Charged != 3 {
		t.Fatal("unknown cancelled dispatch not charged", budget)
	}
	s.stop()
	if _, e = s.Invoke(ctx, resolve, managedID("ar_"), b.Revision, request); !errors.Is(e, ErrManagedUnavailable) {
		t.Fatal("stopped instance reused", e)
	}
	if e = s.start(ctx); e != nil || s.configurationErr != nil {
		t.Fatal("normal restart", e)
	}
	after, _ := s.binding(ctx, false)
	if after != b {
		t.Fatal("restart changed persistent selection")
	}
	mode.Store("success")
	for i := 0; i < 2; i++ {
		if _, e = s.Invoke(ctx, resolve, managedID("ar_"), b.Revision, request); e != nil {
			t.Fatal("remaining durable quota", e)
		}
	}
	beforeExhausted := calls.Load()
	if _, e = s.Invoke(ctx, resolve, managedID("ar_"), b.Revision, request); !errors.Is(e, ErrManagedBudget) || calls.Load() != beforeExhausted {
		t.Fatal("exhausted request quota allowed dispatch", e)
	}
	budget, _ = s.budget(ctx, "local-chat", false)
	if budget.Charged != budget.Quota || budget.Reserved != 0 {
		t.Fatal("exhausted quota counters", budget)
	}
	// Empty/revoked/current-key failures do not expose or move a saved credential.
	p, _ := s.profile(ctx, "local-chat", false)
	wrong := p
	wrong.ID = "remote-chat"
	if secret, e := s.secret(ctx, wrong); e == nil || len(secret) != 0 {
		t.Fatal("cross profile secret reuse")
	}
	// Durable crash fixtures contain an expired reservation and an expired
	// dispatch intent. The ordinary resident worker must settle 0/1 respectively
	// without making either external request (even after a process restart).
	remoteProfile, e := s.profile(ctx, "remote-chat", false)
	if e != nil {
		t.Fatal(e)
	}
	reservedID, dispatchedID := managedID("ar_"), managedID("ar_")
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE ai_model_budgets SET reserved=reserved+2 WHERE profile_id='remote-chat'`); e != nil {
		t.Fatal(e)
	}
	for i, id := range []string{reservedID, dispatchedID} {
		state := "reserved"
		if i == 1 {
			state = "dispatched"
		}
		if _, e = tx.Exec(ctx, `INSERT INTO ai_model_runs(id,owner_id,profile_id,profile_revision,binding_revision,instance_id,state,lease_until_ms) VALUES($1,'101','remote-chat',$2,$3,$4,$5,1)`, id, remoteProfile.Revision, b.Revision, managedID("ai_"), state); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO ai_model_run_events(run_id,ordinal,kind) VALUES($1,1,'started')`, id); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		budget, e = s.budget(ctx, "remote-chat", false)
		if e == nil && budget.Reserved == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expired reservation reconciliation", budget, e)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if budget.Charged != 1 || calls.Load() != beforeExhausted {
		t.Fatal("crash replay or incorrect accounting", budget)
	}
	for i, id := range []string{reservedID, dispatchedID} {
		r, e := s.Run(ctx, resolve, id)
		if e != nil || r.Charged == nil || *r.Charged != i || len(r.Events) != 3 || r.UsageKnown || r.State != "unknown" {
			t.Fatal("crash settlement events", r, e)
		}
	}
	if _, e = pool.Exec(ctx, `UPDATE ai_model_run_events SET kind='failed' WHERE run_id=$1 AND ordinal=3`, reservedID); e == nil {
		t.Fatal("event history was mutable")
	}
}
