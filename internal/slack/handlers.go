package slackruntime

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/katesclau/slacker/internal/agents"
	"github.com/katesclau/slacker/internal/mcpauth"
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
	starter := fmt.Sprintf("Hey <@%s>! I started a new Slacker thread. Reply in this thread to continue the conversation.", cmd.UserID)
	_, threadTS, err := r.client.PostMessageContext(ctx, cmd.ChannelID, slack.MsgOptionText(starter, false))
	if err != nil {
		r.log.Error("post thread starter", "error", err)
		return
	}
	sessionID := threadSessionID(cmd.TeamID, cmd.ChannelID, threadTS)
	if err := r.repo.UpsertChatThread(ctx, postgres.ChatThread{
		SlackTeamID:    cmd.TeamID,
		SlackChannelID: cmd.ChannelID,
		SlackThreadTS:  threadTS,
		SessionID:      sessionID,
		CreatedBy:      cmd.UserID,
	}); err != nil {
		r.log.Error("persist chat thread", "error", err, "thread_ts", threadTS)
	}
	if original := formatOriginalRequestQuote(prompt); original != "" {
		_, _, err := r.client.PostMessageContext(
			ctx,
			cmd.ChannelID,
			slack.MsgOptionText(original, false),
			slack.MsgOptionTS(threadTS),
		)
		if err != nil {
			r.log.Error("post original request quote", "error", err, "thread_ts", threadTS)
		}
	}

	if err := r.postAgentResponseToThread(ctx, cmd.TeamID, cmd.ChannelID, cmd.UserID, threadTS, prompt, agentName, recent); err != nil {
		r.log.Error("post slash response", "error", err)
	}
}

func (r *Runtime) respondConfig(ctx context.Context, cmd slack.SlashCommand) {
	r.log.Debug("trace /slacker-config command received",
		"user_id", cmd.UserID,
		"team_id", cmd.TeamID,
		"channel_id", cmd.ChannelID,
		"raw_text", strings.TrimSpace(cmd.Text),
	)
	if !isAdmin(r.cfg.AdminUsers, cmd.UserID) {
		r.log.Debug("trace /slacker-config rejected non-admin", "user_id", cmd.UserID)
		_, _, _ = r.client.PostMessageContext(ctx, cmd.ChannelID, slack.MsgOptionText("Only admins can run this command.", false))
		return
	}
	action, usageErr := parseMCPConfigAction(cmd.Text)
	if usageErr != nil {
		r.log.Debug("trace /slacker-config invalid usage", "user_id", cmd.UserID, "error", usageErr.Error())
		_, _, _ = r.client.PostMessageContext(ctx, cmd.ChannelID, slack.MsgOptionText(usageErr.Error(), false))
		return
	}
	r.log.Debug("trace /slacker-config parsed action", "user_id", cmd.UserID, "action", action)
	var err error
	switch action {
	case "add":
		r.log.Debug("trace /slacker-config opening add modal", "user_id", cmd.UserID, "channel_id", cmd.ChannelID)
		err = r.openMCPAddModal(ctx, cmd.TriggerID, cmd.ChannelID)
	case "list":
		r.log.Debug("trace /slacker-config opening list modal", "user_id", cmd.UserID, "channel_id", cmd.ChannelID)
		err = r.openMCPListModal(ctx, cmd.TriggerID, cmd.ChannelID)
	case "remove":
		r.log.Debug("trace /slacker-config opening remove modal", "user_id", cmd.UserID, "channel_id", cmd.ChannelID)
		err = r.openMCPRemoveModal(ctx, cmd.TriggerID, cmd.ChannelID)
	default:
		err = fmt.Errorf("unsupported action: %s", action)
	}
	if err != nil {
		r.log.Error("open config modal failed", "error", err, "action", action)
		_, _, _ = r.client.PostMessageContext(ctx, cmd.ChannelID, slack.MsgOptionText(fmt.Sprintf("Failed to open config modal: %v", err), false))
		return
	}
	r.log.Debug("trace /slacker-config modal opened", "user_id", cmd.UserID, "action", action)
}

func (r *Runtime) ResumeOAuthConversation(ctx context.Context, state mcpauth.OAuthState) error {
	if r == nil || r.repo == nil {
		return nil
	}
	req, err := r.repo.GetMCPOAuthResumeRequest(ctx, state.RequestID)
	if err != nil {
		return err
	}
	if req == nil {
		r.log.Debug("oauth resume request not found",
			"mcp_server", state.MCPServer,
			"team_id", state.SlackTeamID,
			"user_id", state.SlackUserID,
			"request_id", state.RequestID,
		)
		return nil
	}
	if req.MCPServer != state.MCPServer || req.SlackTeamID != state.SlackTeamID || req.SlackUserID != state.SlackUserID {
		r.log.Debug("oauth resume request ownership mismatch",
			"mcp_server", state.MCPServer,
			"stored_mcp_server", req.MCPServer,
			"team_id", state.SlackTeamID,
			"stored_team_id", req.SlackTeamID,
			"user_id", state.SlackUserID,
			"stored_user_id", req.SlackUserID,
			"request_id", state.RequestID,
		)
		return nil
	}

	if strings.TrimSpace(req.Prompt) == "" {
		r.log.Debug("oauth resume request has empty prompt",
			"mcp_server", req.MCPServer,
			"team_id", req.SlackTeamID,
			"user_id", req.SlackUserID,
			"request_id", req.RequestID,
		)
		return nil
	}

	_, _, err = r.client.PostMessageContext(
		ctx,
		req.SlackChannelID,
		slack.MsgOptionText(fmt.Sprintf("MCP access for `%s` is connected. Resuming the original request.", req.MCPServer), false),
		slack.MsgOptionTS(req.SlackThreadTS),
	)
	if err != nil {
		return err
	}
	recent, _ := r.memory.Recent(ctx, req.SlackTeamID, req.SlackChannelID, 5)
	if err := r.postAgentResponseToThread(ctx, req.SlackTeamID, req.SlackChannelID, req.SlackUserID, req.SlackThreadTS, req.Prompt, req.AgentName, recent); err != nil {
		return err
	}
	if err := r.repo.DeleteMCPOAuthResumeRequest(ctx, req.RequestID); err != nil {
		return err
	}
	r.log.Debug("oauth resume completed",
		"mcp_server", req.MCPServer,
		"team_id", req.SlackTeamID,
		"user_id", req.SlackUserID,
		"thread_ts", req.SlackThreadTS,
		"request_id", req.RequestID,
	)
	return nil
}

func isAdmin(admins []string, userID string) bool {
	for _, admin := range admins {
		if strings.TrimSpace(admin) == userID {
			return true
		}
	}
	return false
}

func (r *Runtime) postAgentResponseToThread(
	ctx context.Context,
	teamID string,
	channelID string,
	userID string,
	threadTS string,
	prompt string,
	agentName string,
	recent []string,
) error {
	requestNeedsMCP := shouldTriggerMCPAccessFlow(prompt)
	hasMCPAccess := false
	if requestNeedsMCP {
		access, accessErr := r.repo.UserHasEnabledMCPAccess(ctx, teamID, userID)
		if accessErr != nil {
			r.log.Error("failed checking user MCP access", "error", accessErr, "team_id", teamID, "user_id", userID)
		} else {
			hasMCPAccess = access
		}
	}

	thinkingText := ":hourglass_flowing_sand: Thinking..."
	_, thinkingTS, thinkingErr := r.client.PostMessageContext(
		ctx,
		channelID,
		slack.MsgOptionText(thinkingText, false),
		slack.MsgOptionTS(threadTS),
	)

	sessionID := threadSessionID(teamID, channelID, threadTS)
	resultText := "No response generated."
	startedAt := time.Now()
	if r.agents != nil {
		result, err := r.agents.Run(ctx, agents.RunRequest{
			TeamID:    teamID,
			UserID:    userID,
			SessionID: sessionID,
			Text:      prompt,
			AgentName: agentName,
		})
		if err != nil {
			resultText = fmt.Sprintf("Agent execution failed: %v", err)
			if isMCPAuthError(err) {
				if promptErr := r.postMCPAuthPromptEphemeral(ctx, teamID, channelID, userID, threadTS, agentName, prompt); promptErr != nil {
					r.log.Error("failed to send MCP auth prompt", "error", promptErr, "user_id", userID)
				} else {
					resultText = "MCP access is not connected for this user yet. I sent you a private message in this channel with connect links."
				}
			}
		} else {
			resultText = result.Text
		}
	}

	if requestNeedsMCP && !hasMCPAccess {
		if shouldTriggerMCPAccessFlow(resultText) || strings.Contains(strings.ToLower(resultText), "can't") || strings.Contains(strings.ToLower(resultText), "cannot") {
			if promptErr := r.postMCPAuthPromptEphemeral(ctx, teamID, channelID, userID, threadTS, agentName, prompt); promptErr != nil {
				r.log.Error("failed to send MCP auth prompt after response", "error", promptErr, "user_id", userID)
			} else if !strings.Contains(strings.ToLower(resultText), "private message") {
				resultText = strings.TrimSpace(resultText) + "\n\nI sent you a private message in this channel with MCP connect links."
			}
		}
	}

	_ = recent // retained for future memory-context UI; currently not rendered

	resultText = strings.TrimSpace(resultText)
	if resultText == "" {
		resultText = "No response generated."
	}
	text, blocks := r.blockkit.ResolvePlaceholders(resultText)
	uiBlocks := buildResponseUIBlocks(text, blocks, time.Since(startedAt))

	if thinkingErr == nil && strings.TrimSpace(thinkingTS) != "" {
		updateOpts := []slack.MsgOption{
			slack.MsgOptionText(text, false),
		}
		if len(uiBlocks) > 0 {
			updateOpts = append(updateOpts, slack.MsgOptionBlocks(uiBlocks...))
		}
		_, _, _, err := r.client.UpdateMessageContext(ctx, channelID, thinkingTS, updateOpts...)
		return err
	}

	msgOpts := []slack.MsgOption{
		slack.MsgOptionText(text, false),
		slack.MsgOptionTS(threadTS),
	}
	if len(uiBlocks) > 0 {
		msgOpts = append(msgOpts, slack.MsgOptionBlocks(uiBlocks...))
	}
	_, _, err := r.client.PostMessageContext(ctx, channelID, msgOpts...)
	return err
}

func threadSessionID(teamID, channelID, threadTS string) string {
	return fmt.Sprintf("%s:%s:%s", teamID, channelID, threadTS)
}

func shouldTriggerMCPAccessFlow(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	keywords := []string{
		"mcp",
		"github",
		"repo",
		"repository",
		"repositories",
		"organization",
		"pull request",
		"issue",
	}
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func isMCPAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "mcp") {
		return false
	}
	return strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "401") ||
		strings.Contains(msg, "token") ||
		strings.Contains(msg, "oauth")
}

func (r *Runtime) postMCPAuthPromptEphemeral(ctx context.Context, teamID, channelID, userID, threadTS, agentName, prompt string) error {
	if deleted, err := r.repo.DeleteStaleMCPOAuthResumeRequests(ctx, time.Now().Add(-48*time.Hour)); err != nil {
		r.log.Debug("failed to clean stale oauth resume requests", "error", err)
	} else if deleted > 0 {
		r.log.Debug("cleaned stale oauth resume requests", "deleted", deleted)
	}

	servers, err := r.repo.ListMCPServers(ctx)
	if err != nil {
		return err
	}
	enabled := make([]postgres.MCPServer, 0, len(servers))
	for _, s := range servers {
		if s.Enabled {
			enabled = append(enabled, s)
		}
	}
	if len(enabled) == 0 {
		_, err := r.client.PostEphemeralContext(
			ctx,
			channelID,
			userID,
			slack.MsgOptionText("No enabled MCP servers are configured yet. Ask an admin to add one via `/slacker-config mcp add`.", false),
		)
		return err
	}

	var lines []string
	buttons := make([]slack.BlockElement, 0, minInt(len(enabled), 5))
	for i, server := range enabled {
		requestID := uuid.NewString()
		if err := r.repo.UpsertMCPOAuthResumeRequest(ctx, postgres.MCPOAuthResumeRequest{
			RequestID:      requestID,
			MCPServer:      server.Name,
			SlackTeamID:    teamID,
			SlackUserID:    userID,
			SlackChannelID: channelID,
			SlackThreadTS:  threadTS,
			AgentName:      agentName,
			Prompt:         prompt,
		}); err != nil {
			return err
		}
		link := oauthStartLink(r.cfg.PublicBaseURL, server.Name, teamID, userID, requestID)
		lines = append(lines, fmt.Sprintf("• *%s*: %s", server.Name, link))
		if i < 5 {
			btn := slack.NewButtonBlockElement(
				fmt.Sprintf("oauth-%s", server.Name),
				"",
				slack.NewTextBlockObject(slack.PlainTextType, trimLabel(server.Name, 60), false, false),
			)
			btn.URL = link
			buttons = append(buttons, btn)
		}
	}

	opts := []slack.MsgOption{
		slack.MsgOptionText("Connect your MCP access to continue.", false),
		slack.MsgOptionBlocks(
			slack.NewSectionBlock(
				slack.NewTextBlockObject(
					slack.MarkdownType,
					"*MCP access required*\nConnect at least one MCP server with your user identity.\n\n"+strings.Join(lines, "\n"),
					false,
					false,
				),
				nil,
				nil,
			),
		),
	}
	if len(buttons) > 0 {
		opts = []slack.MsgOption{
			slack.MsgOptionText("Connect your MCP access to continue.", false),
			slack.MsgOptionBlocks(
				slack.NewSectionBlock(
					slack.NewTextBlockObject(
						slack.MarkdownType,
						"*MCP access required*\nConnect at least one MCP server with your user identity.",
						false,
						false,
					),
					nil,
					nil,
				),
				slack.NewActionBlock("mcp_oauth_actions", buttons...),
			),
		}
	}

	_, err = r.client.PostEphemeralContext(ctx, channelID, userID, opts...)
	return err
}

func oauthStartLink(baseURL, serverName, teamID, userID, requestID string) string {
	q := url.Values{}
	q.Set("team_id", teamID)
	q.Set("user_id", userID)
	q.Set("request_id", requestID)
	return fmt.Sprintf("%s/slacker/v1/oauth/%s/start?%s", strings.TrimSuffix(baseURL, "/"), url.PathEscape(serverName), q.Encode())
}

func trimLabel(in string, max int) string {
	in = strings.TrimSpace(in)
	if len(in) <= max {
		return in
	}
	return in[:max]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func parseMCPConfigAction(text string) (string, error) {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) != 2 || parts[0] != "mcp" {
		return "", fmt.Errorf("usage: /slacker-config mcp add|list|remove")
	}
	switch parts[1] {
	case "add", "list", "remove":
		return parts[1], nil
	default:
		return "", fmt.Errorf("usage: /slacker-config mcp add|list|remove")
	}
}

func formatOriginalRequestQuote(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	lines := strings.Split(prompt, "\n")
	for i := range lines {
		lines[i] = "> " + strings.TrimRight(lines[i], " ")
	}
	return "Original request:\n" + strings.Join(lines, "\n")
}

func buildResponseUIBlocks(text string, dynamicBlocks []slack.Block, elapsed time.Duration) []slack.Block {
	blocks := make([]slack.Block, 0, 10+len(dynamicBlocks))
	blocks = append(blocks, slack.NewHeaderBlock(
		slack.NewTextBlockObject(slack.PlainTextType, "Slacker", false, false),
	))

	for _, chunk := range splitTextForSection(text, 2800) {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, chunk, false, false),
			nil,
			nil,
		))
	}

	if len(dynamicBlocks) > 0 {
		blocks = append(blocks, dynamicBlocks...)
	}
	blocks = append(blocks, slack.NewContextBlock(
		"",
		slack.NewTextBlockObject(
			slack.MarkdownType,
			fmt.Sprintf("Processed in %.1fs", elapsed.Seconds()),
			false,
			false,
		),
	))
	return blocks
}

func splitTextForSection(text string, max int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{"No response generated."}
	}

	paragraphs := strings.Split(text, "\n\n")
	out := make([]string, 0, len(paragraphs))
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		for len(p) > max {
			out = append(out, p[:max])
			p = p[max:]
		}
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"No response generated."}
	}
	return out
}
