package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMCPOAuthResumeRequestUpsertGetDelete(t *testing.T) {
	ctx := context.Background()
	repo, cleanup := newTestRepository(t, ctx)

	serverName := "resume-test-" + time.Now().Format("150405.000000000")
	requestID := "request-" + serverName
	ensureTestMCPServer(t, ctx, repo.db, serverName)
	defer func() {
		_, _ = repo.db.Exec(context.Background(), `DELETE FROM mcp_servers WHERE name = $1`, serverName)
		cleanup()
	}()

	req := MCPOAuthResumeRequest{
		RequestID:      requestID,
		MCPServer:      serverName,
		SlackTeamID:    "T1",
		SlackUserID:    "U1",
		SlackChannelID: "C1",
		SlackThreadTS:  "123.456",
		AgentName:      "default_agent",
		Prompt:         "list github repos",
	}
	if err := repo.UpsertMCPOAuthResumeRequest(ctx, req); err != nil {
		t.Fatalf("upsert resume request: %v", err)
	}

	got, err := repo.GetMCPOAuthResumeRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("get resume request: %v", err)
	}
	if got == nil {
		t.Fatal("expected resume request")
	}
	if got.RequestID != requestID || got.MCPServer != serverName || got.SlackTeamID != "T1" ||
		got.SlackUserID != "U1" || got.SlackChannelID != "C1" || got.SlackThreadTS != "123.456" ||
		got.AgentName != "default_agent" || got.Prompt != "list github repos" {
		t.Fatalf("unexpected resume request: %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be populated: %+v", got)
	}

	got.Prompt = "updated prompt"
	if err := repo.UpsertMCPOAuthResumeRequest(ctx, *got); err != nil {
		t.Fatalf("upsert updated resume request: %v", err)
	}
	updated, err := repo.GetMCPOAuthResumeRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("get updated resume request: %v", err)
	}
	if updated == nil || updated.Prompt != "updated prompt" {
		t.Fatalf("expected updated prompt, got %+v", updated)
	}
	if !updated.CreatedAt.Equal(got.CreatedAt) {
		t.Fatalf("expected created_at to be preserved, before=%s after=%s", got.CreatedAt, updated.CreatedAt)
	}

	if err := repo.DeleteMCPOAuthResumeRequest(ctx, requestID); err != nil {
		t.Fatalf("delete resume request: %v", err)
	}
	deleted, err := repo.GetMCPOAuthResumeRequest(ctx, requestID)
	if err != nil {
		t.Fatalf("get deleted resume request: %v", err)
	}
	if deleted != nil {
		t.Fatalf("expected resume request to be deleted, got %+v", deleted)
	}
}

func newTestRepository(t *testing.T, ctx context.Context) (*Repository, func()) {
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
	ensureTestResumeSchema(t, ctx, pool)
	return NewRepository(pool), pool.Close
}

func ensureTestResumeSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
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

func ensureTestMCPServer(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
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
