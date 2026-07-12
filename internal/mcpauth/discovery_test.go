package mcpauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthorizationServerMetadataCandidates(t *testing.T) {
	got, err := authorizationServerMetadataCandidates("https://auth.example.com/tenant1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"https://auth.example.com/.well-known/oauth-authorization-server/tenant1",
		"https://auth.example.com/.well-known/openid-configuration/tenant1",
		"https://auth.example.com/tenant1/.well-known/openid-configuration",
	}
	assertSlicesEqual(t, want, got)

	got, err = authorizationServerMetadataCandidates("https://auth.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want = []string{
		"https://auth.example.com/.well-known/oauth-authorization-server",
		"https://auth.example.com/.well-known/openid-configuration",
	}
	assertSlicesEqual(t, want, got)
}

func TestDiscoverAuthorizationServerMetadataPrefersRegistrationEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/auth","token_endpoint":"https://auth.example.com/token"}`))
	})
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/auth","token_endpoint":"https://auth.example.com/token","registration_endpoint":"https://auth.example.com/oidc/register"}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := &Service{HTTPClient: ts.Client()}
	md, err := svc.discoverAuthorizationServerMetadata(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if md.RegistrationEndpoint != "https://auth.example.com/oidc/register" {
		t.Fatalf("expected registration endpoint from OIDC metadata, got %q", md.RegistrationEndpoint)
	}
}

func assertSlicesEqual(t *testing.T, want []string, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("slice length mismatch: want=%d got=%d", len(want), len(got))
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("slice mismatch at %d: want=%q got=%q", i, want[i], got[i])
		}
	}
}
