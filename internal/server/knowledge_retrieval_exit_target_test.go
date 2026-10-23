package server

import (
	"context"
	"encoding/json"
	"fmt"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/modules/features/personaldocuments"
	platformroute "github.com/campusos/CampusOS/internal/platform/route"
	"github.com/campusos/CampusOS/internal/transport/httpapi"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type knowledgeGolden struct {
	Documents []struct{ Key, Kind, Title, Content string } `json:"documents"`
	Cases     []struct {
		ID, Question, Query string
		Relevant            []string `json:"relevant_source_keys"`
		Empty               bool     `json:"expect_empty"`
	} `json:"cases"`
	Thresholds struct {
		Top1      float64 `json:"min_top1_accuracy"`
		Recall    float64 `json:"min_recall_at_5"`
		MRR       float64 `json:"min_mrr_at_5"`
		Empty     float64 `json:"min_empty_accuracy"`
		Citations float64 `json:"min_current_citation_accuracy"`
	} `json:"thresholds"`
}

func TestKnowledgeRetrievalExitPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_EXIT_DSN")
	if dsn == "" {
		t.Skip("requires owned phase10 exit PostgreSQL")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_10_exit" {
		t.Fatal("owned exit DB required")
	}
	root := os.Getenv("CAMPUSOS_KNOWLEDGE_EXIT_ROOT")
	if !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-10-exit/") {
		t.Fatal("owned exit root required")
	}
	var corpus knowledgeGolden
	data, e := os.ReadFile(os.Getenv("CAMPUSOS_KNOWLEDGE_GOLDEN"))
	if e != nil || json.Unmarshal(data, &corpus) != nil || len(corpus.Cases) != 100 || len(corpus.Documents) != 30 {
		t.Fatal("frozen golden corpus required", e)
	}
	t.Chdir(root)
	ctx := t.Context()
	pool, e := pgxpool.NewWithConfig(ctx, parsed)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	scalar := func(sql string, args ...any) int64 {
		t.Helper()
		var n int64
		if e := pool.QueryRow(ctx, sql, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	wait := func(name string, seconds int, fn func() bool) {
		t.Helper()
		deadline := time.Now().Add(time.Duration(seconds) * time.Second)
		for time.Now().Before(deadline) {
			if fn() {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		// Persist metadata only, before owned resources are removed. No source
		// body or credential is needed to diagnose a missed readiness budget.
		var jobs []byte
		diagnostic, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if e := pool.QueryRow(diagnostic, `SELECT COALESCE(jsonb_agg(data),'[]'::jsonb) FROM knowledge_index_jobs`).Scan(&jobs); e == nil {
			_ = os.WriteFile(filepath.Join(root, "readiness-failure-jobs.json"), jobs, 0600)
		}
		t.Fatal("timeout: " + name)
	}
	exec(`INSERT INTO users(id,username,nickname,email) VALUES(450101,'exit-owner-a','Owner A','exit-a@example.test'),(450102,'exit-owner-b','Owner B','exit-b@example.test');
 INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(450110,'Exit group','exit-group','group','active');
 INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(450111,'Selected','exit-selected',450110,'board','active'),(450112,'Outside','exit-outside',450110,'board','active');
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(450111,'discussion',true),(450111,'article',true),(450112,'discussion',true);
 INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status)
 SELECT 450000+n,'校园服务档案 '||n,'校园服务背景资料。'||repeat('background filler ',60)||'编号 '||n,'markdown',450101+(n%2),'Owned exit',CASE WHEN n<=999 THEN 450111 ELSE 450112 END,'published','discussion','published','clear','active' FROM generate_series(1,10000)n;
 UPDATE threads SET publication_status='private',status='private',content='HTTP_PRIVATE_GOLDEN_SECRET' WHERE id BETWEEN 450990 AND 450999;`)
	cfg := config.Load()
	cfg.Database.DSN, cfg.Plugin.DataDir, cfg.Redis.Enabled = dsn, filepath.Join(root, "data/plugin_data"), false
	var server *Server
	var infra *infrastructureBootstrap
	var listener *httptest.Server
	start := func() (*Server, *infrastructureBootstrap, *httptest.Server) {
		t.Helper()
		s := New(cfg)
		i, e := s.startInfrastructure()
		if e != nil || i.database == nil || s.knowledge == nil || s.knowledge.Retrieval() == nil {
			t.Fatal("normal production composition required", e)
		}
		pluginHandler, e := s.pluginHTTPHandler()
		if e != nil {
			t.Fatal(e)
		}
		// The same Build call and real module handlers as runApplication; only
		// listener ownership differs so the test can choose an ephemeral port.
		router := httpapi.Build(httpapi.Dependencies{
			JWT: s.identity.JWTManager(), SessionVerifier: s.identity.Sessions(), Permissions: s.identity.Permissions(),
			AdminAccess: s.identity.AdminAccess(), AdminMFA: s.identity.MFA(), AdminJWT: s.identity.AdminDomainJWT(),
			AdminSessions: portadapt.NewAdminDomainSessionVerifier(s.identity.AdminDomainSessions()), AdminCodes: portadapt.NewAdminCodeChecker(s.identity.AdminDomainLifecycle()),
			Features: s.features, Feature: s.featureHandler, ModuleCatalog: s.moduleCatalog, PluginManager: s.manager,
			Identity: s.identity.Handlers(), Community: s.community.Handlers(), AcademicTerm: s.academicTerm.Handler(), UserStorage: s.storage.Handler(),
			Space: s.space.Handler(), Plugin: pluginHandler, PluginV5: s.pluginsV5Handler(), AI: s.ai.Handler(), Integration: s.integration.Handler(),
			Webhook: s.webhook.Handler(), MCP: s.mcp.Handler(), Message: s.message.Handler(), Homepage: s.appearance.HomepageHandler(), WebTheme: s.appearance.WebThemeHandler(),
			RichText: s.richtext.Handler(), Schedule: s.schedule.Handler(), PersonalDocuments: s.personalDocuments.Handler(), Knowledge: s.knowledge.Handler(),
			MutualAid: s.mutualAid.Handler(), Secondhand: s.secondhand.Handler(), PlatformLog: s.platformLog.Handler(), Moderation: s.moderation.Handler(),
			Reliability: s.reliability.Handler(), EmailDelivery: s.emailDelivery.Handler(), Metrics: i.metrics,
		})
		found := false
		for _, d := range router.RouteDescriptors() {
			if d.OperationCode == "http.knowledge.search.public" {
				if d.Method != http.MethodGet || d.Path != "/api/v1/knowledge/collections/:id/search" || d.Audience != platformroute.AudiencePublic || d.FeatureID != "knowledge" || d.PermissionCode != "" {
					t.Fatal("search route authority drift", d)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("production search route missing")
		}
		if e = s.identity.SyncRouteDescriptors(ctx, router.RouteDescriptors()); e != nil {
			t.Fatal("normal route catalog sync", e)
		}
		return s, i, httptest.NewServer(router)
	}
	server, infra, listener = start()
	defer func() {
		if listener != nil {
			listener.Close()
		}
		if infra != nil {
			infra.Stop()
		}
	}()
	if _, e = server.features.Request(knowledge.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = server.features.Request("personal-documents", true); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 200; i++ {
		if _, e = server.personalDocuments.Service().CreateText(ctx, fmt.Sprint(450101+i%2), personaldocuments.CreateRequest{Name: fmt.Sprintf("private-%d.txt", i), Format: "text", Content: "HTTP_PRIVATE_GOLDEN_SECRET"}); e != nil {
			t.Fatal(e)
		}
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account, sid := "adm.knowledge.exit", "as.knowledge.exit"
	if e = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "exit-admin", Email: "exit-admin@example.test", DisplayName: "Owned exit"}); e != nil {
		t.Fatal(e)
	}
	if e = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.knowledge.exit", AccountID: account, RoleID: "security_admin", GrantedBy: account}); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	if e = identityrepo.NewPgAdminSessionRepository(pool).CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: account, RefreshTokenDigest: strings.Repeat("c", 64), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	resolve := func(c context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(c, server.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: account, SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	c, e := server.knowledge.Collections().SaveCollection(ctx, knowledge.Collection{Title: "Campus golden public", Status: "active"}, resolve)
	if e != nil {
		t.Fatal(e)
	}
	sourceKey := map[string]string{}
	sourceIDs := map[string]string{}
	ordinal := 0
	for _, d := range corpus.Documents {
		if d.Kind == "official" {
			o, e := server.knowledge.Collections().SaveOfficial(ctx, knowledge.Official{Title: d.Title, Content: d.Content, Format: "text", Status: "published"}, resolve)
			if e != nil {
				t.Fatal(e)
			}
			sourceIDs[d.Key] = o.ID
			sourceKey[knowledge.OfficialKind+":"+o.ID] = d.Key
			continue
		}
		ordinal++
		id := fmt.Sprint(450000 + ordinal)
		sourceIDs[d.Key] = id
		if d.Kind == "article" {
			exec(`UPDATE threads SET title=$2,content='EXCERPT_NOT_GOLDEN',content_format='safe_html',thread_type='article' WHERE id=$1::bigint`, id, d.Title)
			exec(`INSERT INTO richtext_article_contents(id,thread_id,title,content_html,status,created_by) VALUES($1::bigint,$2::bigint,$3,$4,'published',450101)`, fmt.Sprint(460000+ordinal), id, d.Title, "<p>"+d.Content+"</p>")
			sourceKey[knowledge.PublicArticleKind+":"+id] = d.Key
		} else {
			exec(`UPDATE threads SET title=$2,content=$3 WHERE id=$1::bigint`, id, d.Title, d.Content)
			sourceKey[knowledge.PublicThreadKind+":"+id] = d.Key
		}
	}
	state, e := server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "board", SourceID: "450111", Active: true}, resolve)
	if e != nil {
		t.Fatal(e)
	}
	c = state.Collection
	state, e = server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "thread", SourceID: "450001", Active: true}, resolve)
	if e != nil {
		t.Fatal(e)
	}
	c = state.Collection
	for _, d := range corpus.Documents {
		if d.Kind == "official" {
			state, e = server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "official", SourceID: sourceIDs[d.Key], Active: true}, resolve)
			if e != nil {
				t.Fatal(e)
			}
			c = state.Collection
		}
	}
	ready := func() bool {
		return scalar(`SELECT count(*) FROM knowledge_index_jobs j JOIN knowledge_collections c ON c.id=j.id WHERE j.id=$1 AND j.state='idle' AND j.data->>'run_generation'='0' AND (j.data->>'ready_generation')::bigint>0 AND j.data->>'collection_version'=c.facts_version::text`, c.ID) == 1
	}
	began := time.Now()
	wait("full resident generation", 180, func() bool {
		return ready() && scalar(`SELECT count(*) FROM knowledge_index_documents WHERE collection_id=$1`, c.ID) == 1000
	})
	readySeconds := time.Since(began).Seconds()
	if scalar(`SELECT count(*) FROM threads`) != 10000 || scalar(`SELECT count(*) FROM personal_documents`) != 200 || scalar(`SELECT count(*) FROM plugin_installations`)+scalar(`SELECT count(*) FROM plugin_user_installations`) != 0 {
		t.Fatal("capacity fixtures drift")
	}
	type sample struct {
		Status int     `json:"status"`
		MS     float64 `json:"milliseconds"`
		Bytes  int     `json:"bytes"`
		Error  string  `json:"error,omitempty"`
	}
	client := &http.Client{Timeout: 8 * time.Second}
	get := func(path string) (json.RawMessage, sample) {
		begin := time.Now()
		r, e := client.Get(listener.URL + path)
		if e != nil {
			return nil, sample{MS: float64(time.Since(begin).Microseconds()) / 1000, Error: e.Error()}
		}
		defer r.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		s := sample{r.StatusCode, float64(time.Since(begin).Microseconds()) / 1000, len(raw), ""}
		if e != nil {
			s.Error = "body read"
		}
		if r.Header.Get("Cache-Control") != "private, no-store" {
			s.Error = "no-store missing"
		}
		if strings.Contains(string(raw), "HTTP_PRIVATE_GOLDEN_SECRET") || strings.Contains(string(raw), "EXCERPT_NOT_GOLDEN") {
			s.Error = "private/excerpt leak"
		}
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(raw, &env) != nil {
			s.Error = "invalid envelope"
		}
		return env.Data, s
	}
	path := "/api/v1/knowledge/collections/" + c.ID
	search := func(q string) (knowledge.PublicSearchPage, sample) {
		raw, s := get(path + "/search?query=" + url.QueryEscape(q) + "&page_size=20")
		var p knowledge.PublicSearchPage
		if s.Status == 200 && json.Unmarshal(raw, &p) != nil {
			s.Error = "invalid page"
		}
		return p, s
	}
	write := func(name string, v any) {
		b, _ := json.MarshalIndent(v, "", "  ")
		if e := os.WriteFile(filepath.Join(root, name), append(b, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if os.Getenv("CAMPUSOS_KNOWLEDGE_EXIT_MODE") == "baseline" {
		observations := map[string]any{}
		for _, q := range []string{"图书馆 周末", "校园服务", "不存在的紫色咖啡研究所"} {
			p, s := search(q)
			observations[q] = map[string]any{"request": s, "total": p.Total}
		}
		var hints int64
		begin := time.Now()
		if e = pool.QueryRow(ctx, `SELECT count(*) FROM knowledge_index_documents WHERE collection_id=$1 AND NOT EXISTS(SELECT 1 FROM unnest($2::text[])term WHERE strpos(lower((data->>'title')||' '||(SELECT string_agg(x,'') FROM jsonb_array_elements_text(data->'chunks')x)),term)=0)`, c.ID, []string{"图书馆", "周末"}).Scan(&hints); e != nil {
			t.Fatal(e)
		}
		write("target-proof.json", map[string]any{"status": "measured_baseline", "requests": observations, "parameterized_pg_hint": map[string]any{"candidate_count": hints, "milliseconds": float64(time.Since(begin).Microseconds()) / 1000, "authority": false}, "resident_index_ready_seconds": readySeconds, "projection_rows": 1000, "unique_sources": 999})
		return
	}
	// Typed chunk scanning must reject malformed JSON elements, never coerce
	// them to text. Restore this owned row before any benchmark samples.
	var malformedID string
	var savedDocument []byte
	if e = pool.QueryRow(ctx, `SELECT id,data FROM knowledge_index_documents WHERE collection_id=$1 ORDER BY id LIMIT 1`, c.ID).Scan(&malformedID, &savedDocument); e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"chunks", "title", "collection_version"} {
		value := "false"
		if field == "chunks" {
			value = "[true]"
		}
		exec(`UPDATE knowledge_index_documents SET data=jsonb_set(data,$2::text[],$3::jsonb) WHERE id=$1`, malformedID, []string{field}, value)
		_, malformedSample := search("校园服务")
		exec(`UPDATE knowledge_index_documents SET data=$2 WHERE id=$1`, malformedID, savedDocument)
		if malformedSample.Status != 503 || malformedSample.Error != "" {
			t.Fatal("malformed projection was coerced or released", field, malformedSample)
		}
	}
	// The owning trigger strips payload Version on every write. Verify that
	// authority guard instead of treating a normalized payload as corruption.
	exec(`UPDATE knowledge_index_documents SET data=jsonb_set(data,'{version}','false'::jsonb) WHERE id=$1`, malformedID)
	if scalar(`SELECT count(*) FROM knowledge_index_documents WHERE id=$1 AND NOT(data?'version')`, malformedID) != 1 {
		t.Fatal("payload Version was persisted as an authority field")
	}
	exec(`UPDATE knowledge_index_documents SET data=$2 WHERE id=$1`, malformedID, savedDocument)
	top, recall, mrr, empty, citations, totalCitations := 0.0, 0.0, 0.0, 0.0, 0, 0
	positive, negative := 0, 0
	rawCases := []any{}
	var firstHit knowledge.PublicSearchHit
	for _, test := range corpus.Cases {
		p, s := search(test.Query)
		if s.Status != 200 || s.Error != "" {
			write("quality-failure.json", map[string]any{"id": test.ID, "sample": s, "completed_cases": rawCases})
			t.Fatalf("golden %s: %+v", test.ID, s)
		}
		got := []string{}
		for _, h := range p.Items[:min(5, len(p.Items))] {
			got = append(got, sourceKey[h.Citation.Source.Kind+":"+h.Citation.Source.ID])
		}
		rank := 0
		for i, k := range got {
			for _, want := range test.Relevant {
				if k == want && rank == 0 {
					rank = i + 1
				}
			}
		}
		if test.Empty {
			negative++
			if p.Total == 0 && len(p.Items) == 0 {
				empty++
			}
		} else {
			positive++
			if rank == 1 {
				top++
			}
			if rank > 0 {
				recall++
				mrr += 1 / float64(rank)
			}
		}
		for _, hit := range p.Items[:min(5, len(p.Items))] {
			totalCitations++
			raw, cs := get(path + "/sources/" + hit.Citation.MemberID + "?revision=" + hit.Citation.VisibilityRevision)
			var v knowledge.VisibleSource
			if cs.Status == 200 && cs.Error == "" && json.Unmarshal(raw, &v) == nil && v.Source.Reference == hit.Citation.Source && v.Revision == hit.Citation.VisibilityRevision && strings.Contains(v.Source.Content, hit.Snippet) {
				citations++
			} else {
				t.Fatal("unverifiable golden citation")
			}
			firstHit = hit
		}
		rawCases = append(rawCases, map[string]any{"id": test.ID, "query": test.Query, "expected": test.Relevant, "top5": got, "rank": rank, "total": p.Total, "request": s})
	}
	metrics := map[string]float64{"top1_accuracy": top / float64(positive), "recall_at_5": recall / float64(positive), "mrr_at_5": mrr / float64(positive), "empty_accuracy": empty / float64(negative), "current_citation_accuracy": float64(citations) / float64(totalCitations)}
	write("quality-raw.json", map[string]any{"cases": rawCases, "metrics": metrics, "positive": positive, "negative": negative, "verified_citations": citations})
	if metrics["top1_accuracy"] < corpus.Thresholds.Top1 || metrics["recall_at_5"] < corpus.Thresholds.Recall || metrics["mrr_at_5"] < corpus.Thresholds.MRR || metrics["empty_accuracy"] < corpus.Thresholds.Empty || metrics["current_citation_accuracy"] < corpus.Thresholds.Citations {
		t.Fatal("frozen quality threshold failed", metrics)
	}
	rss := func() int64 {
		raw, e := os.ReadFile("/proc/self/status")
		if e != nil {
			t.Fatal(e)
		}
		peak := int64(0)
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "VmRSS:") || strings.HasPrefix(line, "VmHWM:") {
				n, _ := strconv.ParseInt(strings.Fields(line)[1], 10, 64)
				peak = max(peak, n*1024)
			}
		}
		return peak
	}
	capacity := map[string]any{}
	peak := rss()
	endpoints := map[string]string{"selective_search": path + "/search?query=" + url.QueryEscape("图书馆 周末"), "broad_search_first_page": path + "/search?query=" + url.QueryEscape("校园服务"), "exact_citation_reopen": path + "/sources/" + firstHit.Citation.MemberID + "?revision=" + firstHit.Citation.VisibilityRevision}
	for _, name := range []string{"selective_search", "broad_search_first_page", "exact_citation_reopen"} {
		endpoint := endpoints[name]
		var mu sync.Mutex
		records := make([]sample, 100)
		jobs := make(chan int)
		var wg sync.WaitGroup
		for n := 0; n < 8; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					raw, s := get(endpoint)
					if name != "exact_citation_reopen" && s.Status == 200 {
						var p knowledge.PublicSearchPage
						if json.Unmarshal(raw, &p) != nil || p.Total < 1 || len(p.Items) == 0 || name == "broad_search_first_page" && p.Total != 999 {
							s.Error = "count/page drift"
						}
					}
					records[i] = s
					mu.Lock()
					peak = max(peak, rss())
					mu.Unlock()
				}
			}()
		}
		for i := 0; i < 100; i++ {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		times := []float64{}
		fail := 0
		for _, s := range records {
			times = append(times, s.MS)
			if s.Status != 200 || s.Error != "" {
				fail++
			}
		}
		sort.Float64s(times)
		p95 := times[int(math.Ceil(.95*float64(len(times))))-1]
		capacity[name] = map[string]any{"raw": records, "p95_ms": p95, "failures": fail}
		write("capacity-raw.json", map[string]any{"endpoints": capacity, "peak_rss_bytes": peak})
		if fail != 0 || p95 > 1000 {
			t.Fatalf("capacity %s: p95 %.3f failures %d", name, p95, fail)
		}
	}
	if peak > 536870912 {
		t.Fatal("RSS budget exceeded", peak)
	}
	// Current facts are reread after the benchmark; no result or authority cache
	// may survive a new request, even when no invalidation event is published.
	exec(`UPDATE threads SET publication_status='private',status='private' WHERE id=$1::bigint`, sourceIDs[corpus.Documents[0].Key])
	p, s := search("图书馆 周末")
	if s.Status != 200 || p.Total != 0 {
		t.Fatal("fresh revoke leaked cached result")
	}
	_, s = get(path + "/sources/" + firstHit.Citation.MemberID + "?revision=" + strings.Repeat("0", 64))
	if s.Status != 409 {
		t.Fatal("stale citation accepted")
	}
	// Source handlers reopen the current reference and do not trust passed query claims.
	for _, suffix := range []string{"&provider=remote", "&principal=admin", "&collection_id=other"} {
		_, s = get(path + "/search?query=图书馆" + suffix)
		if s.Status != 400 {
			t.Fatal("scope override accepted")
		}
	}
	listener.Close()
	listener = nil
	infra.Stop()
	infra = nil
	restartBegin := time.Now()
	server, infra, listener = start()
	wait("restart ready", 120, ready)
	restartSeconds := time.Since(restartBegin).Seconds()
	p, s = search("图书馆 周末")
	if s.Status != 200 || p.Total != 0 {
		t.Fatal("restart revived private source")
	}
	if _, e = server.features.Request(knowledge.FeatureID, false); e != nil {
		t.Fatal(e)
	}
	_, s = get(path + "/search?query=公开")
	if s.Status != 503 {
		t.Fatal("Feature off bypass")
	}
	write("target-proof.json", map[string]any{"status": "passed", "quality": metrics, "quality_cases": len(corpus.Cases), "quality_positive": positive, "quality_negative": negative, "verified_citations": citations, "capacity": capacity, "peak_rss_bytes": peak, "resident_index_ready_seconds": readySeconds, "restart_to_ready_seconds": restartSeconds, "projection_rows": 1000, "unique_sources": 999, "origin_threads": 10000, "unindexed_private_documents": 200, "concurrency": 8, "measured_requests": 300, "external_plugins": 0, "actual_server_modules_router_loopback": true, "normal_resident_indexer": true, "fake_provider_or_manual_page": false, "new_request_revocation": "passed", "scope_override": "passed", "feature_stop_restart": "passed"})
}
