# Slacker C4 container diagram

This container-level diagram shows Slacker's principal runtime components,
state stores, and external integrations.

The visual C4 diagram is maintained in
[c4-diagram.excalidraw](c4-diagram.excalidraw).

## Notes

- Agent definitions are stored in PostgreSQL and can include an MCP server
  allow-list, model override, and agent instruction.
- Local Block Kit tools are available to the agent runtime in addition to MCP
  tools.
- OAuth token encryption uses the application-configured symmetric key
  (`TOKEN_ENCRYPTION_KEY_BASE64`) and AES-256-GCM; unlike `mcp-slackitt`, the
  checked-in Slacker implementation does not use AWS KMS for token encryption.
- `mcp_servers.client_secret_enc` is not reliably encrypted in the current
  static/DCR configuration paths despite its name; treat it as a credential
  storage concern until that behavior is corrected.
- The detailed authorization-code flow is documented in
  [oauth-flow.md](oauth-flow.md).
