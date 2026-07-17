package main

import (
	"log"

	platformversion "github.com/campusos/CampusOS/internal/platform/version"
	"github.com/campusos/CampusOS/internal/server"
	"github.com/campusos/CampusOS/pkg/config"
)

func main() {
	cfg := config.Load()

	log.Printf("CampusOS %s starting on %s (environment=%s)", platformversion.Display, cfg.Server.Addr(), cfg.Deployment.Environment)
	log.Printf("📖 API docs: http://%s/api/v1/health", cfg.Server.Addr())

	srv := server.New(cfg)
	if err := srv.Run(); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}
