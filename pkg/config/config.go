package config

import (
	"os"
	"strconv"
)

type Config struct {
	Server   ServerConfig
	HostAPI  HostAPIConfig
	Database DatabaseConfig
	Redis    RedisConfig
	NATS     NATSConfig
	JWT      JWTConfig
	Plugin   PluginConfig
	AI       AIConfig
}

type ServerConfig struct {
	Host string
	Port string
}

type HostAPIConfig struct {
	Enabled bool
	Addr    string
}

type DatabaseConfig struct {
	DSN string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
	Enabled  bool
}

type NATSConfig struct {
	URL string
}

type JWTConfig struct {
	Secret     string
	AccessTTL  string
	RefreshTTL string
	Issuer     string
}

type PluginConfig struct {
	DataDir string
}

type AIConfig struct {
	Enabled              bool
	Provider             string
	BaseURL              string
	Model                string
	APIKey               string
	Timeout              string
	MaxRequestsPerMinute int
	MaxConcurrent        int
}

func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Host: getEnv("SERVER_HOST", "0.0.0.0"),
			Port: getEnv("SERVER_PORT", "8080"),
		},
		HostAPI: HostAPIConfig{
			Enabled: getEnv("HOST_API_ENABLED", "true") == "true",
			Addr:    getEnv("HOST_API_ADDR", "127.0.0.1:18080"),
		},
		Database: DatabaseConfig{
			DSN: getEnv("DATABASE_DSN", "postgres://campusos:campusos_dev@localhost:5432/campusos?sslmode=disable"),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       0,
			Enabled:  getEnv("REDIS_ENABLED", "true") == "true",
		},
		NATS: NATSConfig{
			URL: getEnv("NATS_URL", "nats://localhost:4222"),
		},
		JWT: JWTConfig{
			Secret:     getEnv("JWT_SECRET", "campusos-dev-secret-key-change-in-production"),
			AccessTTL:  getEnv("JWT_ACCESS_TTL", "2h"),
			RefreshTTL: getEnv("JWT_REFRESH_TTL", "720h"),
			Issuer:     getEnv("JWT_ISSUER", "campusos"),
		},
		Plugin: PluginConfig{
			DataDir: getEnv("PLUGIN_DATA_DIR", ".campusos/plugin-data"),
		},
		AI: AIConfig{
			Enabled:              getEnv("AI_ENABLED", "false") == "true",
			Provider:             getEnv("AI_PROVIDER", "openai-compatible"),
			BaseURL:              getEnv("AI_BASE_URL", "https://api.openai.com/v1"),
			Model:                getEnv("AI_MODEL", "gpt-4o-mini"),
			APIKey:               getEnv("AI_API_KEY", ""),
			Timeout:              getEnv("AI_TIMEOUT", "30s"),
			MaxRequestsPerMinute: getEnvInt("AI_MAX_REQUESTS_PER_MINUTE", 60),
			MaxConcurrent:        getEnvInt("AI_MAX_CONCURRENT", 4),
		},
	}
}

func (s *ServerConfig) Addr() string {
	return s.Host + ":" + s.Port
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		parsed, err := strconv.Atoi(v)
		if err == nil {
			return parsed
		}
	}
	return fallback
}
