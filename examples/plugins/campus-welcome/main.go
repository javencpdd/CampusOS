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
		address = "127.0.0.1:19093"
	}
	log.Printf("campus-welcome listening on %s", address)
	log.Fatal(http.ListenAndServe(address, newServer()))
}

func newServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/extension", func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST is required"})
			return
		}
		var call struct {
			Path   string `json:"path"`
			Caller struct {
				Username string `json:"username"`
				TraceID  string `json:"trace_id"`
			} `json:"caller"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, request.Body, 1<<20)).Decode(&call); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid extension request"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":  "CampusOS external Extension Gateway is ready",
			"path":     call.Path,
			"caller":   map[string]string{"username": call.Caller.Username},
			"trace_id": call.Caller.TraceID,
		})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
