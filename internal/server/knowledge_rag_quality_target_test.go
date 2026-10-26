package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/modules/features/personaldocuments"
	"github.com/campusos/CampusOS/internal/transport/httpapi"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
)

// Real operator-owned loopback LLM, production composition and resident indexes.
// Frozen labels precede execution. This is extractive FAQ quality, not general QA.
func TestManagedRAGRealModelQualityPostgres(t *testing.T) {
	dsn, root := os.Getenv("CAMPUSOS_RAG_QUALITY_DSN"), os.Getenv("CAMPUSOS_RAG_QUALITY_ROOT")
	if dsn == "" {
		t.Skip("requires owned14b private/local RAG target")
	}
	pc, err := pgxpool.ParseConfig(dsn)
	keyRoot := os.Getenv("KNOWLEDGE_PRIVATE_KEY_DIR")
	if err != nil || pc.ConnConfig.Host != "127.0.0.1" || pc.ConnConfig.Database != "campusos_v12_14b_quality" || !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-14b-exit/") || !filepath.IsAbs(keyRoot) || !strings.Contains(filepath.ToSlash(keyRoot), "/.cache/v12-14b-exit/") {
		t.Fatal("refuse unowned private RAG target")
	}
	t.Chdir(root)
	ctx := t.Context()
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	sql := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	newID := func() string {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			t.Fatal(err)
		}
		return "ar_" + hex.EncodeToString(random[:])
	}
	checks := []string{}
	check := func(name string) { checks = append(checks, name); t.Log(name) }
	cfg := config.Load()
	cfg.Database.DSN, cfg.Plugin.DataDir, cfg.Redis.Enabled = dsn, filepath.Join(root, "data/plugin_data"), false
	cfg.AI = config.AIConfig{Enabled: true, BaseURL: "https://unselected.example.test/v1", Model: "unselected", APIKey: adminSPABrowserSecret(t), LocalBaseURL: os.Getenv("CAMPUSOS_RAG_QUALITY_MODEL_URL"), LocalModel: "campusos-rag-eval", LocalAPIKey: adminSPABrowserSecret(t), ActiveKeyID: "owned14bquality", EncryptionKeys: "owned14bquality:" + adminSPABrowserSecret(t), DefaultProfile: "local-chat", BudgetRequests: 300, MaxConcurrent: 2, MaxOutputTokens: 512, Timeout: "60s"}
	var s *Server
	var infra *infrastructureBootstrap
	var listener *httptest.Server
	start := func() {
		t.Helper()
		s = New(cfg)
		var err error
		infra, err = s.startInfrastructure()
		if err != nil || s.knowledge.PrivateRAG() == nil || s.knowledge.PrivateIndex() == nil {
			t.Fatal("normal private RAG composition", err)
		}
		pluginHandler, err := s.pluginHTTPHandler()
		if err != nil {
			t.Fatal(err)
		}
		router := httpapi.Build(httpapi.Dependencies{
			JWT: s.identity.JWTManager(), SessionVerifier: s.identity.Sessions(), Permissions: s.identity.Permissions(), AdminAccess: s.identity.AdminAccess(), AdminMFA: s.identity.MFA(), AdminJWT: s.identity.AdminDomainJWT(), AdminSessions: portadapt.NewAdminDomainSessionVerifier(s.identity.AdminDomainSessions()), AdminCodes: portadapt.NewAdminCodeChecker(s.identity.AdminDomainLifecycle()),
			Features: s.features, Feature: s.featureHandler, ModuleCatalog: s.moduleCatalog, PluginManager: s.manager, Identity: s.identity.Handlers(), Community: s.community.Handlers(), AcademicTerm: s.academicTerm.Handler(), UserStorage: s.storage.Handler(), Space: s.space.Handler(), Plugin: pluginHandler, PluginV5: s.pluginsV5Handler(), AI: s.ai.Handler(), Integration: s.integration.Handler(), Webhook: s.webhook.Handler(), MCP: s.mcp.Handler(), Message: s.message.Handler(), Homepage: s.appearance.HomepageHandler(), WebTheme: s.appearance.WebThemeHandler(), RichText: s.richtext.Handler(), Schedule: s.schedule.Handler(), PersonalDocuments: s.personalDocuments.Handler(), Knowledge: s.knowledge.Handler(), MutualAid: s.mutualAid.Handler(), Secondhand: s.secondhand.Handler(), PlatformLog: s.platformLog.Handler(), Moderation: s.moderation.Handler(), Reliability: s.reliability.Handler(), EmailDelivery: s.emailDelivery.Handler(), Metrics: infra.metrics,
		})
		listener = httptest.NewServer(router)
	}
	stop := func() {
		if listener != nil {
			listener.Close()
			listener = nil
		}
		if infra != nil {
			infra.Stop()
			infra = nil
		}
	}
	defer stop()
	start()
	sql(`INSERT INTO users(id,username,nickname,email) VALUES(814401,'owned14b-quality','Owned private RAG','owned14b-quality@example.test'),(814402,'owned14b-quality-other','Owned other private','owned14b-quality-other@example.test')`)
	issue := func(id string) (identityport.SessionPrincipalResolver, *auth.JWTClaims, string) {
		t.Helper()
		u, err := identityrepo.NewPgUserRepository(pool).GetByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		tokens, err := s.identity.Sessions().Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Owned private RAG target"})
		if err != nil {
			t.Fatal(err)
		}
		claims, err := s.identity.JWTManager().VerifyAccessToken(tokens.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		return func(c context.Context) (identityport.PrincipalContext, error) {
			return identityport.ResolveUserSessionPrincipal(c, s.identity.Sessions(), s.identity.Sessions(), claims)
		}, claims, tokens.AccessToken
	}
	user, _, _ := issue("814401")
	other, _, _ := issue("814402")
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	if err = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: "814401", Username: "owned14b-quality-admin", Email: "owned14b-quality-admin@example.test", DisplayName: "Private RAG Admin"}); err != nil {
		t.Fatal(err)
	}
	if err = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.owned14b-quality", AccountID: "814401", RoleID: "security_admin", GrantedBy: "814401"}); err != nil {
		t.Fatal(err)
	}
	now, sid := time.Now().UTC(), "as.owned14b-quality"
	if err = identityrepo.NewPgAdminSessionRepository(pool).CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: "814401", RefreshTokenDigest: adminSPABrowserSecret(t), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	admin := func(c context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(c, s.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: "814401", SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	for _, feature := range []string{knowledge.FeatureID, personaldocuments.FeatureID} {
		if _, err = s.features.Request(feature, true); err != nil {
			t.Fatal(err)
		}
	}
	waitReady := func(resolve identityport.SessionPrincipalResolver, id string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			rows, err := s.knowledge.PrivateIndex().List(ctx, resolve)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.ID == id && row.State == "ready" {
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("normal resident private index did not become ready")
	}

	fixturePath := os.Getenv("CAMPUSOS_RAG_QUALITY_GOLDEN")
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	fixtureSHA := fmt.Sprintf("%x", sha256.Sum256(raw))
	if fixtureSHA != "1b430a9886ea8b9ca4d0414627076a124f9d59fd1ff6767416f99c4c33ec3d55" {
		t.Fatal("frozen fixture changed")
	}
	var golden struct {
		Documents []struct {
			Key, Title, Content string
			Expected            string `json:"expected_factual_span"`
		} `json:"documents"`
		Cases []struct {
			ID, Query string
			Relevant  []string `json:"relevant_source_keys"`
			Empty     bool     `json:"expect_empty"`
		} `json:"cases"`
	}
	if json.Unmarshal(raw, &golden) != nil || len(golden.Documents) != 30 || len(golden.Cases) != 100 {
		t.Fatal("frozen quality set")
	}
	collection, err := s.knowledge.Collections().SaveCollection(ctx, knowledge.Collection{Title: "冻结合成问答样本", Status: "active"}, admin)
	if err != nil {
		t.Fatal(err)
	}
	type privateRef struct{ ID, Revision string }
	private := map[string]privateRef{}
	publicKeys := map[string]string{}
	privateKeys := map[string]string{}
	facts := map[string]string{}
	for _, d := range golden.Documents {
		official, e := s.knowledge.Collections().SaveOfficial(ctx, knowledge.Official{Title: d.Title, Content: d.Content, Format: "text", Status: "published"}, admin)
		if e != nil {
			t.Fatal(e)
		}
		state, e := s.knowledge.Collections().SaveSelection(ctx, collection.ID, collection.Version, knowledge.Selection{Kind: "official", SourceID: official.ID, Active: true}, admin)
		if e != nil {
			t.Fatal(e)
		}
		collection = state.Collection
		publicKeys[official.ID] = d.Key
		facts[d.Key] = d.Expected
		doc, e := s.personalDocuments.Service().CreateTextForPrincipal(ctx, user, personaldocuments.CreateRequest{Name: d.Key + ".txt", Format: "text", Content: d.Title + "\n" + d.Content})
		if e != nil {
			t.Fatal(e)
		}
		preview, e := s.knowledge.PrivateIndex().Preview(ctx, user, "personal_document", doc.ID)
		if e != nil {
			t.Fatal(e)
		}
		selected, e := s.knowledge.PrivateIndex().Select(ctx, user, preview.Kind, preview.ID, preview.Revision, true)
		if e != nil {
			t.Fatal(e)
		}
		private[d.Key] = privateRef{selected.ID, selected.Revision}
		privateKeys[selected.ID] = d.Key
		waitReady(user, selected.ID)
	}
	// Deliberately unrelated other-owner private material must never enter context.
	foreign, e := s.personalDocuments.Service().CreateTextForPrincipal(ctx, other, personaldocuments.CreateRequest{Name: "foreign.txt", Format: "text", Content: "OTHER_OWNER_QUALITY_NEVER_DISCLOSE 图书馆 开放时间 合成秘密"})
	if e != nil {
		t.Fatal(e)
	}
	fp, e := s.knowledge.PrivateIndex().Preview(ctx, other, "personal_document", foreign.ID)
	if e != nil {
		t.Fatal(e)
	}
	fs, e := s.knowledge.PrivateIndex().Select(ctx, other, fp.Kind, fp.ID, fp.Revision, true)
	if e != nil {
		t.Fatal(e)
	}
	waitReady(other, fs.ID)
	deadline := time.Now().Add(90 * time.Second)
	for {
		page, e := s.knowledge.Retrieval().SearchPublic(ctx, knowledge.PublicSearchRequest{CollectionID: collection.ID, Query: golden.Cases[0].Query, Page: 1, PageSize: 3})
		if e == nil && len(page.Items) > 0 && count(`SELECT count(*) FROM knowledge_index_jobs WHERE id=$1 AND state='idle' AND data->>'run_generation'='0' AND data->>'requested'=data->>'active_request' AND available_at_ms>$2`, collection.ID, time.Now().Add(time.Second).UnixMilli()) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("resident frozen public index unavailable", e)
		}
		time.Sleep(50 * time.Millisecond)
	}
	binding := func() int64 {
		var n int64
		if e := pool.QueryRow(ctx, `SELECT revision FROM ai_model_bindings WHERE name='chat'`).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	normalize := func(v string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || unicode.IsPunct(r) {
				return -1
			}
			return unicode.ToLower(r)
		}, v)
	}
	type metrics struct {
		Cases, Positive, Negative, Answered, Factual, Faithful, CitationCorrect, Refused, Malformed, Errors, Leaks int
		Seconds                                                                                                    float64
	}
	result := map[string]*metrics{"public": {}, "private": {}}
	log, err := os.OpenFile(filepath.Join(root, "quality-results.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	encode := json.NewEncoder(log)
	for _, scope := range []string{"public", "private"} {
		m := result[scope]
		for _, c := range golden.Cases {
			begin := time.Now()
			m.Cases++
			if c.Empty {
				m.Negative++
			} else {
				m.Positive++
			}
			status, answer, reason := "", "", ""
			keys, excerpts := []string{}, []string{}
			var callErr error
			if scope == "public" {
				readyDeadline := time.Now().Add(90 * time.Second)
				for count(`SELECT count(*) FROM knowledge_index_jobs WHERE id=$1 AND state='idle' AND data->>'requested'=data->>'active_request' AND available_at_ms>$2`, collection.ID, time.Now().Add(15*time.Second).UnixMilli()) != 1 {
					if time.Now().After(readyDeadline) {
						t.Fatal("public fixture current generation window unavailable")
					}
					time.Sleep(100 * time.Millisecond)
				}
				out, e := s.knowledge.PublicRAG().Answer(ctx, user, knowledge.PublicLocalQuestion{CollectionID: collection.ID, Question: c.Query, RunID: newID(), BindingRevision: binding()})
				callErr = e
				status, answer, reason = out.Status, out.Answer, out.Reason
				for _, cite := range out.Citations {
					keys = append(keys, publicKeys[cite.Citation.Source.ID])
					excerpts = append(excerpts, cite.Excerpt)
				}
			} else {
				key := golden.Documents[0].Key
				if len(c.Relevant) > 0 {
					key = c.Relevant[0]
				}
				ref := private[key]
				q := knowledge.PrivateLocalQuestion{SelectionID: ref.ID, Revision: ref.Revision, Question: c.Query, RunID: newID(), BindingRevision: binding()}
				review, e := s.knowledge.PrivateRAG().Prepare(ctx, user, q)
				callErr = e
				if e == nil && review.Status == "refused" {
					status, reason = review.Status, review.Reason
				} else if e == nil {
					if strings.Contains(review.Content, "OTHER_OWNER_QUALITY_NEVER_DISCLOSE") {
						m.Leaks++
					}
					out, e := s.knowledge.PrivateRAG().Answer(ctx, user, q, knowledge.PrivateLocalApproval{ID: review.ApprovalID, ReviewDigest: review.Digest, Consented: true})
					callErr = e
					status, answer, reason = out.Status, out.Answer, out.Reason
					for _, cite := range out.Citations {
						keys = append(keys, privateKeys[cite.SelectionID])
						excerpts = append(excerpts, cite.Excerpt)
						if cite.Revision != ref.Revision {
							m.Errors++
						}
					}
				}
			}
			factual, faithful, citation := false, false, false
			if callErr != nil {
				m.Errors++
				if strings.Contains(callErr.Error(), "invalid_answer") {
					m.Malformed++
				}
			}
			if strings.Contains(answer, "OTHER_OWNER_QUALITY_NEVER_DISCLOSE") {
				m.Leaks++
			}
			if c.Empty {
				if callErr == nil && status == "refused" && answer == "" && len(keys) == 0 {
					m.Refused++
				}
			} else if callErr == nil && status == "answered" && len(keys) > 0 {
				m.Answered++
				citation = true
				for _, k := range keys {
					found := false
					for _, expected := range c.Relevant {
						if k == expected {
							found = true
						}
					}
					if !found {
						citation = false
					}
				}
				for _, k := range c.Relevant {
					if strings.Contains(normalize(answer), normalize(facts[k])) {
						factual = true
					}
				}
				for _, excerpt := range excerpts {
					if strings.Contains(normalize(excerpt), normalize(answer)) {
						faithful = true
					}
				}
				if factual {
					m.Factual++
				}
				if faithful {
					m.Faithful++
				}
				if citation {
					m.CitationCorrect++
				}
			}
			elapsed := time.Since(begin).Seconds()
			m.Seconds += elapsed
			errText := ""
			if callErr != nil {
				errText = callErr.Error()
			}
			if encode.Encode(map[string]any{"scope": scope, "case": c.ID, "query": c.Query, "expected": c.Relevant, "negative": c.Empty, "status": status, "reason": reason, "answer": answer, "citation_source_keys": keys, "excerpts": excerpts, "factual": factual, "extractive_faithful": faithful, "citation_correct": citation, "error": errText, "seconds": elapsed}) != nil {
				t.Fatal("write actual quality result")
			}
			_ = log.Sync()
			t.Logf("%s %s status=%s factual=%v faithful=%v citation=%v seconds=%.2f error=%s", scope, c.ID, status, factual, faithful, citation, elapsed, errText)
		}
	}
	passed := true
	for _, m := range result {
		if m.Positive != 90 || m.Negative != 10 || float64(m.Factual)/90 < .9 || float64(m.Faithful)/90 < .9 || m.CitationCorrect != 90 || m.Refused != 10 || m.Malformed != 0 || m.Errors != 0 || m.Leaks != 0 {
			passed = false
		}
	}
	runs := count(`SELECT count(*) FROM ai_model_runs`)
	if runs != 180 {
		passed = false
	}
	proof := map[string]any{"status": "failed", "fixture_sha256": fixtureSHA, "metrics": result, "actual_model_runs": runs, "application_responses_mocked": false, "real_model": "Qwen2.5-3B-Instruct Q4_K_M", "frozen_thresholds_unchanged": true, "scope": "100 public +100 exact-owner private extractive Chinese FAQ; production composition, resident indexes and real loopback model; no production latency SLA"}
	if passed {
		proof["status"] = "passed"
	}
	b, _ := json.MarshalIndent(proof, "", "  ")
	if os.WriteFile(filepath.Join(root, "quality-proof.json"), append(b, '\n'), 0600) != nil {
		t.Fatal("write quality proof")
	}
	check("all_200_frozen_cases_executed_without_subset_selection")
	if !passed {
		t.Fatal("frozen real-model quality thresholds not met; inspect every raw sample")
	}
}
