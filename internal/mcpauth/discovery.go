package mcpauth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type AuthorizationServerMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

func (s *Service) discoverProtectedResourceMetadata(ctx context.Context, resourceMetadataURL string) (*ProtectedResourceMetadata, error) {
	resource := strings.TrimSpace(resourceMetadataURL)
	if resource == "" {
		resource = strings.TrimSuffix(s.ResourceURL, "/") + "/.well-known/oauth-protected-resource"
	}
	slog.Debug("mcpauth discover protected resource metadata", "mcp_server", s.MCPServer, "url", resource)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		slog.Debug("mcpauth discover protected resource request failed", "mcp_server", s.MCPServer, "url", resource, "error", err)
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Debug("mcpauth discover protected resource non-success status", "mcp_server", s.MCPServer, "url", resource, "status", resp.StatusCode)
		return nil, fmt.Errorf("resource metadata lookup failed with status %d", resp.StatusCode)
	}
	var out ProtectedResourceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		slog.Debug("mcpauth decode protected resource metadata failed", "mcp_server", s.MCPServer, "url", resource, "error", err)
		return nil, fmt.Errorf("decode resource metadata: %w", err)
	}
	if len(out.AuthorizationServers) == 0 {
		slog.Debug("mcpauth protected resource metadata missing authorization servers", "mcp_server", s.MCPServer, "url", resource)
		return nil, fmt.Errorf("resource metadata missing authorization_servers")
	}
	slog.Debug("mcpauth protected resource metadata resolved", "mcp_server", s.MCPServer, "resource", out.Resource, "authorization_servers", strings.Join(out.AuthorizationServers, ","))
	return &out, nil
}

func (s *Service) discoverAuthorizationServerMetadata(ctx context.Context, issuer string) (*AuthorizationServerMetadata, error) {
	issuer = strings.TrimSpace(issuer)
	wellKnown := strings.TrimSuffix(issuer, "/") + "/.well-known/oauth-authorization-server"
	slog.Debug("mcpauth discover authorization server metadata", "mcp_server", s.MCPServer, "issuer", issuer, "url", wellKnown)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		slog.Debug("mcpauth discover authorization server request failed", "mcp_server", s.MCPServer, "issuer", issuer, "error", err)
		if fallback := fallbackAuthorizationServerMetadata(issuer); fallback != nil {
			slog.Debug("mcpauth discover authorization server using fallback metadata", "mcp_server", s.MCPServer, "issuer", issuer, "authorization_endpoint", fallback.AuthorizationEndpoint, "token_endpoint", fallback.TokenEndpoint)
			return fallback, nil
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Debug("mcpauth discover authorization server non-success status", "mcp_server", s.MCPServer, "issuer", issuer, "status", resp.StatusCode)
		if fallback := fallbackAuthorizationServerMetadata(issuer); fallback != nil {
			slog.Debug("mcpauth discover authorization server using fallback metadata", "mcp_server", s.MCPServer, "issuer", issuer, "authorization_endpoint", fallback.AuthorizationEndpoint, "token_endpoint", fallback.TokenEndpoint)
			return fallback, nil
		}
		return nil, fmt.Errorf("authorization server metadata lookup failed with status %d", resp.StatusCode)
	}
	var out AuthorizationServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		slog.Debug("mcpauth decode authorization server metadata failed", "mcp_server", s.MCPServer, "issuer", issuer, "error", err)
		if fallback := fallbackAuthorizationServerMetadata(issuer); fallback != nil {
			slog.Debug("mcpauth discover authorization server using fallback metadata", "mcp_server", s.MCPServer, "issuer", issuer, "authorization_endpoint", fallback.AuthorizationEndpoint, "token_endpoint", fallback.TokenEndpoint)
			return fallback, nil
		}
		return nil, fmt.Errorf("decode authorization server metadata: %w", err)
	}
	if out.AuthorizationEndpoint == "" || out.TokenEndpoint == "" {
		slog.Debug("mcpauth authorization server metadata missing endpoints", "mcp_server", s.MCPServer, "issuer", issuer, "authorization_endpoint", out.AuthorizationEndpoint, "token_endpoint", out.TokenEndpoint)
		if fallback := fallbackAuthorizationServerMetadata(issuer); fallback != nil {
			slog.Debug("mcpauth discover authorization server using fallback metadata", "mcp_server", s.MCPServer, "issuer", issuer, "authorization_endpoint", fallback.AuthorizationEndpoint, "token_endpoint", fallback.TokenEndpoint)
			return fallback, nil
		}
		return nil, fmt.Errorf("authorization server metadata missing required endpoints")
	}
	if out.Issuer == "" {
		out.Issuer = issuer
	}
	slog.Debug("mcpauth authorization server metadata resolved", "mcp_server", s.MCPServer, "issuer", out.Issuer, "authorization_endpoint", out.AuthorizationEndpoint, "token_endpoint", out.TokenEndpoint)
	return &out, nil
}

func fallbackAuthorizationServerMetadata(issuer string) *AuthorizationServerMetadata {
	issuer = strings.TrimSuffix(strings.TrimSpace(issuer), "/")
	if issuer == "" {
		return nil
	}
	switch {
	case strings.HasSuffix(issuer, "github.com/login/oauth"):
		return &AuthorizationServerMetadata{
			Issuer:                issuer,
			AuthorizationEndpoint: issuer + "/authorize",
			TokenEndpoint:         issuer + "/access_token",
		}
	}
	return nil
}

func scopeString(hint string, scopes []string) string {
	hint = strings.TrimSpace(hint)
	if hint != "" {
		return hint
	}
	return strings.TrimSpace(strings.Join(scopes, " "))
}

func setURLQuery(baseURL string, values map[string]string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for k, v := range values {
		if strings.TrimSpace(v) != "" {
			query.Set(k, v)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonEmptySlice(primary []string, fallback []string) []string {
	if len(primary) > 0 {
		return primary
	}
	return fallback
}
