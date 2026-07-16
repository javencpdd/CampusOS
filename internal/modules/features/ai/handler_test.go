package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestHandlerGetStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &Service{
		enabled:  true,
		ready:    true,
		provider: "openai-compatible",
		providerConfig: OpenAICompatibleConfig{
			Name:    "openai-compatible",
			BaseURL: "https://ai.example.test/v1",
			APIKey:  "test-secret",
			Model:   "campus-model",
			Timeout: 30 * time.Second,
		},
		logger: NewMemoryCallLogger(),
	}
	router := gin.New()
	router.GET("/ai/status", NewHandler(service).GetStatus)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ai/status", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Enabled bool                   `json:"enabled"`
			Ready   bool                   `json:"ready"`
			Config  map[string]interface{} `json:"config"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 0 || !payload.Data.Enabled || !payload.Data.Ready {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if _, ok := payload.Data.Config["api_key"]; ok {
		t.Fatalf("status leaked api_key: %#v", payload.Data.Config)
	}
	if payload.Data.Config["api_key_configured"] != true {
		t.Fatalf("expected api_key_configured=true, got %#v", payload.Data.Config)
	}
}

func TestHandlerListLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := NewMemoryCallLogger()
	_ = logger.Log(t.Context(), CallLog{Provider: "fake", Model: "model-a", Status: CallStatusSuccess})
	service := &Service{enabled: true, ready: true, logger: logger}
	router := gin.New()
	router.GET("/ai/logs", NewHandler(service).ListLogs)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ai/logs?limit=1", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Items []CallLog `json:"items"`
			Total int       `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 0 || payload.Data.Total != 1 || len(payload.Data.Items) != 1 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}
