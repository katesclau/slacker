# MCP OAuth client registration

Slacker supports two ways to obtain the OAuth client identity used to connect
to an MCP server:

- **Provisioned client** (`static`): an administrator creates the OAuth client
  before configuring Slacker.
- **Dynamic Client Registration** (`dcr`): Slacker creates the OAuth client
  through the authorization server's registration endpoint.

Both approaches use the OAuth 2.0 authorization-code flow with PKCE. They
differ only in how Slacker obtains the application's client ID and secret;
each Slack user still completes consent and receives a separately stored,
delegated token.

## Provisioned client (`static`)

Use a provisioned client when an OAuth application is created and managed
outside Slacker. GitHub is the typical example: create a GitHub OAuth App,
configure its callback URL, and add its client ID, client secret, and scopes to
the Slacker MCP-server configuration.

All Slack users authorize through the same GitHub OAuth App, but Slacker stores
each resulting token under the combination of MCP server, Slack workspace, and
Slack user.

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    actor User as Slack user
    participant GitHub as GitHub OAuth
    participant Slacker
    participant DB as PostgreSQL
    participant MCP as GitHub MCP server

    Admin->>GitHub: Create OAuth App<br/>and configure callback URL
    GitHub-->>Admin: Client ID and client secret
    Admin->>Slacker: Configure static MCP server<br/>with client credentials and scopes
    Slacker->>DB: Store MCP server configuration

    User->>Slacker: Request GitHub MCP work
    Slacker->>GitHub: Redirect to authorize endpoint<br/>with configured client ID + PKCE
    User->>GitHub: Authenticate and grant consent
    GitHub->>Slacker: Callback with authorization code
    Slacker->>GitHub: Exchange code using configured<br/>client credentials + PKCE verifier
    GitHub-->>Slacker: User access and refresh tokens
    Slacker->>DB: Store encrypted token for<br/>(github, Slack team, Slack user)
    Slacker->>MCP: Call tool with user's Bearer token
```

## Dynamic Client Registration (`dcr`)

Use DCR when the MCP authorization server exposes a
`registration_endpoint`. Jira/Atlassian is the primary example. On its first
connection, Slacker discovers the authorization-server metadata and registers
itself, supplying its callback URL, supported grant type, PKCE-compatible
authorization-code flow, and requested scopes.

The authorization server returns a client ID and may return a client secret.
Slacker persists those registration credentials and uses them for the
subsequent user authorization-code exchange.

```mermaid
sequenceDiagram
    autonumber
    actor User as Slack user
    participant Slacker
    participant Jira as Jira / Atlassian OAuth
    participant DB as PostgreSQL
    participant MCP as Atlassian MCP server

    User->>Slacker: Request Jira MCP work
    Slacker->>DB: Load DCR MCP server configuration
    Slacker->>Jira: Discover authorization metadata
    Jira-->>Slacker: Authorization, token, and<br/>registration endpoints
    Slacker->>Jira: Register client with callback URL,<br/>grant type, PKCE, and scopes
    Jira-->>Slacker: Dynamically issued client ID<br/>and optional client secret
    Slacker->>DB: Persist DCR client credentials

    Slacker->>Jira: Redirect to authorize endpoint<br/>with dynamically issued client ID + PKCE
    User->>Jira: Authenticate and grant consent
    Jira->>Slacker: Callback with authorization code
    Slacker->>Jira: Exchange code using DCR client<br/>credentials + PKCE verifier
    Jira-->>Slacker: User access and refresh tokens
    Slacker->>DB: Store encrypted token for<br/>(atlassian, Slack team, Slack user)
    Slacker->>MCP: Call tool with user's Bearer token
```

## Comparison

| Concern | Provisioned client (GitHub) | DCR (Jira/Atlassian) |
| --- | --- | --- |
| Client created by | Administrator, before Slacker configuration | Slacker, at runtime |
| Initial configuration | Client ID, client secret, callback URL, scopes | Resource URL, issuer, DCR mode, client name, scopes |
| Registration endpoint | Not used | Required |
| Client identity | One pre-approved OAuth App | Created by the authorization server |
| User consent | Required for every Slack user | Required for every Slack user |
| Delegated token scope | MCP server + Slack team + Slack user | MCP server + Slack team + Slack user |

## Security considerations

- DCR creates an application client identity; it does not grant a user access
  without that user's OAuth consent.
- Slacker uses signed state and PKCE for both modes.
- Access and refresh tokens are encrypted before being saved in
  `mcp_oauth_tokens`.
- The current `client_secret_enc` storage path is not reliably encrypted for
  static or DCR client secrets despite its name; do not treat it as secure
  secret storage until this is corrected.
- Expired delegated tokens currently require reconnection because Slacker does
  not yet implement refresh-token exchange.
