package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeContract(t *testing.T) {
	handler := newServer()
	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{method: http.MethodGet, path: "/health", status: http.StatusOK},
		{method: http.MethodPost, path: "/extension", status: http.StatusOK},
		{method: http.MethodGet, path: "/extension", status: http.StatusMethodNotAllowed},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s %s status=%d want=%d", test.method, test.path, response.Code, test.status)
		}
	}
}
