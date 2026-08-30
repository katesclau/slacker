package agents

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/katesclau/slacker/internal/mcpclient"
	"github.com/katesclau/slacker/internal/openaiadapter"
	"github.com/katesclau/slacker/internal/store/postgres"
	"github.com/katesclau/slacker/internal/tooling/blockkit"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

const (
	appUserID = "slacker-app-user"

	defaultInstruction = `You are Slacker, a Slack assistant that can call configured MCP tools.

How users work with you:
- Chat: they start a conversation with /slacker <prompt> or by mentioning the bot in a channel (for example @slacker-dev list open pull requests). Optional @agent_name selects a specific agent, for example /slacker @default_agent list open pull requests. A new thread is created on the mention or slash command; replies in that thread continue the same conversation.
- Config: only Slack admins can manage MCP servers with /slacker-config. Usage is /slacker-config mcp add|list|remove. add opens a modal to create or update a server (name, resource URL, OAuth issuer, static or DCR credentials, scopes). list enables or disables servers. remove deletes a server. If a needed MCP server is missing, tell them to ask an admin to run /slacker-config mcp add.
- OAuth: MCP tools are authorized per Slack user. If a request needs an OAuth-enabled MCP server and the user is not connected, Slacker sends them a private in-channel message with Connect buttons. After they finish the provider login, Slacker resumes the original prompt in the same thread. If a tool call fails with unauthorized, 401, missing token, or oauth errors, say they need to connect that MCP server and wait for the connect prompt rather than inventing results.

Be concise. Use MCP tools when the user asks for data those servers provide. Do not claim access you do not have.`
)

type Runtime struct {
	appName      string
	sessionSvc   session.Service
	model        *openaiadapter.ModelAdapter
	repo         *postgres.Repository
	blockToolset tool.Toolset
	resolver     mcpclient.TokenResolver
}

type RunRequest struct {
	TeamID    string
	UserID    string
	SessionID string
	Text      string
	AgentName string
}

type RunResult struct {
	AgentName string
	Text      string
}

type AgentConfig struct {
	Instruction string   `json:"instruction"`
	Model       string   `json:"model"`
	MCPServers  []string `json:"mcp_servers"`
}

func NewRuntime(appName string, model *openaiadapter.ModelAdapter, repo *postgres.Repository, blockRegistry *blockkit.Registry, tokenResolver mcpclient.TokenResolver) (*Runtime, error) {
	blockToolset, err := blockkit.NewADKToolset(blockRegistry)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		appName:      appName,
		sessionSvc:   session.InMemoryService(),
		model:        model,
		repo:         repo,
		blockToolset: blockToolset,
		resolver:     tokenResolver,
	}, nil
}

func (r *Runtime) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	def, err := r.resolveAgentDefinition(ctx, req.AgentName)
	if err != nil {
		return RunResult{}, err
	}
	mcpServers, err := r.repo.ListMCPServers(ctx)
	if err != nil {
		return RunResult{}, err
	}
	filteredServers := filterMCPServers(mcpServers, def.MCPServers)
	slog.Debug("agent mcp server selection",
		"agent_name", def.Name,
		"configured_count", len(mcpServers),
		"enabled_count", countEnabledMCPServers(mcpServers),
		"allowed_mcp_servers", strings.Join(def.MCPServers, ","),
		"selected_mcp_servers", strings.Join(mcpServerNames(filteredServers), ","),
	)
	mcpToolsets, err := mcpclient.Builder{
		Servers:  filteredServers,
		Resolver: r.resolver,
	}.Build()
	if err != nil {
		return RunResult{}, err
	}
	agentToolsets := []tool.Toolset{r.blockToolset}
	agentToolsets = append(agentToolsets, mcpToolsets...)
	slog.Debug("agent toolsets prepared", "agent_name", def.Name, "local_toolsets", 1, "mcp_toolsets", len(mcpToolsets), "total_toolsets", len(agentToolsets))

	instruction := def.Instruction
	if strings.TrimSpace(instruction) == "" {
		instruction = defaultInstruction
	}

	rootAgent, err := llmagent.New(llmagent.Config{
		Name:        def.Name,
		Description: def.Description,
		Model:       r.model,
		Instruction: instruction,
		Toolsets:    agentToolsets,
	})
	if err != nil {
		return RunResult{}, err
	}

	runnerInstance, err := runner.New(runner.Config{
		AppName:           r.appName,
		Agent:             rootAgent,
		SessionService:    r.sessionSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		return RunResult{}, err
	}

	modelCtx := openaiadapter.WithModel(mcpclient.WithSlackIdentity(ctx, req.TeamID, req.UserID), def.Model)
	slog.Debug("agent run started", "agent_name", def.Name, "team_id", req.TeamID, "user_id", req.UserID, "session_id", req.SessionID, "model_override", def.Model)
	msg := &genai.Content{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: req.Text}},
	}

	var out strings.Builder
	for ev, runErr := range runnerInstance.Run(modelCtx, appUserID, req.SessionID, msg, agent.RunConfig{}) {
		if runErr != nil {
			slog.Debug("agent run failed", "agent_name", def.Name, "team_id", req.TeamID, "user_id", req.UserID, "session_id", req.SessionID, "error", runErr)
			return RunResult{}, runErr
		}
		if ev == nil || ev.Author == "user" || ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			if part.Text != "" {
				if out.Len() > 0 {
					out.WriteString("\n")
				}
				out.WriteString(part.Text)
			}
		}
	}

	if out.Len() == 0 {
		out.WriteString("No response generated.")
	}
	slog.Debug("agent run completed", "agent_name", def.Name, "team_id", req.TeamID, "user_id", req.UserID, "session_id", req.SessionID, "response_len", out.Len())
	return RunResult{
		AgentName: def.Name,
		Text:      out.String(),
	}, nil
}

func countEnabledMCPServers(servers []postgres.MCPServer) int {
	count := 0
	for _, server := range servers {
		if server.Enabled {
			count++
		}
	}
	return count
}

func mcpServerNames(servers []postgres.MCPServer) []string {
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}
	return names
}

func (r *Runtime) HasSession(ctx context.Context, sessionID string) bool {
	if strings.TrimSpace(sessionID) == "" {
		return false
	}
	_, err := r.sessionSvc.Get(ctx, &session.GetRequest{
		AppName:   r.appName,
		UserID:    appUserID,
		SessionID: sessionID,
	})
	return err == nil
}

func filterMCPServers(all []postgres.MCPServer, allowed []string) []postgres.MCPServer {
	enabledOnly := make([]postgres.MCPServer, 0, len(all))
	for _, srv := range all {
		if srv.Enabled {
			enabledOnly = append(enabledOnly, srv)
		}
	}
	if len(allowed) == 0 {
		return enabledOnly
	}
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		set[name] = struct{}{}
	}
	filtered := make([]postgres.MCPServer, 0, len(allowed))
	for _, srv := range enabledOnly {
		if _, ok := set[srv.Name]; ok {
			filtered = append(filtered, srv)
		}
	}
	return filtered
}

type agentDefinition struct {
	Name        string
	Description string
	Instruction string
	Model       string
	MCPServers  []string
}

func (r *Runtime) resolveAgentDefinition(ctx context.Context, requested string) (agentDefinition, error) {
	if strings.TrimSpace(requested) != "" {
		a, err := r.repo.GetAgentByName(ctx, requested)
		if err != nil {
			return agentDefinition{}, err
		}
		if a != nil {
			return toDefinition(*a), nil
		}
	}
	all, err := r.repo.ListAgents(ctx)
	if err != nil {
		return agentDefinition{}, err
	}
	if len(all) == 0 {
		return agentDefinition{
			Name:        "default_agent",
			Description: "Default user-defined agent fallback",
			Instruction: defaultInstruction,
			Model:       "",
		}, nil
	}
	return toDefinition(all[0]), nil
}

func toDefinition(a postgres.Agent) agentDefinition {
	out := agentDefinition{
		Name:        sanitizeAgentName(a.Name),
		Description: a.Description,
	}
	var cfg AgentConfig
	if err := json.Unmarshal(a.ConfigJSON, &cfg); err == nil {
		out.Instruction = cfg.Instruction
		out.Model = cfg.Model
		out.MCPServers = cfg.MCPServers
	}
	return out
}

func sanitizeAgentName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return "default_agent"
	}
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return name
}

func ParseAgentDirective(text string) (agentName string, prompt string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "@") {
		return "", text
	}
	parts := strings.SplitN(text, " ", 2)
	agentName = strings.TrimPrefix(parts[0], "@")
	if len(parts) == 1 {
		return agentName, ""
	}
	return agentName, strings.TrimSpace(parts[1])
}
