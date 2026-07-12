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

DROP INDEX IF EXISTS idx_mcp_oauth_resume_requests_lookup;
