package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Server   ServerConfig
	HostAPI  HostAPIConfig
	Database DatabaseConfig
	Redis    RedisConfig
	NATS     NATSConfig
	JWT      JWTConfig
	Auth     AuthConfig
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

type AuthConfig struct {
	PasswordHashEnabled bool
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
	fileEnv := loadDotEnv(".env")
	get := func(key, fallback string) string {
		return getEnvWithFile(fileEnv, key, fallback)
	}
	getInt := func(key string, fallback int) int {
		return getEnvIntWithFile(fileEnv, key, fallback)
	}
	getBool := func(key string, fallback bool) bool {
		return getEnvBoolWithFile(fileEnv, key, fallback)
	}

	return &Config{
		Server: ServerConfig{
			Host: get("SERVER_HOST", "0.0.0.0"),
			Port: get("SERVER_PORT", "8080"),
		},
		HostAPI: HostAPIConfig{
			Enabled: get("HOST_API_ENABLED", "true") == "true",
			Addr:    get("HOST_API_ADDR", "127.0.0.1:18080"),
		},
		Database: DatabaseConfig{
			DSN: get("DATABASE_DSN", "postgres://campusos:campusos_dev@localhost:5432/campusos?sslmode=disable"),
		},
		Redis: RedisConfig{
			Addr:     get("REDIS_ADDR", "localhost:6379"),
			Password: get("REDIS_PASSWORD", ""),
			DB:       0,
			Enabled:  get("REDIS_ENABLED", "true") == "true",
		},
		NATS: NATSConfig{
			URL: get("NATS_URL", "nats://localhost:4222"),
		},
		JWT: JWTConfig{
			Secret:     get("JWT_SECRET", "campusos-dev-secret-key-change-in-production"),
			AccessTTL:  get("JWT_ACCESS_TTL", "2h"),
			RefreshTTL: get("JWT_REFRESH_TTL", "720h"),
			Issuer:     get("JWT_ISSUER", "campusos"),
		},
		Auth: AuthConfig{
			PasswordHashEnabled: getBool("AUTH_PASSWORD_HASH_ENABLED", true),
		},
		Plugin: PluginConfig{
			DataDir: get("PLUGIN_DATA_DIR", ".campusos/plugin-data"),
		},
		AI: AIConfig{
			Enabled:              get("AI_ENABLED", "false") == "true",
			Provider:             get("AI_PROVIDER", "openai-compatible"),
			BaseURL:              get("AI_BASE_URL", "https://api.openai.com/v1"),
			Model:                get("AI_MODEL", "gpt-4o-mini"),
			APIKey:               get("AI_API_KEY", ""),
			Timeout:              get("AI_TIMEOUT", "30s"),
			MaxRequestsPerMinute: getInt("AI_MAX_REQUESTS_PER_MINUTE", 60),
			MaxConcurrent:        getInt("AI_MAX_CONCURRENT", 4),
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

func getEnvWithFile(fileEnv map[string]string, key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if v := fileEnv[key]; v != "" {
		return v
	}
	return fallback
}

func getEnvIntWithFile(fileEnv map[string]string, key string, fallback int) int {
	if v := getEnvWithFile(fileEnv, key, ""); v != "" {
		parsed, err := strconv.Atoi(v)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvBoolWithFile(fileEnv map[string]string, key string, fallback bool) bool {
	if v := getEnvWithFile(fileEnv, key, ""); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}

func loadDotEnv(path string) map[string]string {
	values := make(map[string]string)

	file, err := os.Open(path)
	if err != nil {
		return values
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if ok {
			values[key] = value
		}
	}
	return values
}

func parseDotEnvLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
		return "", "", false
	}

	parts := strings.SplitN(line, "=", 2)
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" {
		return "", "", false
	}

	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			value = value[1 : len(value)-1]
		}
	}

	return key, value, true
}
