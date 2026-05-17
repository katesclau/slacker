package mcpauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func signState(key []byte, st OAuthState) (string, error) {
	raw, err := json.Marshal(st)
	if err != nil {
		return "", fmt.Errorf("marshal state: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	signature := mac.Sum(nil)

	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parseAndVerifyState(key []byte, signed string) (OAuthState, error) {
	var out OAuthState
	parts := strings.SplitN(signed, ".", 2)
	if len(parts) != 2 {
		return out, fmt.Errorf("invalid state encoding")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return out, fmt.Errorf("decode state payload: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return out, fmt.Errorf("decode state signature: %w", err)
	}

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return out, fmt.Errorf("invalid oauth state signature")
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("unmarshal state payload: %w", err)
	}
	return out, nil
}
