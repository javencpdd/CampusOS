package server

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/plugin"
)

var errSecretRotationSetup = errors.New("secret rotation requires valid configuration, PostgreSQL and a Secret keyring")

func secretRotationConfig(getenv func(string) string) (plugin.SecretRotationConfig, error) {
	cfg := plugin.SecretRotationConfig{Interval: 5 * time.Minute, BatchSize: 100, MaxBatchesPerPlugin: 1, MaxPluginsPerRun: 25}
	switch strings.TrimSpace(getenv("CAMPUSOS_SECRET_ROTATION_ENABLED")) {
	case "", "false":
		return cfg, nil
	case "true":
		cfg.Enabled = true
	default:
		return cfg, errSecretRotationSetup
	}
	if value := strings.TrimSpace(getenv("CAMPUSOS_SECRET_ROTATION_INTERVAL")); value != "" {
		interval, err := time.ParseDuration(value)
		if err != nil || interval < time.Second || interval > 24*time.Hour {
			return cfg, errSecretRotationSetup
		}
		cfg.Interval = interval
	}
	return cfg, nil
}

func prepareSecretRotation(infra *infrastructureBootstrap) (*plugin.SecretRotationWorker, error) {
	cfg, err := secretRotationConfig(os.Getenv)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	if infra == nil || infra.database == nil || infra.database.Config().MaxConns < 2 {
		return nil, errSecretRotationSetup
	}
	store := plugin.NewPgAuthorizationStore(infra.database)
	secrets, err := plugin.NewSecretServiceFromEnv(store)
	if err != nil {
		return nil, errSecretRotationSetup
	}
	worker, err := plugin.NewSecretRotationWorker(secrets, store, reliability.NewPostgreSQLStore(infra.database), plugin.NewPgSecretRotationLocker(infra.database), cfg)
	if err != nil {
		return nil, errSecretRotationSetup
	}
	return worker, nil
}

// stop cancels and joins before infrastructure shutdown closes the pool.
func startSecretRotation(infra *infrastructureBootstrap) (stop func(), err error) {
	worker, err := prepareSecretRotation(infra)
	if err != nil {
		return nil, err
	}
	if worker == nil {
		return func() {}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := worker.Run(ctx); err != nil {
			log.Print("Secret rotation stopped after a lease, inventory or audit failure; inspect operation runs and restart after repair")
		}
	}()
	return func() { cancel(); <-done }, nil
}
