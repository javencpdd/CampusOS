//go:build campusos_acceptance

package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/modules/features/ai"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/pkg/config"
	sdk "github.com/campusos/CampusOS/sdk/go"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type pluginExitModel struct {
	local, remote      *httptest.Server
	mode               atomic.Value
	calls              atomic.Int32
	started, cancelled chan struct{}
	dial               *aiOwnedDial
	dns                *aiOwnedDNS
}

func newPluginExitModel(t *testing.T) *pluginExitModel {
	f := &pluginExitModel{started: make(chan struct{}, 32), cancelled: make(chan struct{}, 32)}
	f.mode.Store("plain")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		var q struct {
			Model    string       `json:"model"`
			Messages []ai.Message `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&q) != nil {
			t.Error("protocol request")
			return
		}
		if f.mode.Load() == "wait" {
			f.started <- struct{}{}
			<-r.Context().Done()
			f.cancelled <- struct{}{}
			return
		}
		content := "MODEL_RESULT_14C"
		if len(q.Messages) == 2 {
			var evidence struct {
				Evidence []struct{ ID, Excerpt string } `json:"evidence"`
			}
			if json.Unmarshal([]byte(q.Messages[1].Content), &evidence) != nil || len(evidence.Evidence) == 0 {
				t.Error("host evidence")
				return
			}
			raw, _ := json.Marshal(map[string]any{"answer": evidence.Evidence[0].Excerpt, "citations": []string{evidence.Evidence[0].ID}})
			content = string(raw)
		}
		if f.mode.Load() == "forged" {
			content = `{"answer":"FORGED","citations":["E999"]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": q.Model, "choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 2, "completion_tokens": 2, "total_tokens": 4}})
	})
	f.local = httptest.NewServer(handler)
	f.remote = httptest.NewTLSServer(handler)
	t.Cleanup(f.local.Close)
	t.Cleanup(f.remote.Close)
	return f
}
func (f *pluginExitModel) configure(cfg *config.Config) {
	u, _ := url.Parse(f.remote.URL)
	cfg.AI = config.AIConfig{Enabled: true, BaseURL: "https://example.com:" + u.Port() + "/v1", Model: "owned-remote14c", APIKey: "OWNED14C_REMOTE_KEY", LocalBaseURL: f.local.URL + "/v1", LocalModel: "owned-local14c", LocalAPIKey: "OWNED14C_LOCAL_KEY", ActiveKeyID: "owned14c", EncryptionKeys: "owned14c:" + strings.Repeat("ab", 32), DefaultProfile: "local-chat", BudgetRequests: 100, MaxConcurrent: 1, MaxOutputTokens: 128, Timeout: "4s"}
}
func (f *pluginExitModel) bind(s *Server) {
	u, _ := url.Parse(f.remote.URL)
	if f.dns == nil {
		f.dns = &aiOwnedDNS{}
	}
	if f.dial == nil {
		f.dial = &aiOwnedDial{target: u.Host, port: u.Port()}
	}
	roots := x509.NewCertPool()
	roots.AddCert(f.remote.Certificate())
	s.ai.AcceptanceRemote(f.dns, f.dial, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12})
}
func exitRunID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return "ar_" + hex.EncodeToString(b[:])
}
func runPluginAIKnowledgeExit(t *testing.T, s *Server, pool *pgxpool.Pool, private ed25519.PrivateKey, admin, user identity.SessionPrincipalResolver, root string, model *pluginExitModel, checks *[]string, restart func() *Server) map[string]any {
	t.Helper()
	ctx := t.Context()
	check := func(s string) { *checks = append(*checks, s); t.Log(s) }
	sql := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	count := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if e := pool.QueryRow(ctx, q, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	if _, e := s.features.Request(knowledge.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	collections := s.knowledge.Collections()
	create := func(title string) (knowledge.Collection, knowledge.Official) {
		t.Helper()
		o, e := collections.SaveOfficial(ctx, knowledge.Official{Title: title, Content: "PUBLIC14C_SOURCE 合成公开资料：服务台位于第二层。", Format: "text", Status: "published"}, admin)
		if e != nil {
			t.Fatal(e)
		}
		c, e := collections.SaveCollection(ctx, knowledge.Collection{Title: title, Status: "active"}, admin)
		if e != nil {
			t.Fatal(e)
		}
		state, e := collections.SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "official", SourceID: o.ID, Active: true}, admin)
		if e != nil {
			t.Fatal(e)
		}
		return state.Collection, o
	}
	c, official := create("14c公开绑定")
	foreign, _ := create("14c其他公开绑定")
	// Include a second member for actual source-provider pagination.
	o2, e := collections.SaveOfficial(ctx, knowledge.Official{Title: "14c第二项", Content: "PUBLIC14C_SOURCE 合成公开资料：周二关闭。", Format: "text", Status: "published"}, admin)
	if e != nil {
		t.Fatal(e)
	}
	state, e := collections.SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "official", SourceID: o2.ID, Active: true}, admin)
	if e != nil {
		t.Fatal(e)
	}
	c = state.Collection
	end := time.Now().Add(15 * time.Second)
	for {
		page, e := s.knowledge.Retrieval().SearchPublic(ctx, knowledge.PublicSearchRequest{CollectionID: c.ID, Query: "PUBLIC14C_SOURCE", Page: 1, PageSize: 20})
		if e == nil && page.Total == 2 {
			break
		}
		if time.Now().After(end) {
			t.Fatal("resident index", e)
		}
		time.Sleep(25 * time.Millisecond)
	}
	sql(`INSERT INTO personal_documents(id,owner_user_id,name,document_type) VALUES(461401,461101,'PRIVATE14C_DOCUMENT','text')`)
	raw, e := os.ReadFile(os.Getenv("CAMPUSOS_AI_KNOWLEDGE_TEMPLATE"))
	if e != nil {
		t.Fatal(e)
	}
	var m plugin.PluginV5Manifest
	if json.Unmarshal(raw, &m) != nil {
		t.Fatal("manifest")
	}
	m.Backend.Artifact.Image = os.Getenv("CAMPUSOS_AI_KNOWLEDGE_IMAGE")
	for i := range m.Consumes {
		if m.Consumes[i].Scope.Kind == "collection" {
			m.Consumes[i].Scope.CollectionIDs = []string{c.ID}
		}
	}
	dir := filepath.Join(root, "ai-knowledge-package")
	if os.Mkdir(dir, 0700) != nil {
		t.Fatal("package")
	}
	raw, _ = json.Marshal(m)
	if os.WriteFile(filepath.Join(dir, "plugin.json"), raw, 0600) != nil {
		t.Fatal("manifest")
	}
	archive, p, e := plugin.BuildPluginV5Package(dir)
	if e != nil {
		t.Fatal(e)
	}
	envelope, e := plugin.SignPluginV5Package(archive, "owned11", private)
	if e != nil {
		t.Fatal(e)
	}
	if os.WriteFile(filepath.Join(root, "ai-knowledge.tar.gz"), archive, 0600) != nil || os.WriteFile(filepath.Join(root, "ai-knowledge.release.json"), envelope, 0600) != nil {
		t.Fatal("signed package")
	}
	valueTrace, _ := s.appContext.Lookup("plugin.v5.dispatcher")
	valueTrace.(*plugin.V5ManagedRuntime).AcceptanceObserveHost(func(code, phase string, e error) {
		label := "passed"
		if e != nil {
			label = fmt.Sprintf("%T:%v", e, e)
			if len(label) > 180 {
				label = label[:180]
			}
		}
		t.Logf("host stage: %s %s %s", code, phase, label)
	})
	platform := s.pluginsV5Handler().Platform
	coordinate := plugin.V5Coordinate{Publisher: m.Publisher, Key: m.Key, Version: m.Version}
	if _, e = platform.Import(ctx, archive, envelope, "owned14c-joint-import", admin); e != nil {
		t.Fatal(e)
	}
	sequence := 0
	command := func(owner string, resolve identity.SessionPrincipalResolver, r plugin.V5Mutation) error {
		d, e := platform.Detail(ctx, coordinate, owner == "", resolve)
		if e != nil {
			return e
		}
		sequence++
		r.IdempotencyKey = fmt.Sprintf("owned14c-joint-%d", sequence)
		r.Version = m.Version
		if d.Installation != nil {
			r.ExpectedGeneration = d.Installation.Generation
		}
		if d.Grant != nil {
			r.ExpectedRevision = d.Grant.Revision
		}
		if r.Kind == "consent" {
			r.PurposeHash = d.PurposeHash
			r.ExpectedRevision = 0
			if d.Consent != nil {
				r.ExpectedRevision = d.Consent.Revision
			}
		}
		_, e = platform.Mutate(ctx, m.Publisher, m.Key, r, owner == "", resolve)
		return e
	}
	must := func(owner string, resolve identity.SessionPrincipalResolver, r plugin.V5Mutation) {
		t.Helper()
		if e := command(owner, resolve, r); e != nil {
			t.Fatal(r.Kind, e)
		}
	}
	value, _ := s.appContext.Lookup(ai.PortPluginText)
	port := value.(ai.PluginText)
	value, _ = s.appContext.Lookup(knowledge.PortPluginPublicSource)
	sourcePort := value.(knowledge.PluginPublicSource)
	invoke := func(cctx context.Context, r identity.SessionPrincipalResolver, mode string, q sdk.V5AIChatRequest) (string, error) {
		intent := struct {
			Mode string `json:"mode"`
			sdk.V5AIChatRequest
		}{mode, q}
		wire, _ := json.Marshal(intent)
		out, e := port.AnswerText(cctx, r, ai.PluginTextRequest{Publisher: m.Publisher, Key: m.Key, Version: m.Version, ContributionID: "chat-main", RequestID: "owned14c-joint", Prompt: string(wire), TimeoutMS: 25000})
		return out.Text, e
	}
	waitIndex := func() {
		t.Helper()
		// Let the resident worker complete naturally; leave enough time for all
		// pre-dispatch checks before its next periodic refresh. Never fake ready.
		end := time.Now().Add(30 * time.Second)
		for {
			page, e := s.knowledge.Retrieval().SearchPublic(ctx, knowledge.PublicSearchRequest{CollectionID: c.ID, Query: "PUBLIC14C_SOURCE", Page: 1, PageSize: 20})
			if e == nil && page.Total == 2 && count(`SELECT count(*) FROM knowledge_index_jobs WHERE id=$1 AND state='idle' AND data->>'run_generation'='0' AND data->>'requested'=data->>'active_request' AND available_at_ms>$2`, c.ID, time.Now().Add(15*time.Second).UnixMilli()) == 1 {
				return
			}
			if time.Now().After(end) {
				t.Fatal("resident public index not stable", e)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	request := func(mode string) sdk.V5AIChatRequest {
		if mode == "answer" {
			waitIndex()
		}

		plan, e := s.ai.Managed().PlanChatDisclosure(ctx, user, 0)
		if e != nil {
			t.Fatal(e)
		}
		q := sdk.V5AIChatRequest{ProfileID: plan.ProfileID, ProfileRevision: plan.ProfileRevision, BindingRevision: plan.BindingRevision, RunID: exitRunID(), Prompt: "EXPLICIT14C_PROMPT"}
		if mode == "answer" {
			q.CollectionID = c.ID
			q.Prompt = "PUBLIC14C_SOURCE"
		}
		return q
	}
	deny := func(label string, mode string, q sdk.V5AIChatRequest) {
		t.Helper()
		n := model.calls.Load()
		text, e := invoke(ctx, user, mode, q)
		if e == nil || text != "" || model.calls.Load() != n {
			t.Fatal(label, text, e)
		}
		check(label)
	}
	ready := func() {
		t.Helper()
		end := time.Now().Add(25 * time.Second)
		for {
			v, _ := s.appContext.Lookup("plugin.v5.dispatcher")
			_, e := v.(*plugin.V5ManagedRuntime).Select(ctx, "461101", plugin.V5ContributionQuery{Point: plugin.V5AIChat, ContractVersion: "v1", Capability: "text"}, &plugin.V5ContributionRef{V5Coordinate: coordinate, ID: "chat-main"})
			if e == nil {
				return
			}
			if time.Now().After(end) {
				d, _ := platform.Detail(ctx, coordinate, false, user)
				g, _ := platform.Detail(ctx, coordinate, true, admin)
				t.Fatalf("normal joint publication: %v; user=%+v global=%+v", e, d.Installation, g.Installation)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	must("", admin, plugin.V5Mutation{Kind: "install"})
	publicCaps := []plugin.PluginV5Consume{}
	for _, v := range m.Consumes {
		if v.Capability == plugin.V5KnowledgeSearch || v.Capability == plugin.V5KnowledgeSourceRead {
			publicCaps = append(publicCaps, v)
		}
	}
	must("", admin, plugin.V5Mutation{Kind: "grant", Approved: true, Consumes: publicCaps, Resources: p.Manifest.HostResources})
	must("", admin, plugin.V5Mutation{Kind: "enable"})
	must("461101", user, plugin.V5Mutation{Kind: "install"})
	if e := command("461101", user, plugin.V5Mutation{Kind: "enable"}); e == nil {
		t.Fatal("model declaration without independent consent")
	}
	check("self_model_declarations_require_distinct_current_User_consent_before_runtime_enable")
	must("461101", user, plugin.V5Mutation{Kind: "consent", Accepted: true})
	must("461101", user, plugin.V5Mutation{Kind: "enable"})
	ready()
	deny("Provider_registration_and_public_Grant_do_not_authorize_model_consumption", "model", request("model"))
	sourceReq := knowledge.PluginSourceRequest{Publisher: m.Publisher, Key: m.Key, Version: m.Version, ContributionID: "source-main", RequestID: "owned14c-source", CollectionID: c.ID, Query: "PUBLIC14C_SOURCE", Page: 1, Limit: 1, TimeoutMS: 4500}
	page, e := sourcePort.List(ctx, user, sourceReq)
	if e != nil || len(page.Items) != 1 || page.NextPage != 2 {
		t.Fatal("real source provider page1", page, e)
	}
	sourceReq.Page = 2
	page2, e := sourcePort.List(ctx, user, sourceReq)
	if e != nil || len(page2.Items) != 1 || page2.NextPage != 0 || page.Items[0].MemberID == page2.Items[0].MemberID {
		t.Fatal("source provider page2", page2, e)
	}
	sourceReq.Page = 1
	check("production_Knowledge_Port_actual_knowledge_source_SDK_Container_search_and_current_primary_source_paginated_binding")
	sourceReq.CollectionID = foreign.ID
	if out, e := sourcePort.List(ctx, user, sourceReq); e == nil || len(out.Items) != 0 {
		t.Fatal("foreign Provider binding")
	}
	sourceReq.CollectionID = c.ID
	check("source_Provider_cannot_expand_declared_approved_public_collection")
	must("", admin, plugin.V5Mutation{Kind: "grant", Approved: true, Consumes: m.Consumes, Resources: p.Manifest.HostResources})
	must("", admin, plugin.V5Mutation{Kind: "enable"})
	must("461101", user, plugin.V5Mutation{Kind: "consent", Accepted: true})
	must("461101", user, plugin.V5Mutation{Kind: "enable"})
	ready()
	q := request("model")
	text, e := invoke(ctx, user, "model", q)
	var modelOut sdk.V5AIChatAnswer
	if e != nil || json.Unmarshal([]byte(text), &modelOut) != nil || modelOut.Text != "MODEL_RESULT_14C" || modelOut.ProfileID != "local-chat" {
		var state, code string
		_ = pool.QueryRow(ctx, `SELECT state,error_code FROM ai_model_runs WHERE id=$1`, q.RunID).Scan(&state, &code)
		t.Fatalf("actual local model: %v; model_calls=%d model_runs=%d state=%s code=%s attribution_audits=%d", e, model.calls.Load(), count(`SELECT count(*) FROM ai_model_runs`), state, code, count(`SELECT count(*) FROM platform_command_audits WHERE command_code='plugin.ai.authorize'`))
	}
	check("SDK_plugin_model_Host_exact_local_Profile_current_session_required_audit_persistent_budget_and_result")
	deny("same_RunID_never_replays_or_spends_model_twice", "model", q)

	// Force a real runtime observer to wait on the account row held by model
	// reservation. The subsequent guard must not wait for its publication mutex.
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lockConn.Exec(ctx, `SELECT pg_advisory_lock(141458)`); err != nil {
		t.Fatal(err)
	}
	sql(`CREATE FUNCTION owned14c_reserve_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(141458); RETURN NEW; END $$;CREATE TRIGGER owned14c_reserve_barrier BEFORE INSERT ON ai_model_runs FOR EACH ROW EXECUTE FUNCTION owned14c_reserve_barrier()`)
	barrierQ := request("model")
	callDone := make(chan error, 1)
	go func() {
		text, e := invoke(ctx, user, "model", barrierQ)
		if e == nil && !strings.Contains(text, "MODEL_RESULT_14C") {
			e = fmt.Errorf("barrier output mismatch")
		}
		callDone <- e
	}()
	barrierEnd := time.Now().Add(2 * time.Second)
	for count(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'INSERT INTO ai_model_runs%'`) < 1 {
		if time.Now().After(barrierEnd) {
			t.Fatal("actual model reservation barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	factsDone := make(chan error, 1)
	go func() { _, e := platform.ExecutionFacts(ctx, coordinate, "461101"); factsDone <- e }()
	time.Sleep(40 * time.Millisecond)
	if count(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT id, username%'`) < 1 {
		t.Fatal("actual account row wait absent")
	}
	if _, err = lockConn.Exec(ctx, `SELECT pg_advisory_unlock(141458)`); err != nil {
		t.Fatal(err)
	}
	lockConn.Release()
	select {
	case e := <-callDone:
		if e != nil {
			t.Fatal("current-trust model guard under actual PG observer", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publication/account lock inversion")
	}
	select {
	case e := <-factsDone:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime observer did not finish")
	}
	sql(`DROP TRIGGER owned14c_reserve_barrier ON ai_model_runs;DROP FUNCTION owned14c_reserve_barrier()`)
	check("real_PG_reservation_row_lock_and_concurrent_runtime_observer_reentrant_guard_without_catalog_mutex_inversion")

	rag := request("answer")
	text, e = invoke(ctx, user, "answer", rag)
	var answer sdk.V5RAGAnswer
	if e != nil || json.Unmarshal([]byte(text), &answer) != nil || answer.Status != "answered" || len(answer.Citations) != 1 {
		var state, code string
		_ = pool.QueryRow(ctx, `SELECT state,error_code FROM ai_model_runs WHERE id=$1`, rag.RunID).Scan(&state, &code)
		t.Fatalf("actual public RAG: %v; model_calls=%d state=%s code=%s answer_status=%s", e, model.calls.Load(), state, code, answer.Status)
	}
	citation := answer.Citations[0].Citation
	if _, e = collections.Source(ctx, c.ID, citation.MemberID, citation.VisibilityRevision); e != nil {
		t.Fatal("current citation", e)
	}
	check("SDK_plugin_public_answer_Host_separate_collection_and_model_authorization_actual_index_model_and_host_citation")
	none := request("answer")
	none.Prompt = "NO_MATCH14C"
	n := model.calls.Load()
	text, e = invoke(ctx, user, "answer", none)
	if e != nil || !strings.Contains(text, "no_evidence") || model.calls.Load() != n || count(`SELECT count(*) FROM ai_model_runs WHERE id=$1`, none.RunID) != 0 {
		t.Fatal("no evidence dispatched model", text, e)
	}
	check("plugin_public_RAG_no_evidence_has_zero_model_dispatch_and_no_budget_run")
	if out, e := sourcePort.List(ctx, admin, sourceReq); e == nil || len(out.Items) != 0 {
		t.Fatal("Admin borrowed User source contribution")
	}
	if out, e := sourcePort.List(ctx, nil, sourceReq); e == nil || len(out.Items) != 0 {
		t.Fatal("anonymous source contribution")
	}
	check("Knowledge_Provider_current_User_context_rejects_same_ID_Admin_and_anonymous")

	rag.CollectionID = foreign.ID
	deny("public_RAG_foreign_collection_is_zero_model_and_zero_answer", "answer", rag)
	model.mode.Store("forged")
	if text, e = invoke(ctx, user, "answer", request("answer")); e == nil || text != "" {
		t.Fatal("forged citation escaped", text, e)
	}
	model.mode.Store("plain")
	check("plugin_RAG_still_rejects_forged_model_citations_and_discards_buffer")
	// Explicit remote Profile selection still requires its independent capability.
	plan, e := s.ai.Managed().PlanChatDisclosure(ctx, user, 0)
	if e != nil {
		t.Fatal(e)
	}
	remoteRevision := count(`SELECT revision FROM ai_model_profiles WHERE id='remote-chat'`)
	if e = s.ai.Managed().Bind(ctx, admin, "remote-chat", plan.BindingRevision, remoteRevision); e != nil {
		t.Fatal(e)
	}
	q = request("model")
	text, e = invoke(ctx, user, "model", q)
	if e != nil || !strings.Contains(text, "MODEL_RESULT_14C") || model.dial.calls.Load() == 0 {
		t.Fatal("actual remote TLS", text, e)
	}
	check("independently_approved_remote_Profile_uses_shared_public_DNS_pin_and_actual_TLS_Egress")
	text, e = invoke(ctx, user, "answer", request("answer"))
	if e != nil || !strings.Contains(text, "PUBLIC14C_SOURCE") {
		t.Fatal("remote RAG", text, e)
	}
	check("public_RAG_Host_supports_remote_Profile_without_private_source_or_endpoint_overrides")
	withoutRemote := []plugin.PluginV5Consume{}
	for _, v := range m.Consumes {
		if v.Capability != plugin.V5AIChatRemote {
			withoutRemote = append(withoutRemote, v)
		}
	}
	must("", admin, plugin.V5Mutation{Kind: "grant", Approved: true, Consumes: withoutRemote, Resources: p.Manifest.HostResources})
	must("", admin, plugin.V5Mutation{Kind: "enable"})
	must("461101", user, plugin.V5Mutation{Kind: "consent", Accepted: true})
	must("461101", user, plugin.V5Mutation{Kind: "enable"})
	ready()
	deny("local_Profile_approval_never_authorizes_current_remote_Profile", "model", request("model"))
	must("", admin, plugin.V5Mutation{Kind: "grant", Approved: true, Consumes: m.Consumes, Resources: p.Manifest.HostResources})
	must("", admin, plugin.V5Mutation{Kind: "enable"})
	must("461101", user, plugin.V5Mutation{Kind: "consent", Accepted: true})
	must("461101", user, plugin.V5Mutation{Kind: "enable"})
	ready()

	model.dns.unsafe.Store(true)
	deny("unsafe_model_DNS_rejected_through_real_Host_Egress", "model", request("model"))
	model.dns.unsafe.Store(false)
	// Binding CAS switches back, then unapproved revision cannot select a model.
	plan, _ = s.ai.Managed().PlanChatDisclosure(ctx, user, 0)
	localRevision := count(`SELECT revision FROM ai_model_profiles WHERE id='local-chat'`)
	if e = s.ai.Managed().Bind(ctx, admin, "local-chat", plan.BindingRevision, localRevision); e != nil {
		t.Fatal(e)
	}
	q = request("model")
	q.ProfileRevision++
	deny("stale_or_unapproved_Profile_revision_before_budget_or_network", "model", q)
	q = request("model")
	sql(`UPDATE ai_model_budgets SET quota=charged+reserved WHERE profile_id='local-chat'`)
	deny("managed_model_request_budget_exhaustion_zero_network", "model", q)
	sql(`UPDATE ai_model_budgets SET quota=100 WHERE profile_id='local-chat'`)
	sql(`CREATE FUNCTION owned14c_auth_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.command_code='plugin.ai.authorize' THEN RAISE EXCEPTION 'owned14c fault'; END IF; RETURN NEW; END $$;CREATE TRIGGER owned14c_auth_fault BEFORE INSERT ON platform_command_audits FOR EACH ROW EXECUTE FUNCTION owned14c_auth_fault()`)
	deny("required_plugin_model_attribution_audit_failure_before_budget_and_network", "model", request("model"))
	sql(`DROP TRIGGER owned14c_auth_fault ON platform_command_audits;DROP FUNCTION owned14c_auth_fault()`)
	// A real held HTTP model request, with cancellation receipt before its 4s timeout.
	pending := func(label, mode string, change func(), recover func()) {
		t.Helper()
		for len(model.started) > 0 {
			<-model.started
		}
		for len(model.cancelled) > 0 {
			<-model.cancelled
		}
		model.mode.Store("wait")
		q := request(mode)
		done := make(chan error, 1)
		go func() {
			out, e := invoke(ctx, user, mode, q)
			if out != "" {
				e = fmt.Errorf("late output released")
			}
			done <- e
		}()
		select {
		case <-model.started:
		case <-time.After(3 * time.Second):
			t.Fatal(label, "no real model request")
		}
		if out, e := invoke(ctx, user, "model", request("model")); e == nil || out != "" {
			t.Fatal("per-instance concurrency exceeded")
		}
		begin := time.Now()
		change()
		select {
		case e := <-done:
			if e == nil {
				t.Fatal(label, "released answer")
			}
		case <-time.After(2 * time.Second):
			t.Fatal(label, "did not cancel before model timeout")
		}
		select {
		case <-model.cancelled:
		case <-time.After(2*time.Second - time.Since(begin)):
			t.Fatal(label, "no HTTP peer cancellation receipt")
		}
		model.mode.Store("plain")
		if recover != nil {
			recover()
		}
		must("461101", user, plugin.V5Mutation{Kind: "enable"})
		ready()
		check(label)
	}
	pending("actual_inflight_model_AI_Feature_disable_cancels_network_and_discards_answer", "model", func() {
		_, e = s.features.Request(ai.FeatureID, false)
		if e != nil {
			t.Fatal(e)
		}
	}, func() {
		_, e = s.features.Request(ai.FeatureID, true)
		if e != nil {
			t.Fatal(e)
		}
	})
	pending("actual_inflight_public_RAG_Knowledge_disable_cancels_and_discards", "answer", func() {
		_, e = s.features.Request(knowledge.FeatureID, false)
		if e != nil {
			t.Fatal(e)
		}
	}, func() {
		_, e = s.features.Request(knowledge.FeatureID, true)
		if e != nil {
			t.Fatal(e)
		}
	})
	pending("actual_inflight_plugin_User_consent_withdrawal_cancels_model", "model", func() { must("461101", user, plugin.V5Mutation{Kind: "consent", Accepted: false}) }, func() {
		must("461101", user, plugin.V5Mutation{Kind: "consent", Accepted: true})
		must("461101", user, plugin.V5Mutation{Kind: "enable"})
	})
	pending("actual_inflight_AI_module_Stop_waits_model_cancellation_and_Start_reloads", "model", func() {
		if e = s.ai.Stop(ctx); e != nil {
			t.Fatal(e)
		}
	}, func() {
		if e = s.ai.Start(ctx); e != nil {
			t.Fatal(e)
		}
	})
	pending("actual_inflight_public_origin_withdrawal_cancels_current_RAG", "answer", func() {
		official.Status = "draft"
		var err error
		official, err = collections.SaveOfficial(ctx, official, admin)
		if err != nil {
			t.Fatal(err)
		}
		o2.Status = "draft"
		o2, err = collections.SaveOfficial(ctx, o2, admin)
		if err != nil {
			t.Fatal(err)
		}
	}, func() {
		official.Status = "published"
		var err error
		official, err = collections.SaveOfficial(ctx, official, admin)
		if err != nil {
			t.Fatal(err)
		}
		o2.Status = "published"
		o2, err = collections.SaveOfficial(ctx, o2, admin)
		if err != nil {
			t.Fatal(err)
		}
		end := time.Now().Add(10 * time.Second)
		for {
			page, e := s.knowledge.Retrieval().SearchPublic(ctx, knowledge.PublicSearchRequest{CollectionID: c.ID, Query: "PUBLIC14C_SOURCE", Page: 1, PageSize: 20})
			if e == nil && page.Total == 2 {
				break
			}
			if time.Now().After(end) {
				t.Fatal("index recovery", e)
			}
			time.Sleep(30 * time.Millisecond)
		}
	})
	pending("actual_inflight_Admin_Grant_revoke_cancels_plugin_and_model", "model", func() { must("", admin, plugin.V5Mutation{Kind: "revoke"}) }, func() {
		must("", admin, plugin.V5Mutation{Kind: "grant", Approved: true, Consumes: m.Consumes, Resources: p.Manifest.HostResources})
		must("", admin, plugin.V5Mutation{Kind: "enable"})
		must("461101", user, plugin.V5Mutation{Kind: "consent", Accepted: true})
		must("461101", user, plugin.V5Mutation{Kind: "enable"})
	})
	check("actual_per_instance_concurrency_limit_blocks_second_call_while_model_pending")
	trustBytes, err := os.ReadFile(filepath.Join(root, "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalogValue, _ := s.appContext.Lookup("plugin.v5.package-catalog")
	catalog := catalogValue.(*plugin.PluginV5PackageCatalog)
	pending("actual_current_publisher_trust_revocation_cancels_model_and_requires_fresh_publication", "model", func() {
		if e := catalog.SetTrust([]byte(`{"contract":"campusos.plugin-trust/v1","keys":[]}`)); e != nil {
			t.Fatal(e)
		}
	}, func() {
		if e := catalog.SetTrust(trustBytes); e != nil {
			t.Fatal(e)
		}
		must("", admin, plugin.V5Mutation{Kind: "enable"})
		must("461101", user, plugin.V5Mutation{Kind: "enable"})
	})

	pending("actual_inflight_binding_change_cancels_model_without_Profile_fallback", "model", func() {
		plan, e := s.ai.Managed().PlanChatDisclosure(ctx, user, 0)
		if e != nil {
			t.Fatal(e)
		}
		if e = s.ai.Managed().Bind(ctx, admin, "remote-chat", plan.BindingRevision, count(`SELECT revision FROM ai_model_profiles WHERE id='remote-chat'`)); e != nil {
			t.Fatal(e)
		}
	}, func() {
		plan, e := s.ai.Managed().PlanChatDisclosure(ctx, user, 0)
		if e != nil {
			t.Fatal(e)
		}
		if e = s.ai.Managed().Bind(ctx, admin, "local-chat", plan.BindingRevision, count(`SELECT revision FROM ai_model_profiles WHERE id='local-chat'`)); e != nil {
			t.Fatal(e)
		}
	})
	pending("actual_inflight_User_session_revocation_cancels_plugin_model_call", "model", func() {
		p, e := user(ctx)
		if e != nil {
			t.Fatal(e)
		}
		sql(`UPDATE sessions SET revoked_at=now(),revoke_reason='owned14c-joint' WHERE id=$1`, p.CredentialID)
	}, func() {
		u, e := identityrepo.NewPgUserRepository(pool).GetByID(ctx, "461101")
		if e != nil {
			t.Fatal(e)
		}
		issued, e := s.identity.Sessions().Issue(ctx, u, identityservice.SessionMetadata{DeviceID: "owned14c-joint-new"})
		if e != nil {
			t.Fatal(e)
		}
		claims, e := s.identity.JWTManager().VerifyAccessToken(issued.AccessToken)
		if e != nil {
			t.Fatal(e)
		}
		user = func(c context.Context) (identity.PrincipalContext, error) {
			return identity.ResolveUserSessionPrincipal(c, s.identity.Sessions(), s.identity.Sessions(), claims)
		}
	})

	// Settlement failure must not expose text or replay a model invocation.
	sql(`CREATE FUNCTION owned14c_settle_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.command_code='ai.call.settle' THEN RAISE EXCEPTION 'owned14c settle'; END IF; RETURN NEW; END $$;CREATE TRIGGER owned14c_settle_fault BEFORE INSERT ON platform_command_audits FOR EACH ROW EXECUTE FUNCTION owned14c_settle_fault()`)
	q = request("model")
	before := model.calls.Load()
	if text, e = invoke(ctx, user, "model", q); e == nil || text != "" || model.calls.Load() != before+1 {
		t.Fatal("required settlement failure", text, e)
	}
	sql(`DROP TRIGGER owned14c_settle_fault ON platform_command_audits;DROP FUNCTION owned14c_settle_fault()`)
	sql(`UPDATE ai_model_runs SET lease_until_ms=1 WHERE id=$1`, q.RunID)
	end = time.Now().Add(5 * time.Second)
	for count(`SELECT count(*) FROM ai_model_runs WHERE id=$1 AND state='unknown' AND charged_units=1`, q.RunID) != 1 {
		if time.Now().After(end) {
			t.Fatal("resident unknown settlement")
		}
		time.Sleep(30 * time.Millisecond)
	}
	if model.calls.Load() != before+1 {
		t.Fatal("lease replayed model")
	}
	check("required_settlement_failure_discards_text_resident_lease_charges_unknown_without_model_replay")
	if count(`SELECT count(*) FROM(SELECT to_jsonb(a)::text AS v FROM platform_command_audits a UNION ALL SELECT to_jsonb(o)::text FROM platform_outbox o UNION ALL SELECT to_jsonb(r)::text FROM ai_model_runs r UNION ALL SELECT to_jsonb(e)::text FROM ai_model_run_events e)x WHERE v LIKE '%EXPLICIT14C_PROMPT%' OR v LIKE '%MODEL_RESULT_14C%' OR v LIKE '%PRIVATE14C_DOCUMENT%' OR v LIKE '%OWNED14C_LOCAL_KEY%' OR v LIKE '%OWNED14C_REMOTE_KEY%'`) != 0 {
		t.Fatal("sensitive invocation material persisted")
	}
	check("no_prompt_answer_private_source_or_raw_model_keys_in_audits_Outbox_run_metadata")

	oldPort := port
	s = restart()
	value, _ = s.appContext.Lookup(ai.PortPluginText)
	port = value.(ai.PluginText)
	value, _ = s.appContext.Lookup(knowledge.PortPluginPublicSource)
	sourcePort = value.(knowledge.PluginPublicSource)
	ready()
	if out, e := oldPort.AnswerText(ctx, user, ai.PluginTextRequest{Publisher: m.Publisher, Key: m.Key, Version: m.Version, ContributionID: "chat-main", RequestID: "old", Prompt: "old", TimeoutMS: 1000}); e == nil || out.Text != "" {
		t.Fatal("old Port survived restart")
	}
	text, e = invoke(ctx, user, "model", request("model"))
	if e != nil || !strings.Contains(text, "MODEL_RESULT_14C") {
		t.Fatal("cold restart model", text, e)
	}
	waitIndex()
	if out, e := sourcePort.List(ctx, user, sourceReq); e != nil || len(out.Items) != 1 {
		t.Fatal("cold restart source Provider", out, e)
	}
	check("cold_full_production_graph_restart_preserves_grants_consents_profiles_budgets_and_public_sources_old_Port_closed")
	return map[string]any{"status": "passed", "actual_model_requests": model.calls.Load(), "actual_remote_dials": model.dial.calls.Load(), "SDK_package_digest": p.Digest, "actual_Knowledge_source_Provider": true, "consumption_and_contribution_separately_authorized": true, "source_or_prompt_persistence": false, "new_migrations": []string{}}
}
