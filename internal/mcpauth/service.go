package mcpauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Service struct {
	MCPServer     string
	ResourceURL   string
	PublicBaseURL string
	CallbackPath  string
	StateHMACKey  []byte
	Store         TokenStore
	Cipher        *TokenCipher
	Registration  RegistrationConfig
	HTTPClient    *http.Client

	mu          sync.Mutex
	pendingAuth map[string]pendingAuth
}

func (s *Service) Validate() error {
	if strings.TrimSpace(s.MCPServer) == "" {
		return fmt.Errorf("mcp server is required")
	}
	if strings.TrimSpace(s.ResourceURL) == "" {
		return fmt.Errorf("resource url is required")
	}
	if strings.TrimSpace(s.PublicBaseURL) == "" {
		return fmt.Errorf("public base url is required")
	}
	if len(s.StateHMACKey) < 16 {
		return fmt.Errorf("state hmac key must be at least 16 bytes")
	}
	if s.Store == nil || s.Cipher == nil {
		return fmt.Errorf("store and cipher are required")
	}
	return nil
}

func (s *Service) AuthorizeURL(ctx context.Context, teamID string, slackUserID string, requestID string, scopeHint string, resourceMetadataURL string) (string, error) {
	slog.Debug("mcpauth authorize start",
		"mcp_server", s.MCPServer,
		"team_id", teamID,
		"user_id", slackUserID,
		"request_id", requestID,
		"scope_hint", scopeHint,
		"resource_metadata_url_present", strings.TrimSpace(resourceMetadataURL) != "",
	)
	if err := s.Validate(); err != nil {
		slog.Debug("mcpauth authorize validate failed", "mcp_server", s.MCPServer, "error", err)
		return "", err
	}
	md, err := s.discoverProtectedResourceMetadata(ctx, resourceMetadataURL)
	if err != nil {
		fallbackIssuer := strings.TrimSpace(s.Registration.AuthorizationServerIssuer)
		if fallbackIssuer == "" {
			slog.Debug("mcpauth authorize protected resource discovery failed without fallback", "mcp_server", s.MCPServer, "error", err)
			return "", err
		}
		slog.Debug("mcpauth authorize using protected resource fallback issuer", "mcp_server", s.MCPServer, "fallback_issuer", fallbackIssuer)
		md = &ProtectedResourceMetadata{
			Resource:             s.ResourceURL,
			AuthorizationServers: []string{fallbackIssuer},
			ScopesSupported:      s.Registration.Scopes,
		}
	}

	as, err := s.discoverAuthorizationServerMetadata(ctx, md.AuthorizationServers[0])
	if err != nil {
		slog.Debug("mcpauth authorize auth server discovery failed", "mcp_server", s.MCPServer, "issuer", md.AuthorizationServers[0], "error", err)
		return "", err
	}
	slog.Debug("mcpauth authorize auth server resolved", "mcp_server", s.MCPServer, "issuer", as.Issuer, "authorization_endpoint", as.AuthorizationEndpoint, "token_endpoint", as.TokenEndpoint)
	clientID := strings.TrimSpace(s.Registration.ClientID)
	if clientID == "" {
		return "", fmt.Errorf("registration client id is required")
	}
	clientSecret := strings.TrimSpace(s.Registration.ClientSecret)
	codeVerifier, codeChallenge, err := generatePKCEVerifierAndChallenge()
	if err != nil {
		return "", err
	}

	st := OAuthState{
		MCPServer:   s.MCPServer,
		RequestID:   requestID,
		SlackTeamID: teamID,
		SlackUserID: slackUserID,
		Resource:    firstNonEmpty(md.Resource, s.ResourceURL),
		ASIssuer:    as.Issuer,
		Nonce:       randomNonce(16),
	}
	rawState, err := signState(s.StateHMACKey, st)
	if err != nil {
		return "", err
	}

	scope := scopeString(scopeHint, firstNonEmptySlice(s.Registration.Scopes, md.ScopesSupported))
	slog.Debug("mcpauth authorize pending auth stored", "mcp_server", s.MCPServer, "nonce", st.Nonce, "scope", scope, "resource", st.Resource, "issuer", st.ASIssuer)
	s.putPending(pendingAuth{
		Nonce:         st.Nonce,
		CreatedAt:     time.Now().UTC(),
		Resource:      st.Resource,
		Issuer:        st.ASIssuer,
		RequiredScope: scope,
		CodeVerifier:  codeVerifier,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
	})

	params := map[string]string{
		"response_type":         "code",
		"client_id":             clientID,
		"redirect_uri":          s.redirectURI(),
		"state":                 rawState,
		"code_challenge":        codeChallenge,
		"code_challenge_method": "S256",
		"resource":              st.Resource,
		"scope":                 scope,
	}
	u, err := setURLQuery(as.AuthorizationEndpoint, params)
	if err != nil {
		return "", err
	}
	slog.Debug("mcpauth authorize url generated", "mcp_server", s.MCPServer, "redirect_uri", s.redirectURI(), "authorization_url", u)
	return u, nil
}

func (s *Service) ExchangeCallback(ctx context.Context, code string, rawState string) (*OAuthState, error) {
	slog.Debug("mcpauth callback exchange start", "mcp_server", s.MCPServer, "code_len", len(code), "state_len", len(rawState))
	if err := s.Validate(); err != nil {
		return nil, err
	}
	st, err := parseAndVerifyState(s.StateHMACKey, rawState)
	if err != nil {
		slog.Debug("mcpauth callback state verify failed", "mcp_server", s.MCPServer, "error", err)
		return nil, err
	}
	slog.Debug("mcpauth callback state verified", "mcp_server", s.MCPServer, "nonce", st.Nonce, "team_id", st.SlackTeamID, "user_id", st.SlackUserID, "resource", st.Resource, "issuer", st.ASIssuer)
	pending, ok := s.popPending(st.Nonce)
	if !ok {
		slog.Debug("mcpauth callback pending auth missing", "mcp_server", s.MCPServer, "nonce", st.Nonce)
		return nil, fmt.Errorf("authorization state was not found or expired")
	}
	if pending.Resource != st.Resource || pending.Issuer != st.ASIssuer {
		slog.Debug("mcpauth callback pending auth mismatch", "mcp_server", s.MCPServer, "pending_resource", pending.Resource, "state_resource", st.Resource, "pending_issuer", pending.Issuer, "state_issuer", st.ASIssuer)
		return nil, fmt.Errorf("state resource/issuer mismatch")
	}
	as, err := s.discoverAuthorizationServerMetadata(ctx, st.ASIssuer)
	if err != nil {
		slog.Debug("mcpauth callback auth server discovery failed", "mcp_server", s.MCPServer, "issuer", st.ASIssuer, "error", err)
		return nil, err
	}
	tokenRes, err := s.exchangeToken(ctx, as.TokenEndpoint, pending, code)
	if err != nil {
		slog.Debug("mcpauth callback token exchange failed", "mcp_server", s.MCPServer, "token_endpoint", as.TokenEndpoint, "error", err)
		return nil, err
	}
	slog.Debug("mcpauth callback token exchange succeeded", "mcp_server", s.MCPServer, "expires_at", tokenRes.ExpiresAt, "has_refresh_token", tokenRes.RefreshToken != "")

	encAccess, err := s.Cipher.EncryptToBase64(tokenRes.AccessToken)
	if err != nil {
		return nil, err
	}
	encRefresh := ""
	if tokenRes.RefreshToken != "" {
		encRefresh, err = s.Cipher.EncryptToBase64(tokenRes.RefreshToken)
		if err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	record := TokenRecord{
		MCPServer:     s.MCPServer,
		SlackTeamID:   st.SlackTeamID,
		SlackUserID:   st.SlackUserID,
		Resource:      st.Resource,
		Issuer:        st.ASIssuer,
		ClientID:      pending.ClientID,
		EncAccess:     encAccess,
		EncRefresh:    encRefresh,
		Scope:         strings.ReplaceAll(firstNonEmpty(tokenRes.Scope, pending.RequiredScope), " ", ","),
		ExpiresAt:     tokenRes.ExpiresAt,
		LastUpdatedAt: now,
	}
	if existing, err := s.Store.GetDelegatedOAuthToken(ctx, st.SlackTeamID, st.SlackUserID, s.MCPServer); err == nil && existing != nil {
		record.CreatedAt = existing.CreatedAt
	} else {
		record.CreatedAt = now
	}
	if err := s.Store.PutDelegatedOAuthToken(ctx, record); err != nil {
		slog.Debug("mcpauth callback token persistence failed", "mcp_server", s.MCPServer, "team_id", st.SlackTeamID, "user_id", st.SlackUserID, "error", err)
		return nil, err
	}
	slog.Debug("mcpauth callback token persisted", "mcp_server", s.MCPServer, "team_id", st.SlackTeamID, "user_id", st.SlackUserID, "expires_at", record.ExpiresAt)
	return &st, nil
}

func (s *Service) redirectURI() string {
	path := strings.TrimSpace(s.CallbackPath)
	if path == "" {
		path = fmt.Sprintf("/slacker/v1/oauth/%s/callback", url.PathEscape(s.MCPServer))
	}
	return strings.TrimSuffix(s.PublicBaseURL, "/") + path
}

func (s *Service) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (s *Service) exchangeToken(ctx context.Context, endpoint string, pending pendingAuth, code string) (*struct {
	AccessToken  string
	RefreshToken string
	Scope        string
	ExpiresAt    time.Time
}, error) {
	slog.Debug("mcpauth token exchange request", "mcp_server", s.MCPServer, "endpoint", endpoint, "client_id", pending.ClientID, "has_client_secret", pending.ClientSecret != "", "resource", pending.Resource)
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", s.redirectURI())
	form.Set("client_id", pending.ClientID)
	form.Set("code_verifier", pending.CodeVerifier)
	form.Set("resource", pending.Resource)
	if pending.ClientSecret != "" {
		form.Set("client_secret", pending.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		slog.Debug("mcpauth token exchange http failed", "mcp_server", s.MCPServer, "endpoint", endpoint, "error", err)
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Debug("mcpauth token exchange non-success status", "mcp_server", s.MCPServer, "status", resp.StatusCode)
		return nil, fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		values, parseErr := url.ParseQuery(string(body))
		if parseErr != nil {
			return nil, err
		}
		tr.AccessToken = values.Get("access_token")
		tr.RefreshToken = values.Get("refresh_token")
		tr.Scope = values.Get("scope")
	}
	if tr.AccessToken == "" {
		slog.Debug("mcpauth token exchange empty access token", "mcp_server", s.MCPServer)
		return nil, fmt.Errorf("provider returned empty access token")
	}
	expiresAt := time.Now().Add(time.Hour)
	if tr.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return &struct {
		AccessToken  string
		RefreshToken string
		Scope        string
		ExpiresAt    time.Time
	}{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		Scope:        tr.Scope,
		ExpiresAt:    expiresAt,
	}, nil
}

func (s *Service) putPending(p pendingAuth) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingAuth == nil {
		s.pendingAuth = map[string]pendingAuth{}
	}
	s.pendingAuth[p.Nonce] = p
}

func (s *Service) popPending(nonce string) (pendingAuth, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pendingAuth[nonce]
	if !ok {
		return pendingAuth{}, false
	}
	delete(s.pendingAuth, nonce)
	return p, true
}

func generatePKCEVerifierAndChallenge() (string, string, error) {
	buf := make([]byte, 64)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func randomNonce(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("nonce-%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
