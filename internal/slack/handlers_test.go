package slackruntime

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/katesclau/slacker/internal/mcpauth"
	"github.com/katesclau/slacker/internal/store/postgres"
)

func TestResumeOAuthConversationOwnershipMismatchReturnsBeforeSlack(t *testing.T) {
	ctx := context.Background()
	repo, pool, cleanup := newSlackTestRepository(t, ctx)

	serverName := "slack-resume-mismatch-" + time.Now().Format("150405.000000000")
	requestID := "request-" + serverName
	ensureSlackTestMCPServer(t, ctx, pool, serverName)
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mcp_servers WHERE name = $1`, serverName)
		cleanup()
	}()

	if err := repo.UpsertMCPOAuthResumeRequest(ctx, postgres.MCPOAuthResumeRequest{
		RequestID:      requestID,
		MCPServer:      serverName,
		SlackTeamID:    "T1",
		SlackUserID:    "U1",
		SlackChannelID: "C1",
		SlackThreadTS:  "123.456",
		Prompt:         "list github repos",
	}); err != nil {
		t.Fatalf("upsert resume request: %v", err)
	}

	r := &Runtime{repo: repo, log: slog.Default()}
	err := r.ResumeOAuthConversation(ctx, mcpauth.OAuthState{
		MCPServer:   serverName,
		RequestID:   requestID,
		SlackTeamID: "T1",
		SlackUserID: "different-user",
	})
	if err != nil {
		t.Fatalf("resume oauth conversation: %v", err)
	}

	got, err := repo.GetMCPOAuthResumeRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("get resume request: %v", err)
	}
	if got == nil {
		t.Fatal("expected mismatch path to leave resume request for retry/inspection")
	}
}

func TestResumeOAuthConversationEmptyPromptReturnsBeforeSlack(t *testing.T) {
	ctx := context.Background()
	repo, pool, cleanup := newSlackTestRepository(t, ctx)

	serverName := "slack-resume-empty-" + time.Now().Format("150405.000000000")
	requestID := "request-" + serverName
	ensureSlackTestMCPServer(t, ctx, pool, serverName)
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mcp_servers WHERE name = $1`, serverName)
		cleanup()
	}()

	if err := repo.UpsertMCPOAuthResumeRequest(ctx, postgres.MCPOAuthResumeRequest{
		RequestID:      requestID,
		MCPServer:      serverName,
		SlackTeamID:    "T1",
		SlackUserID:    "U1",
		SlackChannelID: "C1",
		SlackThreadTS:  "123.456",
		Prompt:         "",
	}); err != nil {
		t.Fatalf("upsert resume request: %v", err)
	}

	r := &Runtime{repo: repo, log: slog.Default()}
	err := r.ResumeOAuthConversation(ctx, mcpauth.OAuthState{
		MCPServer:   serverName,
		RequestID:   requestID,
		SlackTeamID: "T1",
		SlackUserID: "U1",
	})
	if err != nil {
		t.Fatalf("resume oauth conversation: %v", err)
	}

	got, err := repo.GetMCPOAuthResumeRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("get resume request: %v", err)
	}
	if got == nil {
		t.Fatal("expected empty prompt path to leave resume request for inspection")
	}
}

func newSlackTestRepository(t *testing.T, ctx context.Context) (*postgres.Repository, *pgxpool.Pool, func()) {
	t.Helper()

	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/slacker?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("postgres test database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres test database unavailable: %v", err)
	}
	ensureSlackTestResumeSchema(t, ctx, pool)
	return postgres.NewRepository(pool), pool, pool.Close
}

func ensureSlackTestResumeSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS mcp_servers (
			name TEXT PRIMARY KEY,
			resource_url TEXT NOT NULL,
			issuer_url TEXT NOT NULL,
			auth_mode TEXT NOT NULL DEFAULT 'static',
			client_name TEXT NOT NULL DEFAULT '',
			client_id TEXT NOT NULL,
			client_secret_enc TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT true,
			scopes_csv TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE IF NOT EXISTS mcp_oauth_resume_requests (
			request_id TEXT PRIMARY KEY,
			mcp_server TEXT NOT NULL REFERENCES mcp_servers(name) ON DELETE CASCADE,
			slack_team_id TEXT NOT NULL,
			slack_user_id TEXT NOT NULL,
			slack_channel_id TEXT NOT NULL,
			slack_thread_ts TEXT NOT NULL,
			agent_name TEXT NOT NULL DEFAULT '',
			prompt TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		ALTER TABLE mcp_oauth_resume_requests
			ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
		CREATE INDEX IF NOT EXISTS idx_mcp_oauth_resume_requests_created_at
			ON mcp_oauth_resume_requests (created_at DESC);
	`)
	if err != nil {
		t.Fatalf("ensure resume schema: %v", err)
	}
}

func ensureSlackTestMCPServer(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO mcp_servers (name, resource_url, issuer_url, client_id, client_secret_enc, enabled)
		VALUES ($1, 'https://resource.example', 'https://issuer.example', 'client-id', '', true)
		ON CONFLICT (name) DO UPDATE SET updated_at = now()
	`, name)
	if err != nil {
		t.Fatalf("ensure mcp server: %v", err)
	}
}
