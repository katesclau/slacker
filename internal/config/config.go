package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	App      AppConfig
	Slack    SlackConfig
	OpenAI   OpenAIConfig
	Postgres PostgresConfig
	Redis    RedisConfig
	MinIO    MinIOConfig
	Security SecurityConfig
	Memory   MemoryConfig
}

type AppConfig struct {
	Env           string
	Port          int
	PublicBaseURL string
	LogLevel      string
}

type SlackConfig struct {
	AppToken      string
	BotToken      string
	ChatCommand   string
	ConfigCommand string
	AdminUsers    []string
}

type OpenAIConfig struct {
	APIKey       string
	DefaultModel string
}

type PostgresConfig struct {
	DSN string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Bucket    string
}

type SecurityConfig struct {
	TokenEncryptionKey []byte
	OAuthStateHMACKey  []byte
}

type MemoryConfig struct {
	EmbeddingDimensions int
}

func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		App: AppConfig{
			Env:           getEnv("APP_ENV", "development"),
			PublicBaseURL: strings.TrimSuffix(getEnv("APP_PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
			LogLevel:      getEnv("APP_LOG_LEVEL", "info"),
		},
		Slack: SlackConfig{
			AppToken:      getEnv("SLACK_APP_TOKEN", ""),
			BotToken:      getEnv("SLACK_BOT_TOKEN", ""),
			ChatCommand:   getEnv("SLACK_CHAT_COMMAND", "/slacker"),
			ConfigCommand: getEnv("SLACK_CONFIG_COMMAND", "/slacker-config"),
			AdminUsers:    splitCSV(getEnv("SLACK_ADMIN_USERS", "")),
		},
		OpenAI: OpenAIConfig{
			APIKey:       getEnv("OPENAI_API_KEY", ""),
			DefaultModel: getEnv("OPENAI_DEFAULT_MODEL", "gpt-5"),
		},
		Postgres: PostgresConfig{
			DSN: getEnv("POSTGRES_DSN", ""),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
		},
		MinIO: MinIOConfig{
			Endpoint:  getEnv("MINIO_ENDPOINT", "localhost:9000"),
			AccessKey: getEnv("MINIO_ACCESS_KEY", ""),
			SecretKey: getEnv("MINIO_SECRET_KEY", ""),
			Bucket:    getEnv("MINIO_BUCKET", "slacker-prompts"),
		},
		Memory: MemoryConfig{
			EmbeddingDimensions: getEnvInt("MEMORY_EMBEDDING_DIMS", 1536),
		},
	}

	cfg.App.Port = getEnvInt("APP_PORT", 8080)
	cfg.Redis.DB = getEnvInt("REDIS_DB", 0)
	cfg.MinIO.UseSSL = getEnvBool("MINIO_USE_SSL", false)

	tokenKey, err := getBase64Env("TOKEN_ENCRYPTION_KEY_BASE64")
	if err != nil {
		return Config{}, err
	}
	stateKey, err := getBase64Env("OAUTH_STATE_HMAC_KEY_BASE64")
	if err != nil {
		return Config{}, err
	}
	cfg.Security = SecurityConfig{
		TokenEncryptionKey: tokenKey,
		OAuthStateHMACKey:  stateKey,
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Slack.AppToken == "" || c.Slack.BotToken == "" {
		return fmt.Errorf("SLACK_APP_TOKEN and SLACK_BOT_TOKEN are required")
	}
	if c.Postgres.DSN == "" {
		return fmt.Errorf("POSTGRES_DSN is required")
	}
	if c.OpenAI.APIKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is required")
	}
	if len(c.Security.TokenEncryptionKey) < 32 {
		return fmt.Errorf("TOKEN_ENCRYPTION_KEY_BASE64 must decode to at least 32 bytes")
	}
	if len(c.Security.OAuthStateHMACKey) < 16 {
		return fmt.Errorf("OAUTH_STATE_HMAC_KEY_BASE64 must decode to at least 16 bytes")
	}
	return nil
}

func getBase64Env(key string) ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil, fmt.Errorf("%s is required", key)
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid base64: %w", key, err)
	}
	return decoded, nil
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getEnv(key string, fallback string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	return val
}

func getEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getEnvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}
