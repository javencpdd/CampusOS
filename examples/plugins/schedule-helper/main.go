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
		address = "127.0.0.1:19092"
	}
	log.Printf("schedule-helper listening on %s", address)
	log.Fatal(http.ListenAndServe(address, newServer()))
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
			"message": "schedule-helper runtime is active",
			"boundary": "user schedule records remain host-managed",
		})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
