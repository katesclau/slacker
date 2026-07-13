package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/katesclau/slacker/internal/mcpauth"
)

func TestOAuthStartAndCallbackHandlers(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	var issuerServer *httptest.Server
	issuerServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":                 issuerServer.URL,
				"authorization_endpoint": issuerServer.URL + "/authorize",
				"token_endpoint":         tokenServer.URL,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer issuerServer.Close()

	resourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              "https://resource.example",
			"authorization_servers": []string{issuerServer.URL},
			"scopes_supported":      []string{"read"},
		})
	}))
	defer resourceServer.Close()

	cipher, _ := mcpauth.NewTokenCipher([]byte("12345678901234567890123456789012"))
	auth := &mcpauth.Service{
		MCPServer:     "demo",
		ResourceURL:   "https://resource.example",
		PublicBaseURL: "http://localhost:8080",
		StateHMACKey:  []byte("1234567890123456"),
		Store:         &testTokenStore{},
		Cipher:        cipher,
		Registration: mcpauth.RegistrationConfig{
			ClientID:                  "client-id",
			ClientSecret:              "client-secret",
			Scopes:                    []string{"read"},
			AuthorizationServerIssuer: issuerServer.URL,
		},
	}

	s := &Server{
		log: slog.Default(),
		authService: func(context.Context, string) (*mcpauth.Service, error) {
			return auth, nil
		},
	}

	startReq := httptest.NewRequest(http.MethodGet, "/slacker/v1/oauth/demo/start?team_id=T1&user_id=U1&request_id=R1&resource_metadata="+resourceServer.URL, nil)
	startReq = withURLParam(startReq, "mcp_server", "demo")
	startRes := httptest.NewRecorder()
	s.oauthStart(startRes, startReq)
	if startRes.Code != http.StatusFound {
		t.Fatalf("expected status 302, got %d", startRes.Code)
	}
	location := startRes.Header().Get("Location")
	if !strings.Contains(location, "state=") {
		t.Fatalf("expected redirect with oauth state, got %s", location)
	}
	state := strings.Split(strings.Split(location, "state=")[1], "&")[0]

	cbReq := httptest.NewRequest(http.MethodGet, "/slacker/v1/oauth/demo/callback?code=abc&state="+state, nil)
	cbReq = withURLParam(cbReq, "mcp_server", "demo")
	cbRes := httptest.NewRecorder()
	s.oauthCallback(cbRes, cbReq)
	if cbRes.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", cbRes.Code)
	}
}

func TestOAuthCallbackTriggersResumeHandler(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	var issuerServer *httptest.Server
	issuerServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":                 issuerServer.URL,
				"authorization_endpoint": issuerServer.URL + "/authorize",
				"token_endpoint":         tokenServer.URL,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer issuerServer.Close()

	resourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              "https://resource.example",
			"authorization_servers": []string{issuerServer.URL},
			"scopes_supported":      []string{"read"},
		})
	}))
	defer resourceServer.Close()

	cipher, _ := mcpauth.NewTokenCipher([]byte("12345678901234567890123456789012"))
	auth := &mcpauth.Service{
		MCPServer:     "demo",
		ResourceURL:   "https://resource.example",
		PublicBaseURL: "http://localhost:8080",
		StateHMACKey:  []byte("1234567890123456"),
		Store:         &testTokenStore{},
		Cipher:        cipher,
		Registration: mcpauth.RegistrationConfig{
			ClientID:                  "client-id",
			ClientSecret:              "client-secret",
			Scopes:                    []string{"read"},
			AuthorizationServerIssuer: issuerServer.URL,
		},
	}

	resumed := make(chan mcpauth.OAuthState, 1)
	s := &Server{
		log: slog.Default(),
		authService: func(context.Context, string) (*mcpauth.Service, error) {
			return auth, nil
		},
		oauthResume: func(_ context.Context, state mcpauth.OAuthState) error {
			resumed <- state
			return nil
		},
	}

	startReq := httptest.NewRequest(http.MethodGet, "/slacker/v1/oauth/demo/start?team_id=T1&user_id=U1&request_id=R1&resource_metadata="+resourceServer.URL, nil)
	startReq = withURLParam(startReq, "mcp_server", "demo")
	startRes := httptest.NewRecorder()
	s.oauthStart(startRes, startReq)
	location := startRes.Header().Get("Location")
	state := strings.Split(strings.Split(location, "state=")[1], "&")[0]

	cbReq := httptest.NewRequest(http.MethodGet, "/slacker/v1/oauth/demo/callback?code=abc&state="+state, nil)
	cbReq = withURLParam(cbReq, "mcp_server", "demo")
	cbRes := httptest.NewRecorder()
	s.oauthCallback(cbRes, cbReq)
	if cbRes.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", cbRes.Code)
	}

	select {
	case got := <-resumed:
		if got.RequestID != "R1" || got.MCPServer != "demo" || got.SlackTeamID != "T1" || got.SlackUserID != "U1" {
			t.Fatalf("unexpected resume state: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected oauth resume handler to be called")
	}
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
}

type testTokenStore struct{}

func (t *testTokenStore) PutDelegatedOAuthToken(context.Context, mcpauth.TokenRecord) error {
	return nil
}

func (t *testTokenStore) GetDelegatedOAuthToken(context.Context, string, string, string) (*mcpauth.TokenRecord, error) {
	return nil, nil
}
