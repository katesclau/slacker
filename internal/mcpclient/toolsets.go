package mcpclient

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/katesclau/slacker/internal/store/postgres"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
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
		slog.Debug("mcp toolset build started", "mcp_server", serverName, "resource_url", srv.ResourceURL, "enabled", srv.Enabled)
		httpClient := &http.Client{
			Timeout: 30 * time.Second,
			Transport: bearerTransport{
				Base:     http.DefaultTransport,
				Server:   serverName,
				Endpoint: srv.ResourceURL,
				GetBearer: func(ctx context.Context) (string, error) {
					identity := identityFromContext(ctx)
					if b.Resolver == nil {
						slog.Debug("mcp bearer resolver missing", "mcp_server", serverName)
						return "", nil
					}
					if identity.TeamID == "" || identity.UserID == "" {
						slog.Debug("mcp bearer identity missing", "mcp_server", serverName, "team_id_present", identity.TeamID != "", "user_id_present", identity.UserID != "")
						return "", nil
					}
					token, err := b.Resolver.ResolveBearerToken(ctx, identity.TeamID, identity.UserID, serverName)
					if err != nil {
						slog.Debug("mcp bearer token unavailable", "mcp_server", serverName, "team_id", identity.TeamID, "user_id", identity.UserID, "error", err)
						return "", nil
					}
					slog.Debug("mcp bearer token resolved", "mcp_server", serverName, "team_id", identity.TeamID, "user_id", identity.UserID)
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
			slog.Debug("mcp toolset build failed", "mcp_server", serverName, "resource_url", srv.ResourceURL, "error", err)
			return nil, err
		}
		slog.Debug("mcp toolset build succeeded", "mcp_server", serverName, "resource_url", srv.ResourceURL)
		out = append(out, ts)
	}
	slog.Debug("mcp toolsets built", "count", len(out))
	return out, nil
}

type bearerTransport struct {
	Base      http.RoundTripper
	Server    string
	Endpoint  string
	GetBearer func(ctx context.Context) (string, error)
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	clone := req.Clone(req.Context())
	hasAuth := false
	if t.GetBearer != nil {
		token, err := t.GetBearer(req.Context())
		if err != nil {
			slog.Debug("mcp outbound bearer lookup failed", "mcp_server", t.Server, "method", req.Method, "host", req.URL.Host, "path", req.URL.Path, "error", err)
			return nil, err
		}
		if token != "" {
			clone.Header = clone.Header.Clone()
			clone.Header.Set("Authorization", "Bearer "+token)
			hasAuth = true
		}
	}
	slog.Debug("mcp outbound request", "mcp_server", t.Server, "configured_endpoint", t.Endpoint, "method", req.Method, "scheme", req.URL.Scheme, "host", req.URL.Host, "path", req.URL.Path, "has_authorization", hasAuth)
	resp, err := base.RoundTrip(clone)
	if err != nil {
		slog.Debug("mcp outbound request failed", "mcp_server", t.Server, "method", req.Method, "host", req.URL.Host, "path", req.URL.Path, "has_authorization", hasAuth, "error", err)
		return nil, err
	}
	slog.Debug("mcp outbound response", "mcp_server", t.Server, "method", req.Method, "host", req.URL.Host, "path", req.URL.Path, "has_authorization", hasAuth, "status", resp.StatusCode)
	return resp, nil
}
