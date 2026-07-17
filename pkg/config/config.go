package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Server     ServerConfig
	HostAPI    HostAPIConfig
	Database   DatabaseConfig
	Redis      RedisConfig
	NATS       NATSConfig
	JWT        JWTConfig
	Auth       AuthConfig
	Email      EmailConfig
	Plugin     PluginConfig
	Deployment DeploymentConfig
	AI         AIConfig
	Webhook    WebhookConfig
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
	PasswordHashEnabled          bool
	BootstrapAdminSecret         string
	AllowDevelopmentDefaultAdmin bool
	ChallengeActiveKeyID         string
	ChallengeHMACKeys            map[string]string
	ChallengeIPHashSecret        string
	SessionIPHashSecret          string
	RefreshBodyCompat            bool
}

// EmailConfig contains process-only email delivery settings. It is never
// returned by an HTTP endpoint; the Admin surface exposes only provider health
// and a redacted generic delivery error.
type EmailConfig struct {
	Provider     string
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPTimeout  string
	SMTPStartTLS bool
}

type PluginConfig struct {
	DataDir string
}

// DeploymentConfig makes local-provider safety explicit. v0.9 supports one
// writer because User Storage and the legacy plugin KV adapter are local.
type DeploymentConfig struct {
	InstanceMode string
	Environment  string
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

// WebhookConfig keeps outbound delivery safe by default. Private destinations
// require an explicit development/test opt-in and an optional host allowlist.
type WebhookConfig struct {
	AllowedHosts        []string
	AllowPrivateNetwork bool
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
			AccessTTL:  get("JWT_ACCESS_TTL", "15m"),
			RefreshTTL: get("JWT_REFRESH_TTL", "720h"),
			Issuer:     get("JWT_ISSUER", "campusos"),
		},
		Auth: AuthConfig{
			PasswordHashEnabled:          getBool("AUTH_PASSWORD_HASH_ENABLED", true),
			BootstrapAdminSecret:         strings.TrimSpace(get("AUTH_BOOTSTRAP_ADMIN_SECRET", "")),
			AllowDevelopmentDefaultAdmin: getBool("AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN", strings.EqualFold(strings.TrimSpace(get("CAMPUSOS_ENV", "development")), "development")),
			ChallengeActiveKeyID:         strings.TrimSpace(get("AUTH_CHALLENGE_ACTIVE_KEY_ID", "development-v1")),
			ChallengeHMACKeys:            parseKeyedSecrets(get("AUTH_CHALLENGE_HMAC_KEYS", "development-v1:campusos-development-challenge-key-change-before-production")),
			ChallengeIPHashSecret:        strings.TrimSpace(get("AUTH_CHALLENGE_IP_HASH_SECRET", "campusos-development-ip-hash-key-change-before-production")),
			SessionIPHashSecret:          strings.TrimSpace(get("AUTH_SESSION_IP_HASH_SECRET", "campusos-development-session-ip-hash-key-change-before-production")),
			RefreshBodyCompat:            getBool("AUTH_REFRESH_BODY_COMPAT", false),
		},
		Email: EmailConfig{
			Provider:     strings.ToLower(strings.TrimSpace(get("EMAIL_PROVIDER", "fake"))),
			SMTPHost:     strings.TrimSpace(get("EMAIL_SMTP_HOST", "")),
			SMTPPort:     getInt("EMAIL_SMTP_PORT", 587),
			SMTPUsername: strings.TrimSpace(get("EMAIL_SMTP_USERNAME", "")),
			SMTPPassword: get("EMAIL_SMTP_PASSWORD", ""),
			SMTPFrom:     strings.TrimSpace(get("EMAIL_SMTP_FROM", "")),
			SMTPTimeout:  strings.TrimSpace(get("EMAIL_SMTP_TIMEOUT", "10s")),
			SMTPStartTLS: getBool("EMAIL_SMTP_STARTTLS", true),
		},
		Plugin: PluginConfig{
			DataDir: get("PLUGIN_DATA_DIR", "data/plugin_data"),
		},
		Deployment: DeploymentConfig{
			InstanceMode: strings.ToLower(get("CAMPUSOS_INSTANCE_MODE", "single")),
			Environment:  strings.ToLower(strings.TrimSpace(get("CAMPUSOS_ENV", "development"))),
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
		Webhook: WebhookConfig{
			AllowedHosts:        splitCSV(get("WEBHOOK_ALLOWED_HOSTS", "")),
			AllowPrivateNetwork: getBool("WEBHOOK_ALLOW_PRIVATE_NETWORK", false),
		},
	}
}

func splitCSV(value string) []string {
	items := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

// parseKeyedSecrets accepts key_id:secret pairs separated by commas. It is
// intentionally local to config loading: callers receive a map but never a
// serialized configuration endpoint. A malformed pair is ignored and is
// rejected by deployment validation when it is selected as the active key.
func parseKeyedSecrets(value string) map[string]string {
	result := make(map[string]string)
	for _, item := range strings.Split(value, ",") {
		parts := strings.SplitN(strings.TrimSpace(item), ":", 2)
		if len(parts) != 2 {
			continue
		}
		keyID := strings.TrimSpace(parts[0])
		secret := strings.TrimSpace(parts[1])
		if keyID == "" || secret == "" {
			continue
		}
		result[keyID] = secret
	}
	return result
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
