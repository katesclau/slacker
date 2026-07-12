package mcpauth

import (
	"context"
	"time"
)

type TokenRecord struct {
	MCPServer     string
	SlackTeamID   string
	SlackUserID   string
	Resource      string
	Issuer        string
	ClientID      string
	EncAccess     string
	EncRefresh    string
	Scope         string
	ExpiresAt     time.Time
	CreatedAt     time.Time
	LastUpdatedAt time.Time
}

type TokenStore interface {
	PutDelegatedOAuthToken(ctx context.Context, rec TokenRecord) error
	GetDelegatedOAuthToken(ctx context.Context, teamID string, userID string, mcpServer string) (*TokenRecord, error)
}

type RegistrationStore interface {
	SaveMCPRegistration(ctx context.Context, mcpServer string, mode string, clientID string, clientSecret string) error
}

type RegistrationConfig struct {
	ClientID                  string
	ClientSecret              string
	Scopes                    []string
	AuthorizationServerIssuer string
	Mode                      string
	ClientName                string
}

type OAuthState struct {
	MCPServer   string `json:"mcp_server"`
	RequestID   string `json:"request_id"`
	SlackTeamID string `json:"slack_team_id"`
	SlackUserID string `json:"slack_user_id"`
	Resource    string `json:"resource"`
	ASIssuer    string `json:"as_issuer"`
	Nonce       string `json:"nonce"`
}

type pendingAuth struct {
	Nonce         string
	CreatedAt     time.Time
	Resource      string
	Issuer        string
	RequiredScope string
	CodeVerifier  string
	ClientID      string
	ClientSecret  string
}
