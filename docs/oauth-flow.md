# Slacker delegated MCP OAuth flow

Slacker uses the OAuth 2.0 authorization-code flow with PKCE when a Slack
user needs to authorize an MCP server. Delegated tokens are scoped to the
combination of MCP server, Slack workspace, and Slack user.

```mermaid
sequenceDiagram
    autonumber
    actor User as Slack user
    participant Slack
    participant Slacker as Slacker Slack runtime
    participant Agent as ADK agent / MCP client
    participant HTTP as Slacker HTTP server
    participant DB as PostgreSQL
    participant Auth as OAuth authorization server
    participant MCP as OAuth-protected MCP server

    User->>Slack: Ask agent to use<br/>an MCP service
    Slack->>Slacker: Slash command or thread interaction
    Slacker->>Agent: Run selected agent
    Agent->>DB: Resolve token(server, team, user)
    DB-->>Agent: Token missing, expired,<br/>or MCP call is unauthorized
    Agent-->>Slacker: Authorization needed

    Slacker->>DB: Persist original request<br/>and conversation location
    Slacker->>Slack: Ephemeral connect links<br/>for enabled MCP servers
    User->>Slack: Select Connect MCP
    Slack->>HTTP: GET /slacker/v1/oauth/{mcp}/start<br/>team_id, user_id, request_id

    HTTP->>DB: Load MCP server configuration
    HTTP->>Auth: Discover metadata; optionally<br/>dynamically register OAuth client
    HTTP->>HTTP: Create signed state + nonce<br/>and PKCE verifier/challenge
    HTTP->>Auth: Redirect to authorization endpoint<br/>with resource and scopes
    User->>Auth: Authenticate and approve access
    Auth->>HTTP: Callback with code + state

    HTTP->>HTTP: Verify signed state and nonce
    HTTP->>Auth: Exchange code with PKCE verifier
    Auth-->>HTTP: Access token + optional refresh token
    HTTP->>DB: Encrypt and store token scoped to<br/>(MCP server, Slack team, Slack user)
    HTTP->>Slacker: Resume OAuth conversation

    Slacker->>DB: Verify saved request ownership;<br/>load and delete resume request
    Slacker->>Agent: Rerun original prompt in thread
    Agent->>DB: Load encrypted delegated token
    Agent->>Agent: Decrypt access token
    Agent->>MCP: Execute tool with Bearer token
    MCP-->>Agent: Tool response
    Agent-->>Slacker: Agent response
    Slacker->>Slack: Update thread with response
```

## Implementation notes

- OAuth starts at `GET /slacker/v1/oauth/{mcp_server}/start` and returns to
  `GET /slacker/v1/oauth/{mcp_server}/callback`.
- Protected-resource and authorization-server metadata are discovered at
  runtime; Slacker supports static client credentials and dynamic client
  registration (DCR).
- The signed state contains the MCP server, request ID, Slack team, Slack
  user, resource, issuer, and a random nonce. The authorization-code exchange
  uses an S256 PKCE challenge.
- `mcp_oauth_tokens` stores encrypted access and refresh tokens with primary
  key `(mcp_server, slack_team_id, slack_user_id)`.
- `mcp_oauth_resume_requests` persists the original prompt and Slack thread
  location, so the HTTP callback can resume the correct conversation after
  authorization.

## Current constraints

- OAuth pending authorization state and the PKCE verifier are held in process
  memory, without expiry cleanup. The callback must reach a live process that
  retains that state.
- Expired tokens currently require reconnecting; the token resolver does not
  perform refresh-token exchange.
- The public OAuth start endpoint accepts `team_id`, `user_id`, and
  `request_id` as query parameters but does not validate them against a
  server-side pending Slack authorization before signing state and exchanging
  tokens. Token persistence therefore occurs before the resumed conversation
  validates request ownership.
- The optional `scope` and `resource_metadata` hints are accepted from the
  public start URL without binding them to the saved resume request.

## Security hardening priorities

1. Resolve the start request from a short-lived, server-side record created by
   the Slack runtime; reject absent, expired, or mismatched team/user/server
   bindings before authorization begins.
2. Bind the requested scope and protected-resource metadata to that same
   record rather than accepting browser-supplied values.
3. Persist pending PKCE state with a TTL so callbacks can safely survive a
   process restart or be rejected deterministically after expiry.
4. Implement refresh-token exchange or explicitly revoke expired records and
   guide the user through reauthorization.
