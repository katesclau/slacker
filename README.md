# slacker

`slacker` is a Go Slack application that runs user-defined agents with Google ADK and allows those agents to call MCP servers via the MCP Go SDK.

## Architecture

### Core components

- **Slack runtime** (`internal/slack`)
  - Socket Mode event loop for slash commands, events, and interactions.
  - Primary user entrypoint is the chat slash command (`/slacker` by default).
- **Agent runtime** (`internal/agents`)
  - Builds and runs user-defined agents using:
    - `google.golang.org/adk/v2/agent/llmagent`
    - `google.golang.org/adk/v2/runner`
  - Agent definitions are loaded from Postgres (`agents` table).
- **MCP integration** (`internal/mcpclient`)
  - MCP client transport via `github.com/modelcontextprotocol/go-sdk/mcp`.
  - MCP tools are exposed to ADK through ADK MCP toolsets.
  - Per-user OAuth bearer token is attached to MCP requests when available.
- **OAuth subsystem** (`internal/mcpauth`, `internal/httpserver/oauth.go`)
  - OAuth start + callback endpoints per MCP server.
  - Token exchange and encrypted token persistence.
- **Persistence and data**
  - Postgres: users/channels/agents/permissions/MCP server config/OAuth tokens/prompt metadata.
  - pgvector + Redis: semantic memory + recent-memory cache.
  - MinIO: versioned prompt document object storage.

### Request flow

1. User triggers `/slacker` in Slack.
2. Runtime parses optional agent target (`@agent_name <prompt>`), starts a new thread, and echoes the original request as a quoted message.
3. ADK runner executes the selected agent while posting a "Thinking..." placeholder in the thread.
4. Agent can call:
   - local Block Kit tools (`internal/tooling/blockkit`)
   - MCP tools from configured MCP servers.
5. The thinking placeholder is updated with a Block Kit response.

## Configuration

`slacker` is configured via `.env` (see `.env.example`).

Required groups:

- **Slack**
  - `SLACK_APP_TOKEN`
  - `SLACK_BOT_TOKEN`
  - `SLACK_CHAT_COMMAND`
  - `SLACK_CONFIG_COMMAND`
  - `SLACK_ADMIN_USERS`
- **LLM**
  - `OPENAI_API_KEY`
  - `OPENAI_DEFAULT_MODEL`
- **Infra**
  - `POSTGRES_DSN`
  - `REDIS_ADDR` (+ optional password/db)
  - `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, `MINIO_BUCKET`
- **OAuth security**
  - `TOKEN_ENCRYPTION_KEY_BASE64`
  - `OAUTH_STATE_HMAC_KEY_BASE64`
- **HTTP**
  - `APP_PORT`
  - `APP_PUBLIC_BASE_URL` (must be public for OAuth callback)

## How to run locally

1. Copy and edit env file:
   - `cp .env.example .env`
2. Start local dependencies:
   - `docker compose up -d`
3. Apply DB schema:
   - `make db-migrate`
4. Run service:
   - `make run`
   - or with hot reload: `make air` (requires `make air-install` once)
5. Verify health:
   - `curl http://localhost:8080/health`

## How to connect to Slack

### 1) Create and configure Slack app

- Create a Slack app for your workspace.
- Enable **Socket Mode**.
- Create an **App-Level Token** with `connections:write`.
- Install app to workspace and copy **Bot User OAuth Token**.
- Create slash commands:
  - `/slacker` (or value from `SLACK_CHAT_COMMAND`)
  - `/slacker-config` (or value from `SLACK_CONFIG_COMMAND`)
- Typical bot scopes:
  - `commands`
  - `chat:write`
  - plus any read scopes you want for future event handling.

### 2) Configure `slacker`

- Set `SLACK_APP_TOKEN` and `SLACK_BOT_TOKEN` in `.env`.
- Start service and run `/slacker hello` in a channel.

### `/slacker-config` admin command

Admin users can manage MCP servers from Slack using:

- `/slacker-config mcp add` to add/update an MCP server configuration.
- `/slacker-config mcp list` to enable/disable configured MCP servers.
- `/slacker-config mcp remove` to delete an MCP server configuration.

## User-defined agents

Agent definitions come from Postgres `agents` table.

`agents.config_json` currently supports:

- `instruction` (agent system instruction)
- `model` (optional per-agent model override)
- `mcp_servers` (optional MCP server allow-list by name)

Example seed:

```sql
INSERT INTO agents (id, name, description, config_json, created_by)
VALUES (
  'agent-default',
  'default_agent',
  'Primary assistant',
  '{
    "instruction":"You are the default Slacker assistant.",
    "model":"gpt-5",
    "mcp_servers":["github"]
  }'::jsonb,
  'system'
)
ON CONFLICT (name) DO NOTHING;
```

Invoke specific agent:

- `/slacker @default_agent summarize open pull requests`

## MCP OAuth flow

OAuth endpoints:

- Start: `GET /slacker/v1/oauth/{mcp_server}/start`
- Callback: `GET /slacker/v1/oauth/{mcp_server}/callback`

### Start endpoint query params

Required:

- `team_id`
- `user_id`
- `request_id`

Optional:

- `scope`
- `resource_metadata`

### End-to-end flow

1. User opens OAuth start URL for an MCP server.
2. `slacker` discovers auth metadata. For DCR mode, it dynamically registers an OAuth client first.
3. `slacker` redirects to provider authorize URL.
4. Provider redirects back with `code` + `state`.
5. `slacker` exchanges code for token.
6. Access/refresh tokens are encrypted and stored in `mcp_oauth_tokens`.
7. Subsequent MCP tool calls by that same Slack user/team include bearer auth.

### OAuth modes supported per MCP server

- `static`: use preconfigured `client_id` + `client_secret_enc`.
- `dcr`: dynamically register a client using the authorization server `registration_endpoint`; generated credentials are persisted into `mcp_servers`.

## Atlassian Jira DCR example

This matches Jira/Atlassian MCP configuration where registration is dynamic.

### 1) Insert MCP server in DCR mode

```sql
INSERT INTO mcp_servers (name, resource_url, issuer_url, auth_mode, client_name, client_id, client_secret_enc, scopes_csv, enabled)
VALUES (
  'atlassian',
  'https://mcp.atlassian.com/v1/mcp/authv2',
  'https://auth.atlassian.com',
  'dcr',
  'slacker',
  '',
  '',
  '',
  true
)
ON CONFLICT (name) DO UPDATE SET
  resource_url = EXCLUDED.resource_url,
  issuer_url = EXCLUDED.issuer_url,
  auth_mode = EXCLUDED.auth_mode,
  client_name = EXCLUDED.client_name,
  enabled = EXCLUDED.enabled,
  updated_at = now();
```

### 2) Trigger OAuth from Slack

Use `/slacker` with an MCP/Jira request; if access is missing, Slacker posts a private connect message in-channel.

### 3) Validate DCR + token persistence

After opening the connect link and completing callback, verify:

```sql
SELECT name, auth_mode, client_name, client_id
FROM mcp_servers
WHERE name = 'atlassian';

SELECT mcp_server, slack_team_id, slack_user_id, expires_at
FROM mcp_oauth_tokens
WHERE mcp_server = 'atlassian';
```

Expected:

- `mcp_servers.client_id` is populated by DCR.
- `mcp_oauth_tokens` contains a row for the Slack user/team.
- other users still need their own OAuth connection.

### Local callback note

Use a publicly reachable HTTPS base URL for `APP_PUBLIC_BASE_URL`. OAuth providers generally cannot redirect to local-only `localhost` URLs.

## GitHub MCP example (from `mcp-slackitt` sample)

Using your sample:

- MCP server name: `github`
- MCP URL: `https://api.githubcopilot.com/mcp/`
- Issuer: `https://github.com/login/oauth`
- Scopes: `repo,read:org,read:user,user:email`

### 1) Map auth keys to `slacker` env

- `public_base_url` -> `APP_PUBLIC_BASE_URL`
- `state_hmac_key_base64` -> `OAUTH_STATE_HMAC_KEY_BASE64`
- `token_encryption_key_base64` -> `TOKEN_ENCRYPTION_KEY_BASE64`

### 2) Insert MCP server config

```sql
INSERT INTO mcp_servers (name, resource_url, issuer_url, client_id, client_secret_enc, scopes_csv)
VALUES (
  'github',
  'https://api.githubcopilot.com/mcp/',
  'https://github.com/login/oauth',
  '<GITHUB_OAUTH_CLIENT_ID>',
  '<GITHUB_OAUTH_CLIENT_SECRET>',
  'repo,read:org,read:user,user:email'
)
ON CONFLICT (name) DO UPDATE SET
  resource_url = EXCLUDED.resource_url,
  issuer_url = EXCLUDED.issuer_url,
  client_id = EXCLUDED.client_id,
  client_secret_enc = EXCLUDED.client_secret_enc,
  scopes_csv = EXCLUDED.scopes_csv,
  updated_at = now();
```

### 3) Start OAuth for a Slack user

```text
https://<APP_PUBLIC_BASE_URL>/slacker/v1/oauth/github/start?team_id=T123&user_id=U123&request_id=req-001
```

After successful callback, token rows appear in `mcp_oauth_tokens` for that user/team/server and are used automatically by MCP tool calls.
