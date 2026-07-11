package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestErrorPreservesLegacyFieldsAndAddsStructuredContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("trace_id", "request-1")
	ErrorWithDetails(ctx, http.StatusForbidden, 20004, "permission denied", gin.H{"scope": "category:1"})

	var payload Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != 20004 || payload.Msg != "permission denied" {
		t.Fatalf("legacy fields changed: %#v", payload)
	}
	if payload.Error == nil || payload.Error.Code != "permission.denied" || payload.Error.RequestID != "request-1" || payload.RequestID != "request-1" {
		t.Fatalf("structured error missing: %#v", payload)
	}
}

func TestParsePaginationRejectsInvalidAndOversizedValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []string{"/?page=0", "/?page_size=101", "/?page_size=abc"} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
		if _, _, ok := ParsePagination(ctx, 20, 100); ok {
			t.Fatalf("expected %s to fail", target)
		}
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d", target, recorder.Code)
		}
	}
}
