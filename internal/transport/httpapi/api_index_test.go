package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAPIIndexNegotiatesJSONAndHTML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1", APIIndex)

	jsonRequest := httptest.NewRequest(http.MethodGet, "/api/v1", nil)
	jsonRequest.Header.Set("Accept", "application/json")
	jsonRecorder := httptest.NewRecorder()
	r.ServeHTTP(jsonRecorder, jsonRequest)
	if jsonRecorder.Code != http.StatusOK || !strings.Contains(jsonRecorder.Body.String(), `"version":"v1"`) || !strings.Contains(jsonRecorder.Body.String(), `"application_version":"v0.11.0"`) {
		t.Fatalf("expected JSON API index, status=%d body=%s", jsonRecorder.Code, jsonRecorder.Body.String())
	}

	htmlRequest := httptest.NewRequest(http.MethodGet, "/api/v1", nil)
	htmlRequest.Header.Set("Accept", "text/html")
	htmlRecorder := httptest.NewRecorder()
	r.ServeHTTP(htmlRecorder, htmlRequest)
	if htmlRecorder.Code != http.StatusOK || !strings.Contains(htmlRecorder.Header().Get("Content-Type"), "text/html") || !strings.Contains(htmlRecorder.Body.String(), "CampusOS API v1") {
		t.Fatalf("expected HTML API index, status=%d content-type=%s body=%s", htmlRecorder.Code, htmlRecorder.Header().Get("Content-Type"), htmlRecorder.Body.String())
	}
}

func TestAPINoRouteUsesStructuredError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.NoRoute(APINoRoute)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"code":"resource.not_found"`) {
		t.Fatalf("expected structured API 404, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestModuleOwnerRecognizesPackageLevelHTTPHandler(t *testing.T) {
	owner := moduleOwner("github.com/campusos/CampusOS/internal/transport/httpapi.APIIndex", "/api/v1")
	if owner != "core.platform-api" {
		t.Fatalf("unexpected API index owner: %q", owner)
	}
}
