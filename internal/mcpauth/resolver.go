package mcpauth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type TokenResolver struct {
	Store  TokenStore
	Cipher *TokenCipher
}

func (r TokenResolver) ResolveBearerToken(ctx context.Context, teamID string, userID string, server string) (string, error) {
	slog.Debug("mcp oauth token lookup started", "mcp_server", server, "team_id", teamID, "user_id", userID)
	rec, err := r.Store.GetDelegatedOAuthToken(ctx, teamID, userID, server)
	if err != nil {
		slog.Debug("mcp oauth token lookup failed", "mcp_server", server, "team_id", teamID, "user_id", userID, "error", err)
		return "", err
	}
	if rec == nil {
		slog.Debug("mcp oauth token missing", "mcp_server", server, "team_id", teamID, "user_id", userID)
		return "", fmt.Errorf("no delegated oauth token found for server %q", server)
	}
	if !rec.ExpiresAt.IsZero() && rec.ExpiresAt.Before(time.Now()) {
		slog.Debug("mcp oauth token expired", "mcp_server", server, "team_id", teamID, "user_id", userID, "expires_at", rec.ExpiresAt)
		return "", fmt.Errorf("delegated oauth token expired for server %q", server)
	}
	token, err := r.Cipher.DecryptFromBase64(rec.EncAccess)
	if err != nil {
		slog.Debug("mcp oauth token decrypt failed", "mcp_server", server, "team_id", teamID, "user_id", userID, "error", err)
		return "", err
	}
	slog.Debug("mcp oauth token ready", "mcp_server", server, "team_id", teamID, "user_id", userID, "issuer", rec.Issuer, "resource", rec.Resource, "scope_csv", rec.Scope, "expires_at", rec.ExpiresAt, "has_refresh_token", rec.EncRefresh != "")
	return token, nil
}

type BearerTransport struct {
	Base      http.RoundTripper
	GetBearer func(ctx context.Context) (string, error)
}

func (t BearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt := t.Base
	if rt == nil {
		rt = http.DefaultTransport
	}
	clone := req.Clone(req.Context())
	if t.GetBearer != nil {
		token, err := t.GetBearer(req.Context())
		if err != nil {
			return nil, err
		}
		clone.Header = clone.Header.Clone()
		clone.Header.Set("Authorization", "Bearer "+token)
	}
	return rt.RoundTrip(clone)
}
