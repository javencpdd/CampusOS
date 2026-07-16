// CampusOS's current grpc runtime uses a local HTTP Extension Gateway
// contract. This example deliberately exposes only a health endpoint and a
// deterministic echo response; it never receives database or token secrets.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func main() {
	address := os.Getenv("CAMPUSOS_PLUGIN_ADDR")
	if address == "" {
		address = "127.0.0.1:19091"
	}
	server := newServer()
	log.Printf("CampusOS v2 managed example listening on %s", address)
	log.Fatal(http.ListenAndServe(address, server))
}

func newServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/extension", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST is required"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"message": "CampusOS v2 managed example runtime is active",
			"path":    r.URL.Path,
		})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
