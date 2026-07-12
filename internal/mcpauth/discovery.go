package mcpauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	RegistrationEndpoint  string `json:"registration_endpoint"`
}

func (s *Service) discoverProtectedResourceMetadata(ctx context.Context, resourceMetadataURL string) (*ProtectedResourceMetadata, error) {
	candidates, err := s.protectedResourceCandidates(resourceMetadataURL)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, resource := range candidates {
		slog.Debug("mcpauth discover protected resource metadata", "mcp_server", s.MCPServer, "url", resource)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := s.httpClient().Do(req)
		if err != nil {
			slog.Debug("mcpauth discover protected resource request failed", "mcp_server", s.MCPServer, "url", resource, "error", err)
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			slog.Debug("mcpauth discover protected resource non-success status", "mcp_server", s.MCPServer, "url", resource, "status", resp.StatusCode)
			lastErr = fmt.Errorf("resource metadata lookup failed with status %d", resp.StatusCode)
			continue
		}
		var out ProtectedResourceMetadata
		if err := json.Unmarshal(body, &out); err != nil {
			slog.Debug("mcpauth decode protected resource metadata failed", "mcp_server", s.MCPServer, "url", resource, "error", err)
			lastErr = fmt.Errorf("decode resource metadata: %w", err)
			continue
		}
		if len(out.AuthorizationServers) == 0 {
			slog.Debug("mcpauth protected resource metadata missing authorization servers", "mcp_server", s.MCPServer, "url", resource)
			lastErr = fmt.Errorf("resource metadata missing authorization_servers")
			continue
		}
		slog.Debug("mcpauth protected resource metadata resolved", "mcp_server", s.MCPServer, "resource", out.Resource, "authorization_servers", strings.Join(out.AuthorizationServers, ","))
		return &out, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("resource metadata lookup failed")
	}
	return nil, lastErr
}

func (s *Service) discoverAuthorizationServerMetadata(ctx context.Context, issuer string) (*AuthorizationServerMetadata, error) {
	issuer = strings.TrimSpace(issuer)
	candidates, err := authorizationServerMetadataCandidates(issuer)
	if err != nil {
		return nil, err
	}
	var best *AuthorizationServerMetadata
	var lastErr error
	for _, metadataURL := range candidates {
		slog.Debug("mcpauth discover authorization server metadata", "mcp_server", s.MCPServer, "issuer", issuer, "url", metadataURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := s.httpClient().Do(req)
		if err != nil {
			slog.Debug("mcpauth discover authorization server request failed", "mcp_server", s.MCPServer, "issuer", issuer, "url", metadataURL, "error", err)
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			slog.Debug("mcpauth discover authorization server non-success status", "mcp_server", s.MCPServer, "issuer", issuer, "url", metadataURL, "status", resp.StatusCode)
			lastErr = fmt.Errorf("authorization server metadata lookup failed with status %d", resp.StatusCode)
			continue
		}
		var out AuthorizationServerMetadata
		if err := json.Unmarshal(body, &out); err != nil {
			slog.Debug("mcpauth decode authorization server metadata failed", "mcp_server", s.MCPServer, "issuer", issuer, "url", metadataURL, "error", err)
			lastErr = fmt.Errorf("decode authorization server metadata: %w", err)
			continue
		}
		if out.AuthorizationEndpoint == "" || out.TokenEndpoint == "" {
			slog.Debug("mcpauth authorization server metadata missing endpoints", "mcp_server", s.MCPServer, "issuer", issuer, "url", metadataURL, "authorization_endpoint", out.AuthorizationEndpoint, "token_endpoint", out.TokenEndpoint)
			lastErr = fmt.Errorf("authorization server metadata missing required endpoints")
			continue
		}
		if out.Issuer == "" {
			out.Issuer = issuer
		}
		if best == nil {
			c := out
			best = &c
		}
		// Prefer metadata that includes registration_endpoint for DCR support.
		if strings.TrimSpace(out.RegistrationEndpoint) != "" {
			slog.Debug("mcpauth authorization server metadata resolved", "mcp_server", s.MCPServer, "issuer", out.Issuer, "authorization_endpoint", out.AuthorizationEndpoint, "token_endpoint", out.TokenEndpoint, "registration_endpoint", out.RegistrationEndpoint)
			return &out, nil
		}
	}
	if best != nil {
		slog.Debug("mcpauth authorization server metadata resolved best candidate", "mcp_server", s.MCPServer, "issuer", best.Issuer, "authorization_endpoint", best.AuthorizationEndpoint, "token_endpoint", best.TokenEndpoint, "registration_endpoint", best.RegistrationEndpoint)
		return best, nil
	}
	if fallback := fallbackAuthorizationServerMetadata(issuer); fallback != nil {
		slog.Debug("mcpauth discover authorization server using fallback metadata", "mcp_server", s.MCPServer, "issuer", issuer, "authorization_endpoint", fallback.AuthorizationEndpoint, "token_endpoint", fallback.TokenEndpoint)
		return fallback, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("authorization server metadata lookup failed")
	}
	return nil, lastErr
}

func (s *Service) protectedResourceCandidates(resourceMetadataURL string) ([]string, error) {
	if v := strings.TrimSpace(resourceMetadataURL); v != "" {
		return []string{v}, nil
	}
	resourceURL := strings.TrimSpace(s.ResourceURL)
	u, err := url.Parse(resourceURL)
	if err != nil {
		return nil, fmt.Errorf("parse resource url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("resource url must be absolute")
	}
	base := fmt.Sprintf("%s://%s", strings.ToLower(u.Scheme), strings.ToLower(u.Host))
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	candidates := make([]string, 0, 2)
	if path != "" {
		candidates = append(candidates, base+"/.well-known/oauth-protected-resource"+path)
	}
	candidates = append(candidates, base+"/.well-known/oauth-protected-resource")
	return candidates, nil
}

func authorizationServerMetadataCandidates(issuer string) ([]string, error) {
	u, err := url.Parse(strings.TrimSpace(issuer))
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("issuer must be absolute")
	}
	base := fmt.Sprintf("%s://%s", strings.ToLower(u.Scheme), strings.ToLower(u.Host))
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	if path == "" {
		return []string{
			base + "/.well-known/oauth-authorization-server",
			base + "/.well-known/openid-configuration",
		}, nil
	}
	return []string{
		base + "/.well-known/oauth-authorization-server" + path,
		base + "/.well-known/openid-configuration" + path,
		strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration",
	}, nil
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
