package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/plugin"
	sdk "github.com/campusos/CampusOS/sdk/go"
	"github.com/jackc/pgx/v5/pgxpool"
)

func prepareKnowledgeExitTrust(t *testing.T, root string) ed25519.PrivateKey {
	t.Helper()
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	trust, _ := json.Marshal(plugin.PluginV5TrustStore{Contract: "campusos.plugin-trust/v1", Keys: []plugin.PluginV5TrustKey{{Publisher: "campusos", KeyID: "owned11", PublicKey: base64.StdEncoding.EncodeToString(pub), Status: "active"}}})
	path := filepath.Join(root, "trust.json")
	if os.WriteFile(path, trust, 0600) != nil {
		t.Fatal("trust output")
	}
	t.Setenv("CAMPUSOS_PLUGIN_V5_TRUST_STORE", path)
	return priv
}
func runKnowledgeMCPExit(t *testing.T, s *Server, pool *pgxpool.Pool, base string, machine *identityservice.IntegrationService, p identityrepo.IntegrationPrincipalRecord, resolve identity.SessionPrincipalResolver, c, foreign knowledge.Collection, source knowledge.VisibleSource, httpToken, root string, checks *[]string) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().Add(-time.Second).UnixMilli()
	expires := time.Now().Add(time.Hour).UnixMilli()
	issue := func() identityservice.IntegrationIssueResult {
		t.Helper()
		key, e := machine.IssueCredential(ctx, p.ID, identityservice.IntegrationIssueRequest{PrincipalVersion: p.Version, NotBeforeMS: now, ExpiresAtMS: expires, Rules: []identity.IntegrationAccessRule{{ID: "mcp", Channel: "integration_mcp", Endpoint: "knowledge.public.read", Action: "knowledge.source.read", ResourceKind: "public_collection", ResourceIDs: []string{c.ID}, Status: "active", NotBeforeMS: now, ExpiresAtMS: expires}}}, resolve)
		if e != nil {
			t.Fatal(e)
		}
		return key
	}
	key := issue()
	path := func(id string) string { return "/api/v1/integration/knowledge/collections/" + id + "/mcp" }
	client := &http.Client{Timeout: 8 * time.Second}
	requests := 0
	type reply struct {
		status int
		raw    []byte
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	send := func(method, id, token, raw string, headers http.Header) reply {
		q, e := http.NewRequestWithContext(ctx, method, base+path(id), strings.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		q.Header.Set("Authorization", "Integration "+token)
		q.Header.Set("Content-Type", "application/json")
		q.Header.Set("Accept", "application/json,text/event-stream")
		q.Header.Set("MCP-Protocol-Version", knowledge.PublicMCPVersion)
		for k, v := range headers {
			q.Header[k] = v
		}
		res, e := client.Do(q)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		wire, e := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		if e != nil {
			t.Fatal(e)
		}
		out := reply{status: res.StatusCode, raw: wire}
		if len(wire) > 0 && json.Unmarshal(wire, &out) != nil {
			t.Fatal("invalid MCP JSON")
		}
		return out
	}
	assert := func(r reply, status int, success bool) {
		t.Helper()
		requests++
		if r.status != status {
			t.Fatal("MCP transport", r.status, string(r.raw))
		}
		if success && (len(r.Result) == 0 || len(r.Error) > 0 || strings.Contains(string(r.Result), `"isError":true`)) {
			t.Fatal("MCP failure", string(r.raw))
		}
		if !success && strings.Contains(string(r.raw), "structuredContent") {
			t.Fatal("partial MCP data escaped")
		}
		if strings.Contains(string(r.raw), "PRIVATE_TARGET_") {
			t.Fatal("private MCP data")
		}
	}
	initialize := `{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"owned-wire-client","version":"1"}}}`
	r := send("POST", c.ID, key.Token, initialize, nil)
	assert(r, 200, true)
	if !strings.Contains(string(r.Result), knowledge.PublicMCPVersion) {
		t.Fatal("protocol negotiation")
	}
	assert(send("POST", c.ID, key.Token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil), 202, false)
	r = send("POST", c.ID, key.Token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	assert(r, 200, true)
	var tools struct {
		Tools []struct {
			Name string `json:"name"`
		}
	}
	_ = json.Unmarshal(r.Result, &tools)
	if len(tools.Tools) != 2 || tools.Tools[0].Name != "knowledge_search" || tools.Tools[1].Name != "knowledge_source_read" {
		t.Fatal("MCP tool allowlist")
	}
	search := `{"jsonrpc":"2.0","id":"read","method":"tools/call","params":{"name":"knowledge_search","arguments":{"query":"INTEGRATION_PUBLIC","page":1,"page_size":2}}}`
	r = send("POST", c.ID, key.Token, search, nil)
	assert(r, 200, true)
	var page struct {
		Structured knowledge.PublicSearchPage `json:"structuredContent"`
	}
	if json.Unmarshal(r.Result, &page) != nil || page.Structured.Total != 3 {
		t.Fatal("actual MCP search")
	}
	sourceBody, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "knowledge_source_read", "arguments": map[string]string{"member_id": source.MemberID, "revision": source.Revision}}})
	assert(send("POST", c.ID, key.Token, string(sourceBody), nil), 200, true)
	assert(send("GET", c.ID, key.Token, "", nil), 405, false)
	assert(send("POST", foreign.ID, key.Token, search, nil), 403, false)
	assert(send("POST", c.ID, httpToken, search, nil), 403, false)
	assert(send("POST", c.ID, key.Token, search, http.Header{"Origin": {"https://untrusted.test"}}), 403, false)
	assert(send("POST", c.ID, key.Token, search, http.Header{"MCP-Protocol-Version": {"2099-01-01"}}), 400, false)
	for _, body := range []string{`[]`, `{"jsonrpc":"2.0","id":1,"method":"ping","method":"tools/list"}`, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knowledge_search","arguments":{"query":"x","page":1,"page_size":2,"owner":"admin"}}}`} {
		r = send("POST", c.ID, key.Token, body, nil)
		assert(r, r.status, false)
		if len(r.Error) == 0 {
			t.Fatal("strict MCP input accepted")
		}
	}
	for _, method := range []string{"admin/reset", "resources/read", "sampling/createMessage"} {
		r = send("POST", c.ID, key.Token, `{"jsonrpc":"2.0","id":3,"method":"`+method+`"}`, nil)
		assert(r, 200, false)
		if !strings.Contains(string(r.Error), "-32601") {
			t.Fatal("unexpected protocol response")
		}
	}
	for _, tc := range []struct {
		body string
		code string
	}{
		{`{`, "-32700"},
		{`{"jsonrpc":"2.0","id":"bad-params","method":"tools/list","params":[]}`, "-32602"},
	} {
		r = send("POST", c.ID, key.Token, tc.body, nil)
		assert(r, 400, false)
		if !strings.Contains(string(r.Error), tc.code) {
			t.Fatal("JSON-RPC parse/params distinction", string(r.raw))
		}
		if strings.Contains(tc.body, "bad-params") && !strings.Contains(string(r.raw), `"id":"bad-params"`) {
			t.Fatal("lost request ID")
		}
	}
	if _, e := s.features.Request(knowledge.FeatureID, false); e != nil {
		t.Fatal(e)
	}
	assert(send("POST", c.ID, key.Token, search, nil), 503, false)
	if _, e := s.features.Request(knowledge.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	assert(send("POST", c.ID, key.Token, search, nil), 200, true)
	*checks = append(*checks, "MCP pinned2025 Streamable HTTP initialize/initialized/list/search/source, direct JSON-RPC, exact two read-only tools, GET405, no Admin/private/sampling, malformed/scope/channel/Origin/version rejection")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	for _, table := range []string{"platform_command_audits", "platform_outbox"} {
		condition := "NEW.command_code='identity.integration.read'"
		if table == "platform_outbox" {
			condition = "NEW.event_type='integration.accessed'"
		}
		exec(`CREATE FUNCTION owned11_mcp_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'internal MCP receipt failure'; END $$`)
		exec(`CREATE TRIGGER owned11_mcp_fail BEFORE INSERT ON ` + table + ` FOR EACH ROW WHEN (` + condition + `) EXECUTE FUNCTION owned11_mcp_fail()`)
		assert(send("POST", c.ID, key.Token, search, nil), 200, false)
		exec(`DROP TRIGGER owned11_mcp_fail ON ` + table)
		exec(`DROP FUNCTION owned11_mcp_fail()`)
	}
	*checks = append(*checks, "MCP actual required audit and Outbox failure discard the complete result")
	// Block the required receipt after current source reads. Cancellation must not
	// wait for the principal row held by this same request.
	conn, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	if _, e = conn.Exec(ctx, `SELECT pg_advisory_lock(451199)`); e != nil {
		t.Fatal(e)
	}
	exec(`CREATE FUNCTION owned11_mcp_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(451199); RETURN NEW; END $$`)
	exec(`CREATE TRIGGER owned11_mcp_wait BEFORE INSERT ON platform_command_audits FOR EACH ROW WHEN (NEW.command_code='identity.integration.read') EXECUTE FUNCTION owned11_mcp_wait()`)
	done := make(chan reply, 1)
	go func() { done <- send("POST", c.ID, key.Token, search, nil) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var n int
		if e = pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND objid=451199 AND NOT granted`).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("MCP barrier deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert(send("POST", c.ID, key.Token, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"read","reason":"operator cancelled"}}`, nil), 202, false)
	select {
	case r = <-done:
		assert(r, 200, false)
	case <-time.After(2 * time.Second):
		t.Fatal("cancel waited for own authority lock")
	}
	_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock(451199)`)
	exec(`DROP TRIGGER owned11_mcp_wait ON platform_command_audits`)
	exec(`DROP FUNCTION owned11_mcp_wait()`)
	*checks = append(*checks, "MCP cancellation during real PostgreSQL audit barrier uses nonlocking current-key verification, interrupts the transaction and emits no data")
	original := key.Credential.Rules
	removed, e := machine.UpdateCredential(ctx, p.ID, key.Credential.ID, key.Credential.Version, []identity.IntegrationAccessRule{}, false, resolve)
	if e != nil {
		t.Fatal(e)
	}
	assert(send("POST", c.ID, key.Token, search, nil), 403, false)
	key.Credential, e = machine.UpdateCredential(ctx, p.ID, removed.ID, removed.Version, original, false, resolve)
	if e != nil {
		t.Fatal(e)
	}
	assert(send("POST", c.ID, key.Token, search, nil), 200, true)
	_, e = machine.UpdateCredential(ctx, p.ID, key.Credential.ID, key.Credential.Version, original, true, resolve)
	if e != nil {
		t.Fatal(e)
	}
	assert(send("POST", c.ID, key.Token, search, nil), 401, false)
	*checks = append(*checks, "MCP current key scope shrink/restore and revocation take effect without an allow cache")
	proof, _ := json.MarshalIndent(map[string]any{"status": "passed", "http_requests": requests, "protocol": knowledge.PublicMCPVersion, "actual_pg_audit_cancel": true, "official_sdk_client": false}, "", "  ")
	if os.WriteFile(filepath.Join(root, "mcp-proof.json"), proof, 0600) != nil {
		t.Fatal("MCP proof")
	}
}

func runKnowledgeHostExit(t *testing.T, s *Server, pool *pgxpool.Pool, private ed25519.PrivateKey, admin identity.SessionPrincipalResolver, c, foreign knowledge.Collection, source knowledge.VisibleSource, root string, checks *[]string) {
	t.Helper()
	ctx := t.Context()
	raw, e := os.ReadFile(os.Getenv("CAMPUSOS_KNOWLEDGE_EXIT_TEMPLATE"))
	if e != nil {
		t.Fatal(e)
	}
	var m plugin.PluginV5Manifest
	if json.Unmarshal(raw, &m) != nil {
		t.Fatal("template")
	}
	m.Backend.Artifact.Image = os.Getenv("CAMPUSOS_KNOWLEDGE_EXIT_IMAGE")
	for n := range m.Consumes {
		m.Consumes[n].Scope.CollectionIDs = []string{c.ID}
	}
	packageDir := filepath.Join(root, "package")
	if os.Mkdir(packageDir, 0700) != nil {
		t.Fatal("package dir")
	}
	raw, _ = json.Marshal(m)
	if os.WriteFile(filepath.Join(packageDir, "plugin.json"), raw, 0600) != nil {
		t.Fatal("manifest")
	}
	archive, p, e := plugin.BuildPluginV5Package(packageDir)
	if e != nil {
		t.Fatal(e)
	}
	envelope, e := plugin.SignPluginV5Package(archive, "owned11", private)
	if e != nil {
		t.Fatal(e)
	}
	for name, wire := range map[string][]byte{"reader.tar.gz": archive, "reader.release.json": envelope} {
		if os.WriteFile(filepath.Join(root, name), wire, 0600) != nil {
			t.Fatal("package evidence")
		}
	}
	platform := s.pluginsV5Handler().Platform
	coordinate := plugin.V5Coordinate{Publisher: m.Publisher, Key: m.Key, Version: m.Version}
	if _, e = platform.Import(ctx, archive, envelope, "owned11-import", admin); e != nil {
		t.Fatal(e)
	}
	sequence := 0
	command := func(owner string, resolve identity.SessionPrincipalResolver, r plugin.V5Mutation) plugin.V5Operation {
		t.Helper()
		d, e := platform.Detail(ctx, coordinate, owner == "", resolve)
		if e != nil {
			t.Fatal(e)
		}
		sequence++
		r.IdempotencyKey = fmt.Sprintf("owned11-%d", sequence)
		r.Version = m.Version
		if d.Installation != nil {
			r.ExpectedGeneration = d.Installation.Generation
		}
		if d.Grant != nil {
			r.ExpectedRevision = d.Grant.Revision
		}
		if r.Kind == "consent" {
			r.PurposeHash = d.PurposeHash
			if d.Consent != nil {
				r.ExpectedRevision = d.Consent.Revision
			} else {
				r.ExpectedRevision = 0
			}
		}
		op, e := platform.Mutate(ctx, m.Publisher, m.Key, r, owner == "", resolve)
		if e != nil {
			t.Fatal("command", r.Kind, e)
		}
		return op
	}
	wait := func(owner string, resolve identity.SessionPrincipalResolver) {
		t.Helper()
		end := time.Now().Add(25 * time.Second)
		for time.Now().Before(end) {
			d, e := platform.Detail(ctx, coordinate, owner == "", resolve)
			if e == nil && d.Installation != nil && d.Installation.Observed == "ready" {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		d, _ := platform.Detail(ctx, coordinate, owner == "", resolve)
		t.Fatal("normal container coordinator readiness", d.Installation)
	}
	command("", admin, plugin.V5Mutation{Kind: "install"})
	command("", admin, plugin.V5Mutation{Kind: "grant", Approved: true, Consumes: p.Manifest.Consumes, Resources: p.Manifest.HostResources})
	command("", admin, plugin.V5Mutation{Kind: "enable"})
	wait("", admin)
	users := identityrepo.NewPgUserRepository(pool)
	u, e := users.GetByID(ctx, "451101")
	if e != nil {
		t.Fatal(e)
	}
	session, e := s.identity.Sessions().Issue(ctx, u, identityservice.SessionMetadata{DeviceID: "owned11-sdk"})
	if e != nil {
		t.Fatal(e)
	}
	claims, e := s.identity.JWTManager().VerifyAccessToken(session.AccessToken)
	if e != nil {
		t.Fatal(e)
	}
	user := func(ctx context.Context) (identity.PrincipalContext, error) {
		return identity.ResolveUserSessionPrincipal(ctx, s.identity.Sessions(), s.identity.Sessions(), claims)
	}
	command("451101", user, plugin.V5Mutation{Kind: "install"})
	command("451101", user, plugin.V5Mutation{Kind: "enable"})
	wait("451101", user)
	value, ok := s.appContext.Lookup("plugin.v5.runtime")
	hub, typed := value.(*plugin.V5ManagedRuntime)
	if !ok || !typed {
		t.Fatal("normal managed Runtime")
	}
	var binding plugin.V5ManagedBinding
	publicationDeadline := time.Now().Add(25 * time.Second)
	for {
		binding, e = hub.Select(ctx, "451101", plugin.V5ContributionQuery{Point: plugin.V5AIChat, ContractVersion: "v1", Capability: "text"}, nil)
		if e == nil {
			break
		}
		if time.Now().After(publicationDeadline) {
			t.Fatal("normal managed publication deadline", e)
		}
		time.Sleep(25 * time.Millisecond)
	}
	consumer := plugin.V5Consumer{Feature: s.features, FeatureID: knowledge.FeatureID}
	invoke := func(intent any) (sdk.V5ChatResponse, error) {
		prompt, _ := json.Marshal(intent)
		q := sdk.V5ChatRequest{RequestID: "owned11-sdk", ContributionID: binding.Descriptor().ID, InstanceGeneration: binding.Descriptor().InstanceGeneration, DeadlineMS: 4500, Prompt: string(prompt), ContextClass: "none"}
		body, _ := json.Marshal(q)
		call, _ := json.Marshal(sdk.V5DispatchCall{Contract: "campusos.plugin-dispatch/v1", RequestID: q.RequestID, Target: sdk.V5DispatchTarget{Kind: "contribution", ID: q.ContributionID}, InstanceGeneration: q.InstanceGeneration, DeadlineMS: time.Now().Add(4500 * time.Millisecond).UnixMilli(), RequestBytes: int64(len(body)), ResponseLimitBytes: 65536})
		wire, e := hub.Invoke(ctx, consumer, binding, call, body, user)
		var out sdk.V5ChatResponse
		if e == nil {
			e = json.Unmarshal(wire, &out)
		}
		return out, e
	}
	search := map[string]any{"mode": "search", "collection_id": c.ID, "query": "INTEGRATION_PUBLIC", "page": 1, "page_size": 2}
	out, e := invoke(search)
	if e != nil {
		t.Fatal("native SDK search", e)
	}
	var page sdk.V5KnowledgeSearchPage
	if json.Unmarshal([]byte(out.Text), &page) != nil || page.Total != 3 || len(page.Items) != 2 {
		t.Fatal("SDK search DTO", out.Text)
	}
	read := map[string]any{"mode": "source", "collection_id": c.ID, "member_id": source.MemberID, "revision": source.Revision}
	out, e = invoke(read)
	if e != nil {
		t.Fatal("native SDK source", e)
	}
	var reopened sdk.V5KnowledgeSource
	if json.Unmarshal([]byte(out.Text), &reopened) != nil || reopened.Revision != source.Revision || !strings.Contains(reopened.Source.Content, "INTEGRATION_PUBLIC_THREAD") {
		t.Fatal("SDK current citation", out.Text)
	}
	*checks = append(*checks, "independently built public Go SDK example, signed package, normal durable import/Grant/user-session install, actual Docker Container coordinator, managed Host search and current citation DTOs")
	search["collection_id"] = foreign.ID
	out, e = invoke(search)
	if e == nil || out.Text != "" {
		t.Fatal("foreign Host collection")
	}
	search["collection_id"] = c.ID
	*checks = append(*checks, "actual isolated SDK guest cannot read another collection outside declaration and exact typed Grant")
	if _, e = pool.Exec(ctx, `UPDATE threads SET publication_status='private',status='private',updated_at=clock_timestamp() WHERE id=451201`); e != nil {
		t.Fatal(e)
	}
	out, e = invoke(read)
	if e == nil || out.Text != "" {
		t.Fatal("private current source returned through SDK")
	}
	out, e = invoke(search)
	if e != nil {
		t.Fatal("SDK private prefilter", e)
	}
	if json.Unmarshal([]byte(out.Text), &page) != nil || page.Total != 2 || strings.Contains(out.Text, "INTEGRATION_PUBLIC_THREAD") {
		t.Fatal("SDK stale projection leak", out.Text)
	}
	if _, e = pool.Exec(ctx, `UPDATE threads SET publication_status='published',status='published',updated_at=clock_timestamp() WHERE id=451201`); e != nil {
		t.Fatal(e)
	}
	*checks = append(*checks, "actual source turned private while projection remains: SDK citation fails closed, search totals filter current origin before pagination")
	if _, e = s.features.Request(knowledge.FeatureID, false); e != nil {
		t.Fatal(e)
	}
	out, e = invoke(search)
	if e == nil || out.Text != "" {
		t.Fatal("Feature disabled Host")
	}
	if _, e = s.features.Request(knowledge.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	command("", admin, plugin.V5Mutation{Kind: "revoke"})
	out, e = invoke(search)
	if e == nil || out.Text != "" {
		t.Fatal("revoked Grant reached guest")
	}
	*checks = append(*checks, "actual owning Feature disable and durable Grant revoke fence SDK calls and discard output")
	proof, _ := json.MarshalIndent(map[string]any{"status": "passed", "managed_container": true, "sdk_only_author_build": true, "package_digest": p.Digest, "image": m.Backend.Artifact.Image, "external_ai_provider": false, "user_owner": "451101", "calls": 7}, "", "  ")
	if os.WriteFile(filepath.Join(root, "host-sdk-proof.json"), proof, 0600) != nil {
		t.Fatal("SDK proof")
	}
}
