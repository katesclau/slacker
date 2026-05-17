package config

import (
	"encoding/base64"
	"testing"
)

func TestLoad_ConfigFromEnv(t *testing.T) {
	t.Setenv("SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("POSTGRES_DSN", "postgres://postgres:postgres@localhost:5432/slacker?sslmode=disable")
	t.Setenv("TOKEN_ENCRYPTION_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte("12345678901234567890123456789012")))
	t.Setenv("OAUTH_STATE_HMAC_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte("1234567890123456")))
	t.Setenv("APP_PORT", "9090")
	t.Setenv("SLACK_ADMIN_USERS", "U1,U2")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.App.Port != 9090 {
		t.Fatalf("expected app port 9090, got %d", cfg.App.Port)
	}
	if len(cfg.Slack.AdminUsers) != 2 {
		t.Fatalf("expected 2 admins, got %d", len(cfg.Slack.AdminUsers))
	}
}
