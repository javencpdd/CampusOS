package config

import "os"

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	NATS     NATSConfig
	JWT      JWTConfig
}

type ServerConfig struct {
	Host string
	Port string
}

type DatabaseConfig struct {
	DSN string
}

type RedisConfig struct {
	Addr string
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

func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Host: getEnv("SERVER_HOST", "0.0.0.0"),
			Port: getEnv("SERVER_PORT", "8080"),
		},
		Database: DatabaseConfig{
			DSN: getEnv("DATABASE_DSN", "postgres://campusos:campusos_dev@localhost:5432/campusos?sslmode=disable"),
		},
		Redis: RedisConfig{
			Addr: getEnv("REDIS_ADDR", "localhost:6379"),
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
