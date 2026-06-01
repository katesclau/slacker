package slackruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/katesclau/slacker/internal/store/postgres"
	"github.com/slack-go/slack"
)

const (
	mcpAddModalCallbackID    = "slacker_config_mcp_add"
	mcpListModalCallbackID   = "slacker_config_mcp_list"
	mcpRemoveModalCallbackID = "slacker_config_mcp_remove"

	fieldName      = "mcp_name"
	fieldResource  = "mcp_resource_url"
	fieldIssuer    = "mcp_issuer_url"
	fieldClientID  = "mcp_client_id"
	fieldSecret    = "mcp_client_secret"
	fieldScopesCSV = "mcp_scopes_csv"
	fieldEnabled   = "mcp_enabled_servers"
	fieldRemove    = "mcp_remove_server"
)

type configPrivateMetadata struct {
	ChannelID string `json:"channel_id"`
}

func (r *Runtime) openMCPAddModal(ctx context.Context, triggerID string, channelID string) error {
	r.log.Debug("trace /slacker-config openMCPAddModal start", "channel_id", channelID)
	servers, err := r.repo.ListMCPServers(ctx)
	if err != nil {
		r.log.Debug("trace /slacker-config openMCPAddModal list servers failed", "error", err)
		return err
	}
	r.log.Debug("trace /slacker-config openMCPAddModal list servers", "count", len(servers))

	blocks := []slack.Block{
		slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, "MCP Server Management", false, false)),
		slack.NewContextBlock("",
			slack.NewTextBlockObject(slack.MarkdownType, "Add or update an MCP server used by agents. Existing servers are listed below.", false, false),
		),
	}

	if len(servers) == 0 {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "_No MCP servers configured yet._", false, false),
			nil,
			nil,
		))
	} else {
		summary := make([]string, 0, len(servers))
		for _, s := range servers {
			summary = append(summary, fmt.Sprintf("• *%s* - `%s`", s.Name, s.ResourceURL))
		}
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, strings.Join(summary, "\n"), false, false),
			nil,
			nil,
		))
	}

	privateMetadata, _ := json.Marshal(configPrivateMetadata{ChannelID: channelID})
	modal := slack.ModalViewRequest{
		Type:            slack.ViewType("modal"),
		CallbackID:      mcpAddModalCallbackID,
		PrivateMetadata: string(privateMetadata),
		Title:           slack.NewTextBlockObject(slack.PlainTextType, "MCP Add", false, false),
		Submit:          slack.NewTextBlockObject(slack.PlainTextType, "Save", false, false),
		Close:           slack.NewTextBlockObject(slack.PlainTextType, "Cancel", false, false),
		Blocks: slack.Blocks{
			BlockSet: append(blocks,
				inputBlock(fieldName, "MCP Server Name", "github"),
				inputBlock(fieldResource, "Resource URL", "https://api.githubcopilot.com/mcp/"),
				inputBlock(fieldIssuer, "OAuth Issuer URL", "https://github.com/login/oauth"),
				inputBlock(fieldClientID, "Client ID", "github_oauth_client_id"),
				inputBlock(fieldSecret, "Client Secret", "github_oauth_client_secret"),
				inputBlock(fieldScopesCSV, "Scopes CSV", "repo,read:org,read:user,user:email"),
			),
		},
	}

	_, err = r.client.OpenViewContext(ctx, triggerID, modal)
	if err != nil {
		r.log.Debug("trace /slacker-config openMCPAddModal open failed", "error", err)
		return err
	}
	r.log.Debug("trace /slacker-config openMCPAddModal opened")
	return err
}

func (r *Runtime) openMCPListModal(ctx context.Context, triggerID string, channelID string) error {
	r.log.Debug("trace /slacker-config openMCPListModal start", "channel_id", channelID)
	servers, err := r.repo.ListMCPServers(ctx)
	if err != nil {
		r.log.Debug("trace /slacker-config openMCPListModal list servers failed", "error", err)
		return err
	}
	r.log.Debug("trace /slacker-config openMCPListModal list servers", "count", len(servers))
	if len(servers) == 0 {
		r.log.Debug("trace /slacker-config openMCPListModal no servers configured")
		_, _, postErr := r.client.PostMessageContext(ctx, channelID, slack.MsgOptionText("No MCP servers configured to list.", false))
		return postErr
	}
	options := make([]*slack.OptionBlockObject, 0, len(servers))
	initial := make([]*slack.OptionBlockObject, 0, len(servers))
	for _, s := range servers {
		opt := slack.NewOptionBlockObject(
			s.Name,
			slack.NewTextBlockObject(slack.PlainTextType, fmt.Sprintf("%s (%s)", s.Name, s.ResourceURL), false, false),
			nil,
		)
		options = append(options, opt)
		if s.Enabled {
			initial = append(initial, opt)
		}
	}
	checkbox := slack.NewCheckboxGroupsBlockElement(fieldEnabled, options...)
	checkbox.InitialOptions = initial

	privateMetadata, _ := json.Marshal(configPrivateMetadata{ChannelID: channelID})
	modal := slack.ModalViewRequest{
		Type:            slack.ViewType("modal"),
		CallbackID:      mcpListModalCallbackID,
		PrivateMetadata: string(privateMetadata),
		Title:           slack.NewTextBlockObject(slack.PlainTextType, "MCP List", false, false),
		Submit:          slack.NewTextBlockObject(slack.PlainTextType, "Save", false, false),
		Close:           slack.NewTextBlockObject(slack.PlainTextType, "Cancel", false, false),
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, "Enable MCP Servers", false, false)),
			slack.NewInputBlock(
				fieldEnabled,
				slack.NewTextBlockObject(slack.PlainTextType, "Enabled servers", false, false),
				nil,
				checkbox,
			),
		}},
	}
	_, err = r.client.OpenViewContext(ctx, triggerID, modal)
	if err != nil {
		r.log.Debug("trace /slacker-config openMCPListModal open failed", "error", err)
		return err
	}
	r.log.Debug("trace /slacker-config openMCPListModal opened")
	return err
}

func (r *Runtime) openMCPRemoveModal(ctx context.Context, triggerID string, channelID string) error {
	r.log.Debug("trace /slacker-config openMCPRemoveModal start", "channel_id", channelID)
	servers, err := r.repo.ListMCPServers(ctx)
	if err != nil {
		r.log.Debug("trace /slacker-config openMCPRemoveModal list servers failed", "error", err)
		return err
	}
	r.log.Debug("trace /slacker-config openMCPRemoveModal list servers", "count", len(servers))
	if len(servers) == 0 {
		r.log.Debug("trace /slacker-config openMCPRemoveModal no servers configured")
		_, _, postErr := r.client.PostMessageContext(ctx, channelID, slack.MsgOptionText("No MCP servers configured.", false))
		return postErr
	}
	options := make([]*slack.OptionBlockObject, 0, len(servers))
	for _, s := range servers {
		options = append(options, slack.NewOptionBlockObject(
			s.Name,
			slack.NewTextBlockObject(slack.PlainTextType, s.Name, false, false),
			slack.NewTextBlockObject(slack.PlainTextType, s.ResourceURL, false, false),
		))
	}
	privateMetadata, _ := json.Marshal(configPrivateMetadata{ChannelID: channelID})
	modal := slack.ModalViewRequest{
		Type:            slack.ViewType("modal"),
		CallbackID:      mcpRemoveModalCallbackID,
		PrivateMetadata: string(privateMetadata),
		Title:           slack.NewTextBlockObject(slack.PlainTextType, "MCP Remove", false, false),
		Submit:          slack.NewTextBlockObject(slack.PlainTextType, "Remove", false, false),
		Close:           slack.NewTextBlockObject(slack.PlainTextType, "Cancel", false, false),
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			slack.NewInputBlock(
				fieldRemove,
				slack.NewTextBlockObject(slack.PlainTextType, "Server to remove", false, false),
				nil,
				slack.NewOptionsSelectBlockElement(
					slack.OptTypeStatic,
					slack.NewTextBlockObject(slack.PlainTextType, "Select server", false, false),
					fieldRemove,
					options...,
				),
			),
		}},
	}
	_, err = r.client.OpenViewContext(ctx, triggerID, modal)
	if err != nil {
		r.log.Debug("trace /slacker-config openMCPRemoveModal open failed", "error", err)
		return err
	}
	r.log.Debug("trace /slacker-config openMCPRemoveModal opened")
	return err
}

func (r *Runtime) handleMCPAddModalSubmission(ctx context.Context, cb slack.InteractionCallback) error {
	r.log.Debug("trace /slacker-config handleMCPAddModalSubmission start", "user_id", cb.User.ID)
	values := cb.View.State.Values
	name := strings.TrimSpace(getViewInput(values, fieldName))
	resourceURL := strings.TrimSpace(getViewInput(values, fieldResource))
	issuerURL := strings.TrimSpace(getViewInput(values, fieldIssuer))
	clientID := strings.TrimSpace(getViewInput(values, fieldClientID))
	clientSecret := strings.TrimSpace(getViewInput(values, fieldSecret))
	scopesCSV := strings.TrimSpace(getViewInput(values, fieldScopesCSV))
	r.log.Debug("trace /slacker-config add submission parsed values",
		"user_id", cb.User.ID,
		"name", name,
		"resource_url", resourceURL,
		"issuer_url", issuerURL,
		"client_id", clientID,
		"has_client_secret", clientSecret != "",
		"scopes_csv", scopesCSV,
	)

	if name == "" || resourceURL == "" || issuerURL == "" || clientID == "" || clientSecret == "" {
		return fmt.Errorf("name, resource URL, issuer URL, client ID, and client secret are required")
	}

	err := r.repo.UpsertMCPServer(ctx, postgres.MCPServer{
		Name:            name,
		ResourceURL:     resourceURL,
		IssuerURL:       issuerURL,
		ClientID:        clientID,
		ClientSecretEnc: clientSecret,
		Enabled:         true,
		ScopesCSV:       scopesCSV,
	})
	if err != nil {
		r.log.Debug("trace /slacker-config add submission upsert failed", "error", err, "name", name)
		return err
	}
	r.log.Debug("trace /slacker-config add submission upserted", "name", name)

	return r.postConfigMessage(ctx, cb.View.PrivateMetadata, fmt.Sprintf("MCP server `%s` saved.", name))
}

func (r *Runtime) handleMCPListModalSubmission(ctx context.Context, cb slack.InteractionCallback) error {
	r.log.Debug("trace /slacker-config handleMCPListModalSubmission start", "user_id", cb.User.ID)
	byAction, ok := cb.View.State.Values[fieldEnabled]
	if !ok {
		return fmt.Errorf("enabled servers selection missing")
	}
	action, ok := byAction[fieldEnabled]
	if !ok {
		return fmt.Errorf("enabled servers action missing")
	}
	selected := make([]string, 0, len(action.SelectedOptions))
	for _, opt := range action.SelectedOptions {
		selected = append(selected, opt.Value)
	}
	r.log.Debug("trace /slacker-config list submission selected servers", "user_id", cb.User.ID, "selected", strings.Join(selected, ","))
	if err := r.repo.SetEnabledMCPServers(ctx, selected); err != nil {
		r.log.Debug("trace /slacker-config list submission set enabled failed", "error", err)
		return err
	}
	r.log.Debug("trace /slacker-config list submission updated enabled set", "count", len(selected))
	if len(selected) == 0 {
		return r.postConfigMessage(ctx, cb.View.PrivateMetadata, "Updated enabled MCP servers: none enabled.")
	}
	return r.postConfigMessage(ctx, cb.View.PrivateMetadata, fmt.Sprintf("Updated enabled MCP servers: %s", strings.Join(selected, ", ")))
}

func (r *Runtime) handleMCPRemoveModalSubmission(ctx context.Context, cb slack.InteractionCallback) error {
	r.log.Debug("trace /slacker-config handleMCPRemoveModalSubmission start", "user_id", cb.User.ID)
	values := cb.View.State.Values
	name := strings.TrimSpace(getViewSelect(values, fieldRemove))
	if name == "" {
		return fmt.Errorf("no MCP server selected")
	}
	r.log.Debug("trace /slacker-config remove submission parsed", "user_id", cb.User.ID, "name", name)
	if err := r.repo.DeleteMCPServer(ctx, name); err != nil {
		r.log.Debug("trace /slacker-config remove submission delete failed", "error", err, "name", name)
		return err
	}
	r.log.Debug("trace /slacker-config remove submission deleted", "name", name)
	return r.postConfigMessage(ctx, cb.View.PrivateMetadata, fmt.Sprintf("MCP server `%s` removed.", name))
}

func (r *Runtime) postConfigMessage(ctx context.Context, privateMetadata string, message string) error {
	md := configPrivateMetadata{}
	_ = json.Unmarshal([]byte(privateMetadata), &md)
	if strings.TrimSpace(md.ChannelID) == "" {
		r.log.Warn("missing channel_id in config modal private metadata; skipping feedback message")
		return nil
	}
	r.log.Debug("trace /slacker-config posting feedback message", "channel_id", md.ChannelID)
	_, _, err := r.client.PostMessageContext(ctx, md.ChannelID, slack.MsgOptionText(message, false))
	return err
}

func inputBlock(actionID string, label string, placeholder string) *slack.InputBlock {
	return slack.NewInputBlock(
		actionID,
		slack.NewTextBlockObject(slack.PlainTextType, label, false, false),
		nil,
		slack.NewPlainTextInputBlockElement(slack.NewTextBlockObject(slack.PlainTextType, placeholder, false, false), actionID),
	)
}

func getViewInput(values map[string]map[string]slack.BlockAction, blockID string) string {
	byAction, ok := values[blockID]
	if !ok {
		return ""
	}
	action, ok := byAction[blockID]
	if !ok {
		return ""
	}
	return action.Value
}

func getViewSelect(values map[string]map[string]slack.BlockAction, blockID string) string {
	byAction, ok := values[blockID]
	if !ok {
		return ""
	}
	action, ok := byAction[blockID]
	if !ok {
		return ""
	}
	if action.SelectedOption.Value != "" {
		return action.SelectedOption.Value
	}
	return action.Value
}
