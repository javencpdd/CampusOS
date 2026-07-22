package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/pkg/apperror"
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

func TestWriteErrorUsesRegisteredDescriptorAndPreservesLegacyEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("trace_id", "request-2")
	WriteError(ctx, apperror.Wrap(errors.New("private cause"), apperror.MutualAidNotFound, gin.H{"resource": "thread"}))

	var payload Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusNotFound || payload.Code != 40003 || payload.Msg != "mutual aid thread not found" {
		t.Fatalf("legacy contract changed: status=%d payload=%#v", recorder.Code, payload)
	}
	if payload.Error == nil || payload.Error.Code != "mutual_aid.not_found" || payload.Error.RequestID != "request-2" {
		t.Fatalf("structured contract missing: %#v", payload)
	}
	if strings.Contains(recorder.Body.String(), "private cause") {
		t.Fatalf("private cause leaked: %s", recorder.Body.String())
	}
}

func TestWriteErrorSanitizesUnknownError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	secret := errors.New("postgres password=secret")
	WriteError(ctx, secret)

	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), secret.Error()) {
		t.Fatalf("unknown error was not sanitized: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != 10006 || payload.Error == nil || payload.Error.Code != "internal.error" || !payload.Error.Retryable {
		t.Fatalf("unexpected internal contract: %#v", payload)
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
