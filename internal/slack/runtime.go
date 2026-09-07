package slackruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/katesclau/slacker/internal/agents"
	"github.com/katesclau/slacker/internal/memory"
	"github.com/katesclau/slacker/internal/openaiadapter"
	"github.com/katesclau/slacker/internal/store/postgres"
	"github.com/katesclau/slacker/internal/tooling/blockkit"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

type Config struct {
	AppToken      string
	BotToken      string
	ChatCommand   string
	ConfigCommand string
	BotUserTag    string
	AdminUsers    []string
	PublicBaseURL string
}

type Runtime struct {
	cfg       Config
	log       *slog.Logger
	client    *slack.Client
	socket    *socketmode.Client
	repo      *postgres.Repository
	memory    *memory.Service
	blockkit  *blockkit.Tools
	agents    *agents.Runtime
	model     llmCompleter
	botUserID string

	processedMu       sync.Mutex
	processedMessages map[string]time.Time
}

func New(cfg Config, log *slog.Logger, repo *postgres.Repository, memorySvc *memory.Service, blockTools *blockkit.Tools, agentRuntime *agents.Runtime, model *openaiadapter.ModelAdapter) *Runtime {
	client := slack.New(cfg.BotToken, slack.OptionAppLevelToken(cfg.AppToken))
	socket := socketmode.New(client)
	return &Runtime{
		cfg:               cfg,
		log:               log,
		client:            client,
		socket:            socket,
		repo:              repo,
		memory:            memorySvc,
		blockkit:          blockTools,
		agents:            agentRuntime,
		model:             model,
		processedMessages: map[string]time.Time{},
	}
}

func (r *Runtime) consumeEvents(ctx context.Context) {
	for evt := range r.socket.Events {
		switch evt.Type {
		case socketmode.EventTypeSlashCommand:
			cmd, ok := evt.Data.(slack.SlashCommand)
			if !ok {
				r.socket.Ack(*evt.Request)
				continue
			}
			r.socket.Ack(*evt.Request)
			r.handleSlashCommand(ctx, cmd)
		case socketmode.EventTypeEventsAPI:
			api, ok := evt.Data.(slackevents.EventsAPIEvent)
			if !ok {
				r.socket.Ack(*evt.Request)
				continue
			}
			r.socket.Ack(*evt.Request)
			r.handleEventsAPI(ctx, api)
		case socketmode.EventTypeInteractive:
			cb, ok := evt.Data.(slack.InteractionCallback)
			if !ok {
				r.socket.Ack(*evt.Request)
				continue
			}
			r.socket.Ack(*evt.Request)
			r.handleInteraction(ctx, cb)
		}
	}
}

func (r *Runtime) Start(ctx context.Context) error {
	go r.resolveBotIdentity(ctx)
	go r.consumeEvents(ctx)
	r.log.Info("slack socket mode starting")
	return r.socket.Run()
}

func (r *Runtime) resolveBotIdentity(ctx context.Context) {
	const (
		maxAttempts = 5
		timeout     = 20 * time.Second
	)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return
		}
		authCtx, cancel := context.WithTimeout(context.Background(), timeout)
		identity, err := r.client.AuthTestContext(authCtx)
		cancel()
		if err == nil {
			botUserID := strings.TrimSpace(identity.UserID)
			r.setBotUserID(botUserID)
			r.log.Info("resolved slack bot identity", "bot_user_id", botUserID, "bot_user_tag", r.cfg.BotUserTag)
			return
		}
		r.log.Warn("slack auth.test failed", "attempt", attempt, "max_attempts", maxAttempts, "error", err)
		if attempt == maxAttempts {
			r.log.Error("failed to resolve slack bot user id", "error", err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		}
	}
}

func (r *Runtime) setBotUserID(id string) {
	r.processedMu.Lock()
	r.botUserID = id
	r.processedMu.Unlock()
}

func (r *Runtime) slackBotUserID() string {
	r.processedMu.Lock()
	defer r.processedMu.Unlock()
	return r.botUserID
}

func (r *Runtime) handleSlashCommand(ctx context.Context, cmd slack.SlashCommand) {
	switch cmd.Command {
	case r.cfg.ChatCommand:
		r.respondChat(ctx, cmd)
	case r.cfg.ConfigCommand:
		r.respondConfig(ctx, cmd)
	}
}

func (r *Runtime) handleEventsAPI(ctx context.Context, evt slackevents.EventsAPIEvent) {
	switch inner := evt.InnerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		r.handleAppMention(ctx, evt, inner)
	case *slackevents.MessageEvent:
		r.handleMessageEvent(ctx, evt, inner)
	}
}

func (r *Runtime) handleAppMention(ctx context.Context, evt slackevents.EventsAPIEvent, inner *slackevents.AppMentionEvent) {
	if inner == nil || inner.BotID != "" {
		return
	}
	teamID := strings.TrimSpace(evt.TeamID)
	if teamID == "" {
		teamID = strings.TrimSpace(inner.UserTeam)
	}
	r.handleUserPrompt(ctx, chatStartRequest{
		TeamID:      teamID,
		ChannelID:   inner.Channel,
		UserID:      inner.User,
		Text:        inner.Text,
		MessageTS:   inner.TimeStamp,
		ThreadTS:    inner.ThreadTimeStamp,
		FromMention: true,
	})
}

func (r *Runtime) handleMessageEvent(ctx context.Context, evt slackevents.EventsAPIEvent, inner *slackevents.MessageEvent) {
	if inner == nil || inner.BotID != "" || inner.SubType != "" {
		return
	}
	r.handleUserPrompt(ctx, chatStartRequest{
		TeamID:    resolveTeamID(evt, inner),
		ChannelID: inner.Channel,
		UserID:    inner.User,
		Text:      inner.Text,
		MessageTS: inner.TimeStamp,
		ThreadTS:  inner.ThreadTimeStamp,
	})
}

func (r *Runtime) handleUserPrompt(ctx context.Context, req chatStartRequest) {
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		return
	}
	if req.TeamID == "" || req.ChannelID == "" || req.UserID == "" {
		r.log.Debug("ignoring event missing identifiers", "team_id", req.TeamID, "channel", req.ChannelID, "user", req.UserID)
		return
	}
	if req.MessageTS != "" {
		eventKey := fmt.Sprintf("%s:%s:%s", req.TeamID, req.ChannelID, req.MessageTS)
		if !r.markMessageAsNew(eventKey) {
			r.log.Debug("skipping duplicate message event", "event_key", eventKey)
			return
		}
	}

	_, _ = r.memory.Save(ctx, memory.Entry{
		SlackTeamID:    req.TeamID,
		SlackChannelID: req.ChannelID,
		UserID:         req.UserID,
		Content:        req.Text,
	})

	if req.ThreadTS != "" {
		if r.continueKnownThread(ctx, req) {
			return
		}
	}
	if req.FromMention || mentionsBot(req.Text, r.slackBotUserID(), r.cfg.BotUserTag) {
		r.startMentionConversation(ctx, req)
	}
}

func (r *Runtime) continueKnownThread(ctx context.Context, req chatStartRequest) bool {
	if r.agents == nil || r.repo == nil {
		return false
	}
	thread, err := r.repo.GetChatThread(ctx, req.TeamID, req.ChannelID, req.ThreadTS)
	if err != nil {
		r.log.Error("failed to load chat thread mapping", "error", err, "team_id", req.TeamID, "channel_id", req.ChannelID, "thread_ts", req.ThreadTS)
		return true
	}
	if thread == nil {
		r.log.Debug("thread message does not match known slacker thread", "thread_ts", req.ThreadTS)
		return false
	}
	recent, _ := r.memory.Recent(ctx, req.TeamID, req.ChannelID, 5)
	if err := r.postAgentResponseToThread(ctx, req.TeamID, req.ChannelID, req.UserID, req.ThreadTS, stripBotMentions(req.Text, r.slackBotUserID(), r.cfg.BotUserTag), "", recent); err != nil {
		r.log.Error("post threaded agent response", "error", err, "session_id", thread.SessionID)
	}
	return true
}

func resolveTeamID(evt slackevents.EventsAPIEvent, inner *slackevents.MessageEvent) string {
	if strings.TrimSpace(evt.TeamID) != "" {
		return strings.TrimSpace(evt.TeamID)
	}
	if inner == nil {
		return ""
	}
	if strings.TrimSpace(inner.Message.Team) != "" {
		return strings.TrimSpace(inner.Message.Team)
	}
	return ""
}

func (r *Runtime) markMessageAsNew(key string) bool {
	r.processedMu.Lock()
	defer r.processedMu.Unlock()

	now := time.Now()
	const ttl = 2 * time.Minute
	for k, ts := range r.processedMessages {
		if now.Sub(ts) > ttl {
			delete(r.processedMessages, k)
		}
	}
	if _, exists := r.processedMessages[key]; exists {
		return false
	}
	r.processedMessages[key] = now
	return true
}

func (r *Runtime) handleInteraction(_ context.Context, cb slack.InteractionCallback) {
	ctx := context.Background()
	r.log.Debug("trace /slacker-config interaction received",
		"type", cb.Type,
		"user", cb.User.ID,
		"callback_id", cb.View.CallbackID,
	)
	switch cb.Type {
	case slack.InteractionTypeViewSubmission:
		var err error
		switch cb.View.CallbackID {
		case mcpAddModalCallbackID:
			r.log.Debug("trace /slacker-config handling add submission", "user", cb.User.ID)
			err = r.handleMCPAddModalSubmission(ctx, cb)
		case mcpListModalCallbackID:
			r.log.Debug("trace /slacker-config handling list submission", "user", cb.User.ID)
			err = r.handleMCPListModalSubmission(ctx, cb)
		case mcpRemoveModalCallbackID:
			r.log.Debug("trace /slacker-config handling remove submission", "user", cb.User.ID)
			err = r.handleMCPRemoveModalSubmission(ctx, cb)
		}
		if err != nil {
			r.log.Error("config modal submission failed", "error", err, "user", cb.User.ID, "callback_id", cb.View.CallbackID)
			return
		}
		r.log.Debug("trace /slacker-config submission handled", "user", cb.User.ID, "callback_id", cb.View.CallbackID)
		return
	}
	r.log.Info("received interaction callback", "type", cb.Type, "user", cb.User.ID)
}
