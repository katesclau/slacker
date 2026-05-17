package slackruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/katesclau/slacker/internal/agents"
	"github.com/katesclau/slacker/internal/memory"
	"github.com/katesclau/slacker/internal/store/postgres"
	"github.com/slack-go/slack"
)

func (r *Runtime) respondChat(ctx context.Context, cmd slack.SlashCommand) {
	_ = r.repo.UpsertUser(ctx, postgres.User{
		SlackUserID: cmd.UserID,
		SlackTeamID: cmd.TeamID,
		DisplayName: cmd.UserName,
	})
	_ = r.repo.UpsertChannel(ctx, postgres.Channel{
		SlackChannelID: cmd.ChannelID,
		SlackTeamID:    cmd.TeamID,
		Name:           cmd.ChannelName,
	})
	_, _ = r.memory.Save(ctx, memory.Entry{
		SlackTeamID:    cmd.TeamID,
		SlackChannelID: cmd.ChannelID,
		UserID:         cmd.UserID,
		Content:        strings.TrimSpace(cmd.Text),
	})

	recent, _ := r.memory.Recent(ctx, cmd.TeamID, cmd.ChannelID, 5)
	agentName, prompt := agents.ParseAgentDirective(strings.TrimSpace(cmd.Text))
	sessionID := cmd.ChannelID
	resultText := "No response generated."
	if r.agents != nil {
		result, err := r.agents.Run(ctx, agents.RunRequest{
			TeamID:    cmd.TeamID,
			UserID:    cmd.UserID,
			SessionID: sessionID,
			Text:      prompt,
			AgentName: agentName,
		})
		if err != nil {
			resultText = fmt.Sprintf("Agent execution failed: %v", err)
		} else {
			resultText = result.Text
		}
	}

	header := r.blockkit.Header("Slacker")
	notice := r.blockkit.Notice("info", "ADK agent runtime is active (user-defined agents + MCP go-sdk tool execution).")
	kv := r.blockkit.KV(map[string]string{
		"RecentMemoryCount": fmt.Sprintf("%d", len(recent)),
		"Team":              cmd.TeamID,
		"Channel":           cmd.ChannelID,
	})
	msg := fmt.Sprintf("%s %s %s\n%s", header, notice, kv, resultText)

	text, blocks := r.blockkit.ResolvePlaceholders(msg)
	_, _, err := r.client.PostMessageContext(ctx, cmd.ChannelID, slack.MsgOptionText(text, false), slack.MsgOptionBlocks(blocks...))
	if err != nil {
		r.log.Error("post slash response", "error", err)
	}
}

func (r *Runtime) respondConfig(ctx context.Context, cmd slack.SlashCommand) {
	if !isAdmin(r.cfg.AdminUsers, cmd.UserID) {
		_, _, _ = r.client.PostMessageContext(ctx, cmd.ChannelID, slack.MsgOptionText("Only admins can run this command.", false))
		return
	}
	_, _, _ = r.client.PostMessageContext(ctx, cmd.ChannelID, slack.MsgOptionText("Configuration surface is available. OAuth start links are exposed via HTTP.", false))
}

func isAdmin(admins []string, userID string) bool {
	for _, admin := range admins {
		if strings.TrimSpace(admin) == userID {
			return true
		}
	}
	return false
}
