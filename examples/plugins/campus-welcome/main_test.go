package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtensionRuntime(t *testing.T) {
	server := newServer()
	request := httptest.NewRequest(http.MethodPost, "/extension", strings.NewReader(`{"path":"/welcome","caller":{"username":"alice","trace_id":"trace-1"}}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "trace-1") {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}
