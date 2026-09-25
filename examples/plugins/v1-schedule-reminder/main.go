package main

import (
	"encoding/json"
	"net/http"
	"os"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-CampusOS-Plugin-Token") == "" {
			http.Error(w, "missing runtime token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-CampusOS-Process-Contract", "campusos.process/v1")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-CampusOS-Plugin-Token") == "" {
			http.Error(w, "missing runtime token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-CampusOS-Process-Contract", "campusos.process/v1")
		w.WriteHeader(http.StatusNoContent)
	})
	_ = http.ListenAndServe(env("CAMPUSOS_PLUGIN_LISTEN", "127.0.0.1:39101"), mux)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
