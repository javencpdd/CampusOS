package webcompanion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientUsesScopedCompanionTokenOnlyAtCampusOS(t *testing.T) {
	token := strings.Repeat("a", 64)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Cookie") != "" {
			t.Error("cookie forwarded")
		}
		switch r.URL.Path {
		case "/api/v1/web-companion/connect":
			var q map[string]any
			if json.NewDecoder(r.Body).Decode(&q) != nil || q["proxy_mode"] != "http" || len(q) != 3 || q["proxy"] != nil {
				t.Error("proxy configuration uploaded or mode missing")
			}

			if r.Header.Get("Authorization") != "" {
				t.Error("connect used a platform credential")
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]string{"token": token}})
		case "/api/v1/web-companion/poll":
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Error("scoped token missing")
			}
			w.WriteHeader(403)
		default:
			t.Error("unexpected helper request")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client, err := NewClientWithProxy(server.URL, "http://owned-user:owned-proxy-password@127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Connect(context.Background(), strings.Repeat("b", 32), "test PC"); err != nil {
		t.Fatal(err)
	}
	if err = client.Run(context.Background(), nil); err != ErrDenied {
		t.Fatal("revoked helper did not exit", err)
	}
	if requests != 2 {
		t.Fatal("unexpected retries", requests)
	}
}

func TestClientReportsFailedReadWithEmptyBase64InsteadOfNull(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	token := strings.Repeat("a", 64)
	var polls atomic.Int32
	completed := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/web-companion/connect":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]string{"token": token}})
		case "/api/v1/web-companion/poll":
			if polls.Add(1) == 1 {
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"job": Job{strings.Repeat("b", 32), "https://127.0.0.1/", 1024}}})
				return
			}
			select {
			case <-completed:
			case <-r.Context().Done():
			}
			w.WriteHeader(403)
		case "/api/v1/web-companion/result":
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["failed"] != true || body["bytes"] != "" {
				t.Error("failed result is not a strict empty base64 string")
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]bool{"received": true}})
			once.Do(func() { close(completed) })
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	if err := client.Connect(ctx, strings.Repeat("c", 32), "PC"); err != nil {
		t.Fatal(err)
	}
	if err := client.Run(ctx, nil); err != ErrDenied {
		t.Fatal(err)
	}
	select {
	case <-completed:
	default:
		t.Fatal("failed source result was not delivered")
	}
}
func TestClientRejectsUnsafeServersAndPrivateSourceTargets(t *testing.T) {
	for _, server := range []string{"http://example.com", "https://user:secret@example.com", "https://example.com/path", "file:///tmp/file", "https://example.com/?token=secret"} {
		if _, err := NewClient(server); err == nil {
			t.Fatal("unsafe server accepted")
		}
	}
	for _, target := range []string{"http://example.com/feed", "https://127.0.0.1/feed", "https://localhost/feed", "https://169.254.169.254/metadata", "https://user:secret@example.com/feed"} {
		if _, err := ReadPublic(context.Background(), target, 1024); err == nil {
			t.Fatal("unsafe source accepted")
		}
	}
}
