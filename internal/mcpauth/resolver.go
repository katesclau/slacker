package mcpauth

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type TokenResolver struct {
	Store  TokenStore
	Cipher *TokenCipher
}

func (r TokenResolver) ResolveBearerToken(ctx context.Context, teamID string, userID string, server string) (string, error) {
	rec, err := r.Store.GetDelegatedOAuthToken(ctx, teamID, userID, server)
	if err != nil {
		return "", err
	}
	if rec == nil {
		return "", fmt.Errorf("no delegated oauth token found for server %q", server)
	}
	if !rec.ExpiresAt.IsZero() && rec.ExpiresAt.Before(time.Now()) {
		return "", fmt.Errorf("delegated oauth token expired for server %q", server)
	}
	return r.Cipher.DecryptFromBase64(rec.EncAccess)
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
