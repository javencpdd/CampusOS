package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type gatewayTestRuntime struct {
	running map[string]bool
	handler func(context.Context, *ExtensionRequest) (*ExtensionResponse, error)
}

func (r *gatewayTestRuntime) Start(_ context.Context, p *Plugin) error {
	if r.running == nil {
		r.running = map[string]bool{}
	}
	r.running[p.ID] = true
	return nil
}
func (r *gatewayTestRuntime) Stop(_ context.Context, name string) error {
	delete(r.running, name)
	return nil
}
func (r *gatewayTestRuntime) SendEvent(context.Context, string, *EventMessage) (*PluginResponse, error) {
	return &PluginResponse{Allowed: true}, nil
}
func (r *gatewayTestRuntime) HealthCheck(_ context.Context, name string) error {
	if !r.running[name] {
		return context.Canceled
	}
	return nil
}
func (r *gatewayTestRuntime) IsRunning(name string) bool { return r.running[name] }
func (r *gatewayTestRuntime) Type() string               { return "builtin" }
func (r *gatewayTestRuntime) DispatchExtension(ctx context.Context, _ string, request *ExtensionRequest) (*ExtensionResponse, error) {
	return r.handler(ctx, request)
}

func TestExtensionGatewayUsesTrustedCallerContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	manifest := `name: gateway-test
version: 0.1.0
runtime: builtin
scope: user
ui:
  actions:
    - id: plugin.gateway-test.action.check
      label: Check
      method: POST
      path: /check
  surfaces:
    - id: plugin.gateway-test.page.main
      version: v1
      type: page
      layout_role: main
      renderer: schema
      action_ids: [plugin.gateway-test.action.check]
      schema: {component: text, text: Gateway}
  routes:
    - id: plugin.gateway-test.route.main
      path: /extensions/gateway-test
      surface_id: plugin.gateway-test.page.main
`
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewManager()
	runtime := &gatewayTestRuntime{}
	runtime.handler = func(_ context.Context, request *ExtensionRequest) (*ExtensionResponse, error) {
		body, _ := json.Marshal(map[string]string{"user_id": request.Caller.UserID, "username": request.Caller.Username})
		return &ExtensionResponse{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Body: body}, nil
	}
	manager.RegisterRuntime("builtin", runtime)
	if _, err := manager.Install(dir); err != nil {
		t.Fatal(err)
	}
	if err := manager.RequestEnable("gateway-test"); err != nil {
		t.Fatal(err)
	}
	handler := NewRuntimeHTTPHandler(manager, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "core-user")
		c.Set("username", "alice")
		c.Set("trace_id", "trace-1")
		c.Next()
	})
	router.Any("/api/v1/extensions/:plugin/*path", handler.Extension)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/extensions/gateway-test/check", strings.NewReader(`{"user_id":"attacker"}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"user_id":"core-user"`) || strings.Contains(recorder.Body.String(), "attacker") {
		t.Fatalf("untrusted identity leaked: %s", recorder.Body.String())
	}
}

func TestRuntimeManifestIncludesIndependentLifecycleStates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(`name: ui-runtime-test
version: 0.1.0
runtime: builtin
scope: system
ui:
  surfaces:
    - id: plugin.ui-runtime-test.page.main
      version: v1
      type: page
      layout_role: main
      renderer: schema
      schema: {component: text, text: Ready}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewManager()
	manager.RegisterRuntime("builtin", &gatewayTestRuntime{})
	if _, err := manager.Install(dir); err != nil {
		t.Fatal(err)
	}
	handler := NewRuntimeHTTPHandler(manager, nil)
	router := gin.New()
	router.GET("/runtime", handler.RuntimeManifest)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/runtime", nil))
	if recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"frontend_state":"loaded"`) || !strings.Contains(recorder.Body.String(), `"backend_activation_mode":"restart"`) {
		t.Fatalf("missing lifecycle states: %s", recorder.Body.String())
	}
}
