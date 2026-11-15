package main

import (
	"errors"
	"log"

	platformversion "github.com/campusos/CampusOS/internal/platform/version"
	"github.com/campusos/CampusOS/internal/server"
	"github.com/campusos/CampusOS/pkg/config"
)

func main() {
	for {
		cfg := config.Load()

		log.Printf("CampusOS %s starting on %s (environment=%s)", platformversion.Display, cfg.Server.Addr(), cfg.Deployment.Environment)
		log.Printf("📖 API docs: http://%s/api/v1/health", cfg.Server.Addr())

		srv := server.New(cfg)
		if err := srv.Run(); errors.Is(err, server.ErrRestart) {
			log.Printf("配置已保存，正在重新加载 API")
			continue
		} else if err != nil {
			log.Fatalf("❌ Server failed to start: %v", err)
		}
		return
	}
}
