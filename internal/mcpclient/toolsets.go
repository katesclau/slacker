package mcpclient

import (
	"context"
	"net/http"
	"time"

	"github.com/katesclau/slacker/internal/store/postgres"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

type Builder struct {
	Servers  []postgres.MCPServer
	Resolver TokenResolver
}

type TokenResolver interface {
	ResolveBearerToken(ctx context.Context, teamID string, userID string, server string) (string, error)
}

func (b Builder) Build() ([]tool.Toolset, error) {
	out := make([]tool.Toolset, 0, len(b.Servers))
	for _, srv := range b.Servers {
		serverName := srv.Name
		httpClient := &http.Client{
			Timeout: 30 * time.Second,
			Transport: bearerTransport{
				Base: http.DefaultTransport,
				GetBearer: func(ctx context.Context) (string, error) {
					identity := identityFromContext(ctx)
					if b.Resolver == nil {
						return "", nil
					}
					if identity.TeamID == "" || identity.UserID == "" {
						return "", nil
					}
					token, err := b.Resolver.ResolveBearerToken(ctx, identity.TeamID, identity.UserID, serverName)
					if err != nil {
						return "", nil
					}
					return token, nil
				},
			},
		}
		ts, err := mcptoolset.New(mcptoolset.Config{
			Transport: &mcp.StreamableClientTransport{
				Endpoint:   srv.ResourceURL,
				HTTPClient: httpClient,
			},
		})
		if err != nil {
			return nil, err
		}
		out = append(out, ts)
	}
	return out, nil
}

type bearerTransport struct {
	Base      http.RoundTripper
	GetBearer func(ctx context.Context) (string, error)
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	clone := req.Clone(req.Context())
	if t.GetBearer != nil {
		token, err := t.GetBearer(req.Context())
		if err != nil {
			return nil, err
		}
		if token != "" {
			clone.Header = clone.Header.Clone()
			clone.Header.Set("Authorization", "Bearer "+token)
		}
	}
	return base.RoundTrip(clone)
}
