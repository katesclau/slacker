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
	AdminUsers    []string
	PublicBaseURL string
}

type Runtime struct {
	cfg      Config
	log      *slog.Logger
	client   *slack.Client
	socket   *socketmode.Client
	repo     *postgres.Repository
	memory   *memory.Service
	blockkit *blockkit.Tools
	agents   *agents.Runtime

	processedMu       sync.Mutex
	processedMessages map[string]time.Time
}

func New(cfg Config, log *slog.Logger, repo *postgres.Repository, memorySvc *memory.Service, blockTools *blockkit.Tools, agentRuntime *agents.Runtime) *Runtime {
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
	go r.consumeEvents(ctx)
	r.log.Info("slack socket mode starting")
	return r.socket.Run()
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
	inner, ok := evt.InnerEvent.Data.(*slackevents.MessageEvent)
	if !ok {
		return
	}
	if inner.BotID != "" {
		return
	}
	if inner.SubType != "" {
		return
	}
	channel := inner.Channel
	teamID := resolveTeamID(evt, inner)
	user := inner.User
	text := strings.TrimSpace(inner.Text)
	if text == "" {
		return
	}
	if teamID == "" || channel == "" || user == "" {
		r.log.Debug("ignoring message event missing identifiers", "team_id", teamID, "channel", channel, "user", user)
		return
	}
	if inner.TimeStamp != "" {
		eventKey := fmt.Sprintf("%s:%s:%s", teamID, channel, inner.TimeStamp)
		if !r.markMessageAsNew(eventKey) {
			r.log.Debug("skipping duplicate message event", "event_key", eventKey)
			return
		}
	}
	_, _ = r.memory.Save(ctx, memory.Entry{
		SlackTeamID:    teamID,
		SlackChannelID: channel,
		UserID:         user,
		Content:        text,
	})

	if inner.ThreadTimeStamp == "" {
		return
	}
	if r.agents == nil {
		return
	}
	thread, err := r.repo.GetChatThread(ctx, teamID, channel, inner.ThreadTimeStamp)
	if err != nil {
		r.log.Error("failed to load chat thread mapping", "error", err, "team_id", teamID, "channel_id", channel, "thread_ts", inner.ThreadTimeStamp)
		return
	}
	if thread == nil {
		r.log.Debug("thread message does not match known slacker thread", "thread_ts", inner.ThreadTimeStamp)
		return
	}

	recent, _ := r.memory.Recent(ctx, teamID, channel, 5)
	if err := r.postAgentResponseToThread(ctx, teamID, channel, user, inner.ThreadTimeStamp, text, "", recent); err != nil {
		r.log.Error("post threaded agent response", "error", err, "session_id", thread.SessionID)
	}
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
