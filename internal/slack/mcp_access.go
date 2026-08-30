package slackruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/katesclau/slacker/internal/store/postgres"
)

const (
	mcpAccessClassifierModel   = "gpt-5-mini"
	mcpAccessClassifierTimeout = 12 * time.Second
	mcpAccessMaxPromptRunes    = 4000
)

type llmCompleter interface {
	Complete(ctx context.Context, modelName, system, user string) (string, error)
}

func (r *Runtime) shouldTriggerMCPAccessFlow(ctx context.Context, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || r == nil || r.repo == nil {
		return false
	}
	servers, err := r.repo.ListMCPServers(ctx)
	if err != nil {
		if r.log != nil {
			r.log.Error("failed listing MCP servers for access classification", "error", err)
		}
		return false
	}
	oauthServers := oauthEnabledMCPServers(servers)
	if len(oauthServers) == 0 {
		return false
	}
	return r.classifyOAuthMCPAccess(ctx, text, oauthServers)
}

func (r *Runtime) classifyOAuthMCPAccess(ctx context.Context, text string, servers []postgres.MCPServer) bool {
	if strings.TrimSpace(text) == "" || len(servers) == 0 {
		return false
	}
	if r == nil || r.model == nil {
		return textMentionsOAuthMCPServer(text, servers)
	}

	classifyCtx, cancel := context.WithTimeout(ctx, mcpAccessClassifierTimeout)
	defer cancel()

	raw, err := r.model.Complete(classifyCtx, mcpAccessClassifierModel, mcpAccessClassifierSystemPrompt(), mcpAccessClassifierUserPrompt(text, servers))
	if err != nil {
		if r.log != nil {
			r.log.Debug("mcp access classifier failed", "error", err)
		}
		return textMentionsOAuthMCPServer(text, servers)
	}

	needed, matched := parseMCPAccessDecision(raw, servers)
	if r.log != nil {
		r.log.Debug("mcp access classifier result",
			"needed", needed,
			"matched_servers", strings.Join(matched, ","),
			"raw", raw,
		)
	}
	return len(matched) > 0
}

func oauthEnabledMCPServers(servers []postgres.MCPServer) []postgres.MCPServer {
	out := make([]postgres.MCPServer, 0, len(servers))
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		if strings.TrimSpace(server.IssuerURL) == "" {
			continue
		}
		out = append(out, server)
	}
	return out
}

func mcpAccessClassifierSystemPrompt() string {
	return strings.TrimSpace(`
You classify whether a Slack message needs access to any configured OAuth MCP server.

Match when the message asks to read, write, search, list, or operate on that server or the product it represents.
Also match assistant replies that cannot complete the request without that server.

Do not match generic chat that does not need those tools.
Only match servers from the provided list. Infer common aliases from the server name, resource URL, and issuer URL.

Reply with JSON only, no markdown:
{"needed":<bool>,"servers":[<matching configured server names>]}
`)
}

func mcpAccessClassifierUserPrompt(text string, servers []postgres.MCPServer) string {
	var b strings.Builder
	b.WriteString("Configured OAuth MCP servers:\n")
	for _, server := range servers {
		fmt.Fprintf(&b, "- name=%q resource=%q issuer=%q auth_mode=%q\n",
			server.Name, server.ResourceURL, server.IssuerURL, server.AuthMode)
	}
	b.WriteString("\nMessage:\n")
	b.WriteString(truncateRunes(strings.TrimSpace(text), mcpAccessMaxPromptRunes))
	return b.String()
}

func parseMCPAccessDecision(raw string, servers []postgres.MCPServer) (bool, []string) {
	raw = stripJSONFence(raw)
	if raw == "" || len(servers) == 0 {
		return false, nil
	}

	allowed := make(map[string]string, len(servers))
	for _, server := range servers {
		name := strings.TrimSpace(server.Name)
		if name != "" {
			allowed[strings.ToLower(name)] = name
		}
	}

	var dec struct {
		Needed  bool     `json:"needed"`
		Servers []string `json:"servers"`
	}
	if err := json.Unmarshal([]byte(raw), &dec); err != nil {
		return false, nil
	}

	matched := uniqueConfiguredServers(dec.Servers, allowed)
	return dec.Needed, matched
}

func uniqueConfiguredServers(names []string, allowed map[string]string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		canonical, ok := allowed[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			continue
		}
		key := strings.ToLower(canonical)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, canonical)
	}
	return out
}

func textMentionsOAuthMCPServer(text string, servers []postgres.MCPServer) bool {
	lower := strings.ToLower(text)
	for _, server := range servers {
		name := strings.ToLower(strings.TrimSpace(server.Name))
		if name != "" && strings.Contains(lower, name) {
			return true
		}
		if host := resourceHost(server.ResourceURL); host != "" && strings.Contains(lower, host) {
			return true
		}
		if host := resourceHost(server.IssuerURL); host != "" && strings.Contains(lower, host) {
			return true
		}
	}
	return false
}

func resourceHost(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")
	if host == "" || !strings.Contains(host, ".") {
		return ""
	}
	return host
}

func stripJSONFence(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}

func truncateRunes(text string, max int) string {
	if max <= 0 || text == "" {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}
