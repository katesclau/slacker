# Slack App Setup For Slacker

This guide walks through creating and connecting a new Slack app for `slacker`.

## 1) Create a new Slack app

1. Go to [https://api.slack.com/apps](https://api.slack.com/apps).
2. Click **Create New App** -> **From scratch**.
3. Name the app (for example, `slacker-dev`).
4. Select your target workspace.

## 2) Enable Socket Mode

1. Open **Socket Mode** in the Slack app settings.
2. Turn Socket Mode **On**.
3. Create an app-level token:
   - Name: `socket-mode`
   - Scope: `connections:write`
4. Copy the token (`xapp-...`) and save it for `.env`:
   - `SLACK_APP_TOKEN`

## 3) Configure bot token scopes

1. Open **OAuth & Permissions**.
2. Under **Bot Token Scopes**, add at minimum:
   - `commands`
   - `chat:write`
3. Add additional scopes only if your features require them.

## 4) Add slash commands

1. Open **Slash Commands**.
2. Create:
   - `/slacker`
   - `/slacker-config`
3. Keep descriptions short and clear.

These should match your `.env` values:

- `SLACK_CHAT_COMMAND=/slacker`
- `SLACK_CONFIG_COMMAND=/slacker-config`

## 5) Install app to workspace

1. Return to **OAuth & Permissions**.
2. Click **Install to Workspace**.
3. Copy **Bot User OAuth Token** (`xoxb-...`) and set:
   - `SLACK_BOT_TOKEN`

## 6) Configure slacker environment

In your local `slacker/.env`, set:

```dotenv
SLACK_APP_TOKEN=xapp-...
SLACK_BOT_TOKEN=xoxb-...
SLACK_CHAT_COMMAND=/slacker
SLACK_CONFIG_COMMAND=/slacker-config
SLACK_ADMIN_USERS=U12345
```

To get your Slack user ID:

1. Open Slack profile menu.
2. Copy member ID.
3. Put it in `SLACK_ADMIN_USERS` (comma-separated for multiple admins).

## 7) Run and verify

1. Start dependencies:
   - `docker compose up -d`
2. Start slacker:
   - `go run ./cmd/slacker`
3. In Slack, run:
   - `/slacker hello`

If you receive a response in Slack, app setup is working.

## 8) OAuth callback note (for MCP servers)

For MCP OAuth flows (for example GitHub MCP), `APP_PUBLIC_BASE_URL` must be publicly reachable by OAuth providers.

For local development, use a tunnel (ngrok/cloudflared) or deploy `slacker` to a public URL before testing OAuth start/callback endpoints.
