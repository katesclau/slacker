package mcpauth

import "testing"

func TestSignAndVerifyState(t *testing.T) {
	key := []byte("abcdefghijklmnop")
	state := OAuthState{
		MCPServer:   "github",
		RequestID:   "req-1",
		SlackTeamID: "T123",
		SlackUserID: "U123",
		Resource:    "https://api.github.com",
		ASIssuer:    "https://github.com",
		Nonce:       "nonce-1",
	}

	signed, err := signState(key, state)
	if err != nil {
		t.Fatalf("sign state: %v", err)
	}

	verified, err := parseAndVerifyState(key, signed)
	if err != nil {
		t.Fatalf("verify state: %v", err)
	}
	if verified.SlackUserID != "U123" {
		t.Fatalf("unexpected slack user: %s", verified.SlackUserID)
	}
}
