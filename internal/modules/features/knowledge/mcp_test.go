package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/gin-gonic/gin"
)

type mcpGateway struct{ integrationReadFunc }

func (m mcpGateway) CheckForIntegration(ctx context.Context, token string, target identity.IntegrationAccessTarget) error {
	_, e := m.ReadForIntegration(ctx, token, target, func(context.Context) (any, error) { return nil, nil })
	return e
}
func TestPublicMCPProtocolCurrentSearchSourceAndClosedTools(t *testing.T) {
	f, _, retrieval, c := retrievalFixture(t, 1)
	h := NewHandler(f.s, noAdminSession{}, retrieval)
	h.machine = mcpGateway{func(ctx context.Context, token string, target identity.IntegrationAccessTarget, read func(context.Context) (any, error)) (any, error) {
		if token != "mcp-test" || target.Channel != "integration_mcp" || target.Endpoint != "knowledge.public.read" || target.Resource.ID != c.ID {
			t.Fatal("scope drift", target)
		}
		return read(ctx)
	}}
	r := gin.New()
	r.POST("/collections/:id/mcp", h.IntegrationMCP)
	r.GET("/collections/:id/mcp", h.IntegrationMCP)
	request := func(method, raw string, status int) map[string]json.RawMessage {
		t.Helper()
		q := httptest.NewRequest(method, "/collections/"+c.ID+"/mcp", strings.NewReader(raw))
		q.Header.Set("Authorization", "Integration mcp-test")
		q.Header.Set("Content-Type", "application/json")
		q.Header.Set("Accept", "application/json, text/event-stream")
		q.Header.Set("MCP-Protocol-Version", PublicMCPVersion)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		if w.Code != status {
			t.Fatal(w.Code, w.Body.String())
		}
		var out map[string]json.RawMessage
		if w.Body.Len() > 0 && json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal(w.Body.String())
		}
		return out
	}
	out := request("POST", `{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, 200)
	if !strings.Contains(string(out["result"]), PublicMCPVersion) || out["data"] != nil {
		t.Fatal("not direct MCP", out)
	}
	request("POST", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, 202)
	out = request("POST", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, 200)
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		}
	}
	_ = json.Unmarshal(out["result"], &list)
	if len(list.Tools) != 2 || list.Tools[0].Name != "knowledge_search" || list.Tools[1].Name != "knowledge_source_read" {
		t.Fatal(string(out["result"]))
	}
	out = request("POST", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"knowledge_search","arguments":{"query":"公开文本","page":1,"page_size":2}}}`, 200)
	var result struct {
		IsError    bool             `json:"isError"`
		Structured PublicSearchPage `json:"structuredContent"`
	}
	if json.Unmarshal(out["result"], &result) != nil || result.IsError || result.Structured.Total != 2 {
		t.Fatal(string(out["result"]))
	}
	citation := result.Structured.Items[0].Citation
	args, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "knowledge_source_read", "arguments": map[string]string{"member_id": citation.MemberID, "revision": citation.VisibilityRevision}}})
	out = request("POST", string(args), 200)
	if !strings.Contains(string(out["result"]), `"isError":false`) {
		t.Fatal(string(out["result"]))
	}
	request("GET", "", 405)
	for _, raw := range []string{`[]`, `null`, `{"jsonrpc":"2.0","id":null,"method":"ping"}`, `{"jsonrpc":"2.0","id":1,"method":"ping","method":"tools/list"}`, `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"\ud800"}}`, strings.Repeat(" ", 32769)} {
		request("POST", raw, 400)
	}
	for _, method := range []string{"admin/reset", "resources/read", "sampling/createMessage", "tools/private"} {
		out = request("POST", `{"jsonrpc":"2.0","id":7,"method":"`+method+`"}`, 200)
		if !strings.Contains(string(out["error"]), "-32601") {
			t.Fatal("method escaped allowlist", out)
		}
	}
	for _, args := range []string{`{"name":"knowledge_search","arguments":{"query":"x","page":1,"page_size":2,"collection_id":"other"}}`, `{"name":"knowledge_source_read","arguments":{"member_id":"personal","revision":"private"}}`, `{"name":"unknown","arguments":{}}`} {
		out = request("POST", `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":`+args+`}`, 200)
		if !strings.Contains(string(out["error"]), "-32602") {
			t.Fatal("params escaped", out)
		}
	}
}
func TestPublicMCPDiscardsFinalFailure(t *testing.T) {
	for _, mode := range []string{"revoke", "audit", "disable", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f, _, retrieval, c := retrievalFixture(t, 1)
			h := NewHandler(f.s, noAdminSession{}, retrieval)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.machine = integrationReadFunc(func(ctx context.Context, _ string, _ identity.IntegrationAccessTarget, read func(context.Context) (any, error)) (any, error) {
				data, e := read(ctx)
				if e != nil {
					t.Fatal(e)
				}
				switch mode {
				case "revoke":
					return data, identity.ErrIntegrationCredentialInvalid
				case "audit":
					return data, errors.New("SECRET_INTERNAL")
				case "disable":
					f.enabled = false
				case "cancel":
					cancel()
				}
				return data, nil
			})
			r := gin.New()
			r.POST("/collections/:id/mcp", h.IntegrationMCP)
			q := httptest.NewRequest("POST", "/collections/"+c.ID+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knowledge_search","arguments":{"query":"公开文本","page":1,"page_size":2}}}`)).WithContext(ctx)
			q.Header.Set("Authorization", "Integration test")
			q.Header.Set("Content-Type", "application/json")
			q.Header.Set("Accept", "application/json,text/event-stream")
			q.Header.Set("MCP-Protocol-Version", PublicMCPVersion)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, q)
			if strings.Contains(w.Body.String(), "structuredContent") || strings.Contains(w.Body.String(), "SECRET_INTERNAL") {
				t.Fatal("late data escaped", w.Body.String())
			}
		})
	}
}
func TestPublicMCPCancelIsolationBoundAndCleanup(t *testing.T) {
	var s publicMCPState
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	key := mcpCallKey{Collection: "one", ID: mcpKeyID(json.RawMessage(`"a"`))}
	end, e := s.enter(key, "tools/call", cancel)
	if e != nil {
		t.Fatal(e)
	}
	foreign := key
	foreign.Collection = "other"
	s.cancel(foreign)
	if ctx.Err() != nil {
		t.Fatal("foreign cancel")
	}
	if _, e = s.enter(key, "tools/call", cancel); e == nil {
		t.Fatal("duplicate live ID")
	}
	key.ID = mcpKeyID(json.RawMessage(`"\u0061"`))
	s.cancel(key)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancellation missing")
	}
	end()
	if len(s.calls) != 0 {
		t.Fatal("retained calls")
	}
	initCtx, initCancel := context.WithCancel(t.Context())
	defer initCancel()
	end, _ = s.enter(key, "initialize", initCancel)
	defer end()
	s.cancel(key)
	if initCtx.Err() != nil {
		t.Fatal("initialize cancelled")
	}
}

func TestPublicMCPParseErrorAndInvalidParamsKeepRequestID(t *testing.T) {
	for _, tc := range []struct {
		wire string
		code int
		id   string
	}{
		{`{`, -32700, ""},
		{`[]`, -32600, ""},
		{`{"jsonrpc":"2.0","id":"current","method":"tools/call","params":[]}`, -32602, `"current"`},
	} {
		q, e := parseMCP([]byte(tc.wire))
		var pe *mcpProtocolError
		if !errors.As(e, &pe) || pe.code != tc.code || string(q.id) != tc.id {
			t.Fatal("JSON-RPC error contract", q, e)
		}
	}
}

func TestPublicMCPMetadataFailureIsNeverAToolResult(t *testing.T) {
	for _, tool := range []bool{false, true} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		mcpFailure(c, json.RawMessage(`"request"`), ErrNotFound, tool)
		if tool {
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"isError":true`) {
				t.Fatal(w.Code, w.Body.String())
			}
		} else {
			if w.Code != 404 || strings.Contains(w.Body.String(), `"result"`) || !strings.Contains(w.Body.String(), `"error"`) {
				t.Fatal("metadata rejection became fake tool success", w.Code, w.Body.String())
			}
		}
	}
}
