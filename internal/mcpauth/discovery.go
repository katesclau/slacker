package mcpauth

import (
	"context"
	"encoding/json"
	"fmt"
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("resource metadata lookup failed with status %d", resp.StatusCode)
	}
	var out ProtectedResourceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode resource metadata: %w", err)
	}
	if len(out.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("resource metadata missing authorization_servers")
	}
	return &out, nil
}

func (s *Service) discoverAuthorizationServerMetadata(ctx context.Context, issuer string) (*AuthorizationServerMetadata, error) {
	wellKnown := strings.TrimSuffix(strings.TrimSpace(issuer), "/") + "/.well-known/oauth-authorization-server"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("authorization server metadata lookup failed with status %d", resp.StatusCode)
	}
	var out AuthorizationServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode authorization server metadata: %w", err)
	}
	if out.AuthorizationEndpoint == "" || out.TokenEndpoint == "" {
		return nil, fmt.Errorf("authorization server metadata missing required endpoints")
	}
	if out.Issuer == "" {
		out.Issuer = issuer
	}
	return &out, nil
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
