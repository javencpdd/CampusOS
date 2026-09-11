package main

import (
	"net/http"
	"os"
)

func main() {
	mux := http.NewServeMux()
	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("X-CampusOS-Plugin-Token") == "" {
			http.Error(w, "missing runtime token", http.StatusUnauthorized)
			return false
		}
		w.Header().Set("X-CampusOS-Process-Contract", "campusos.process/v1")
		return true
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if authorized(w, r) {
			w.WriteHeader(http.StatusOK)
		}
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	_ = http.ListenAndServe(env("CAMPUSOS_PLUGIN_LISTEN", "127.0.0.1:39103"), mux)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
