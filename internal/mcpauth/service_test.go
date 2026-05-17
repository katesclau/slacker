package mcpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthorizeAndExchangeCallback(t *testing.T) {
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":                 issuer.URL,
				"authorization_endpoint": issuer.URL + "/authorize",
				"token_endpoint":         tokenSrvURL(t),
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer issuer.Close()

	resource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              "https://api.example.com",
			"authorization_servers": []string{issuer.URL},
			"scopes_supported":      []string{"read", "write"},
		})
	}))
	defer resource.Close()

	tokenCipher, err := NewTokenCipher([]byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	store := &memoryTokenStore{}
	svc := &Service{
		MCPServer:     "demo",
		ResourceURL:   "https://api.example.com",
		PublicBaseURL: "http://localhost:8080",
		StateHMACKey:  []byte("1234567890123456"),
		Store:         store,
		Cipher:        tokenCipher,
		Registration: RegistrationConfig{
			ClientID:                  "client-id",
			ClientSecret:              "client-secret",
			Scopes:                    []string{"read"},
			AuthorizationServerIssuer: issuer.URL,
		},
		HTTPClient: &http.Client{Timeout: time.Second * 2},
	}

	u, err := svc.AuthorizeURL(context.Background(), "T1", "U1", "req-1", "read", resource.URL+"/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatalf("authorize url: %v", err)
	}
	if !strings.Contains(u, "state=") {
		t.Fatalf("authorize URL missing state: %s", u)
	}

	parts := strings.Split(u, "state=")
	if len(parts) < 2 {
		t.Fatalf("invalid authorize URL: %s", u)
	}
	state := strings.Split(parts[1], "&")[0]
	if _, err := svc.ExchangeCallback(context.Background(), "auth-code", state); err != nil {
		t.Fatalf("exchange callback: %v", err)
	}
	if store.rec == nil {
		t.Fatal("expected token to be persisted")
	}
}

func tokenSrvURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"scope":         "read",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

type memoryTokenStore struct {
	rec *TokenRecord
}

func (m *memoryTokenStore) PutDelegatedOAuthToken(_ context.Context, rec TokenRecord) error {
	cpy := rec
	m.rec = &cpy
	return nil
}

func (m *memoryTokenStore) GetDelegatedOAuthToken(_ context.Context, _ string, _ string, _ string) (*TokenRecord, error) {
	return m.rec, nil
}
