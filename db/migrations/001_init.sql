CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    slack_user_id TEXT NOT NULL,
    slack_team_id TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (slack_user_id, slack_team_id)
);

CREATE TABLE IF NOT EXISTS channels (
    id TEXT PRIMARY KEY,
    slack_channel_id TEXT NOT NULL,
    slack_team_id TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    settings_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (slack_channel_id, slack_team_id)
);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS permissions (
    id TEXT PRIMARY KEY,
    subject_ref TEXT NOT NULL,
    object_ref TEXT NOT NULL,
    action TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (subject_ref, object_ref, action)
);

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

CREATE TABLE IF NOT EXISTS mcp_oauth_tokens (
    mcp_server TEXT NOT NULL,
    slack_team_id TEXT NOT NULL,
    slack_user_id TEXT NOT NULL,
    resource TEXT NOT NULL DEFAULT '',
    issuer TEXT NOT NULL DEFAULT '',
    client_id TEXT NOT NULL DEFAULT '',
    enc_access_token TEXT NOT NULL,
    enc_refresh_token TEXT NOT NULL DEFAULT '',
    scope_csv TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (mcp_server, slack_team_id, slack_user_id)
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
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_mcp_oauth_resume_requests_lookup
    ON mcp_oauth_resume_requests(mcp_server, slack_team_id, slack_user_id, request_id);

CREATE TABLE IF NOT EXISTS prompt_documents (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS prompt_versions (
    id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES prompt_documents(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    object_key TEXT NOT NULL,
    content_sha256 TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, version)
);

CREATE TABLE IF NOT EXISTS memory_entries (
    id TEXT PRIMARY KEY,
    slack_team_id TEXT NOT NULL,
    slack_channel_id TEXT NOT NULL,
    user_id TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    embedding vector(1536),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memory_entries_scope ON memory_entries(slack_team_id, slack_channel_id, created_at DESC);

CREATE TABLE IF NOT EXISTS chat_threads (
    id TEXT PRIMARY KEY,
    slack_team_id TEXT NOT NULL,
    slack_channel_id TEXT NOT NULL,
    slack_thread_ts TEXT NOT NULL,
    session_id TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (slack_team_id, slack_channel_id, slack_thread_ts)
);

ALTER TABLE mcp_servers ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE mcp_servers ADD COLUMN IF NOT EXISTS auth_mode TEXT NOT NULL DEFAULT 'static';
ALTER TABLE mcp_servers ADD COLUMN IF NOT EXISTS client_name TEXT NOT NULL DEFAULT '';
