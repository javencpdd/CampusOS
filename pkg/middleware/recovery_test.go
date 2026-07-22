package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

func TestRecoveryReturnsStructuredInternalErrorWithoutPanicValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	collector := observability.NewCollector()
	router := gin.New()
	router.Use(Recovery(collector), TraceID())
	router.GET("/panic", func(*gin.Context) { panic("database password=secret") })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "password=secret") {
		t.Fatalf("panic value leaked: %s", recorder.Body.String())
	}
	var payload response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error == nil || payload.Error.Code != "internal.error" || payload.RequestID == "" || payload.Error.RequestID != payload.RequestID {
		t.Fatalf("structured panic response missing: %#v", payload)
	}
	if recorder.Header().Get("X-Request-ID") != payload.RequestID {
		t.Fatalf("request header/body mismatch: header=%q body=%q", recorder.Header().Get("X-Request-ID"), payload.RequestID)
	}
	found := false
	for _, item := range collector.Snapshot().Metrics {
		if item.Name == "campusos_http_panics_total" && item.Value == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("panic metric was not recorded")
	}
}
