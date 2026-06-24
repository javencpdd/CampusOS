package main

import (
	"log"

	"github.com/campusos/CampusOS/internal/server"
	"github.com/campusos/CampusOS/pkg/config"
)

func main() {
	cfg := config.Load()

	log.Printf("🚀 CampusOS v0.1.0-dev starting on %s", cfg.Server.Addr())
	log.Printf("📖 API docs: http://%s/api/v1/health", cfg.Server.Addr())

	srv := server.New(cfg)
	if err := srv.Run(); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}
