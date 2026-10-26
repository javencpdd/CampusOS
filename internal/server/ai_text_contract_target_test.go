package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/features/ai"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAITextContractPostgres(t *testing.T) {
	dsn, root := os.Getenv("CAMPUSOS_AI_TEXT_DSN"), os.Getenv("CAMPUSOS_AI_TEXT_ROOT")
	if dsn == "" {
		t.Skip("requires owned14a text-contract PostgreSQL target")
	}
	pc, e := pgxpool.ParseConfig(dsn)
	if e != nil || pc.ConnConfig.Host != "127.0.0.1" || pc.ConnConfig.Database != "campusos_v12_14a_text" || !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-14a/") {
		t.Fatal("refuse non-owned14a target")
	}
	t.Chdir(root)
	ctx := t.Context()
	pool, e := pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	checks := []string{}
	check := func(s string) { checks = append(checks, s) }
	key := adminSPABrowserSecret(t)
	prompt := "SYNTHETIC_PRIVATE_PROMPT 校园图书馆"
	var mode atomic.Value
	mode.Store("text")
	var calls, escaped atomic.Int32
	started := make(chan struct{}, 1)
	var peer *httptest.Server
	peer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			escaped.Add(1)
			w.WriteHeader(500)
			return
		}
		calls.Add(1)
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("actual fixed POST/auth failed")
			w.WriteHeader(403)
			return
		}
		var request struct {
			Model       string       `json:"model"`
			Messages    []ai.Message `json:"messages"`
			MaxTokens   int          `json:"max_tokens"`
			Temperature *float64     `json:"temperature"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || request.Model != "owned-text-model" || len(request.Messages) != 1 || request.Messages[0].Content != prompt {
			t.Error("wire request changed or leaked metadata")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch mode.Load().(string) {
		case "error":
			w.WriteHeader(503)
			_, _ = w.Write([]byte("provider error " + key + " " + prompt))
			return
		case "redirect":
			w.Header().Set("Location", peer.URL+"/must-not-follow")
			w.WriteHeader(307)
			return
		case "cancel":
			started <- struct{}{}
			<-r.Context().Done()
			return
		case "tools":
			_, _ = w.Write([]byte(`{"model":"owned-text-model","choices":[{"message":{"role":"assistant","content":"UNTRUSTED_TOOL_RESULT","tool_calls":[{"name":"unsafe"}]},"finish_reason":"stop"}]}`))
			return
		case "oversize":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"TRUNCATED_RESULT"},"finish_reason":"stop"}]}` + strings.Repeat(" ", ai.MaxChatResponseBytes)))
			return
		}
		_, _ = w.Write([]byte(`{"id":"owned-response","object":"chat.completion","created":1,"model":"owned-text-model","choices":[{"index":0,"message":{"role":"assistant","content":"协议测试回复 <img src=\"/never-fetch\">","refusal":null,"annotations":[]},"finish_reason":"stop","logprobs":null}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5},"system_fingerprint":null}`))
	}))
	defer peer.Close()
	cfg := config.Load()
	cfg.Database.DSN = dsn
	cfg.Plugin.DataDir = filepath.Join(root, "data/plugin_data")
	cfg.Redis.Enabled = false
	cfg.AI = config.AIConfig{Enabled: true, Provider: "openai-compatible", BaseURL: peer.URL + "/v1", APIKey: key, Model: "owned-text-model", Timeout: "5s", MaxConcurrent: 4, MaxRequestsPerMinute: 100}
	s := New(cfg)
	infra, e := s.startInfrastructure()
	if e != nil {
		t.Fatal(e)
	}
	defer infra.Stop()
	if s.ai == nil || s.ai.Service() == nil {
		t.Fatal("normal production AI module required")
	}
	service := s.ai.Service()
	status := service.Status()
	if !status.Ready || status.Definition == nil || status.Definition.Version != ai.TextContractVersion || status.Definition.Tools || status.Definition.Streaming {
		t.Fatal("actual current definition missing")
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), key) || calls.Load() != 0 {
		t.Fatal("status leaked credentials or auto-executed")
	}
	check("normal_production_module_current_text_definition_no_auto_call")
	var external int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM plugin_installations)+(SELECT count(*) FROM plugin_user_installations)`).Scan(&external); e != nil || external != 0 {
		t.Fatal("zero external plugins required", e)
	}
	check("builtin_text_path_independent_of_external_plugins")
	var invalid ai.ChatRequest
	if json.Unmarshal([]byte(`{"messages":[{"role":"user","content":"x"}],"tools":[{"name":"unsafe"}]}`), &invalid) == nil {
		t.Fatal("unknown modality silently dropped")
	}
	if out, e := service.ChatCompletion(ctx, ai.ChatRequest{Messages: []ai.Message{{Role: "tool", Content: prompt}}}); out != nil || e == nil || calls.Load() != 0 {
		t.Fatal("unsupported typed role executed")
	}
	check("closed_JSON_and_typed_unknown_modalities_reject_before_network")
	request := ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: prompt}}, MaxTokens: 32}
	out, e := service.ChatCompletion(ctx, request)
	if e != nil || out == nil || out.Content != "协议测试回复 <img src=\"/never-fetch\">" || out.Usage.TotalTokens != 5 || calls.Load() != 1 {
		t.Fatal("actual text adapter failed", e)
	}
	check("real_loopback_HTTP_POST_model_auth_exact_text_and_usage_no_execution_of_text")
	for _, name := range []string{"error", "tools", "oversize", "redirect"} {
		mode.Store(name)
		before := calls.Load()
		out, e := service.ChatCompletion(ctx, request)
		if e == nil || out != nil || calls.Load() != before+1 {
			t.Fatal("unsafe provider outcome or blind retry", name, e)
		}
		if strings.Contains(e.Error(), key) || strings.Contains(e.Error(), prompt) || strings.Contains(e.Error(), "UNTRUSTED") {
			t.Fatal("raw provider data exposed")
		}
	}
	if escaped.Load() != 0 {
		t.Fatal("redirect executed")
	}
	check("HTTP_error_tool_output_oversize_and_redirect_fail_without_retry_or_follow")
	var count, success, unsafe int
	if e = pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='success'),count(*) FILTER(WHERE error_message LIKE '%SYNTHETIC_PRIVATE_PROMPT%' OR error_message LIKE '%UNTRUSTED%' OR error_message LIKE '%'||$1||'%') FROM ai_call_logs`, key).Scan(&count, &success, &unsafe); e != nil || count != 5 || success != 1 || unsafe != 0 {
		t.Fatal("real PostgreSQL safe call logs required", count, success, unsafe, e)
	}
	logs, e := service.ListLogs(ctx, 100)
	if e != nil {
		t.Fatal(e)
	}
	logJSON, _ := json.Marshal(logs)
	if strings.Contains(string(logJSON), key) || strings.Contains(string(logJSON), prompt) {
		t.Fatal("secret/prompt in current log view")
	}
	check("real_PostgreSQL_logs_only_safe_host_labels_usage_status_and_error_codes")
	mode.Store("cancel")
	cancelCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	before := calls.Load()
	go func() {
		out, e := service.ChatCompletion(cancelCtx, request)
		if out != nil {
			done <- errors.New("late result delivered")
			return
		}
		done <- e
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("actual request not observed")
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal("current cancellation lost", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("actual HTTP cancellation failed")
	}
	if calls.Load() != before+1 {
		t.Fatal("cancelled unknown outcome retried")
	}
	check("actual_inflight_HTTP_cancellation_propagates_no_result_or_automatic_retry")
	before = calls.Load()
	if _, e = service.ChatCompletion(cancelCtx, request); !errors.Is(e, context.Canceled) || calls.Load() != before {
		t.Fatal("pre-cancel executed")
	}
	check("already_cancelled_request_never_connects")
	proof := map[string]any{"status": "passed", "checks": checks, "check_count": len(checks), "actual_production_server_modules": true, "actual_postgresql": true, "actual_loopback_http_adapter": true, "provider_fixture": "owned deterministic HTTP protocol peer, not an LLM or public egress", "http_calls": calls.Load(), "redirect_target_calls": escaped.Load(), "external_plugins": external, "mocked_API_data": false, "standalone_cmd_server": false, "browser": false, "private_RAG_or_egress_authorization": false}
	encoded, _ := json.MarshalIndent(proof, "", "  ")
	if e = os.WriteFile(filepath.Join(root, "ai-text-proof.json"), append(encoded, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
}
