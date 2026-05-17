package slackruntime

import (
	"context"
	"log/slog"
	"strings"

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
}

func New(cfg Config, log *slog.Logger, repo *postgres.Repository, memorySvc *memory.Service, blockTools *blockkit.Tools, agentRuntime *agents.Runtime) *Runtime {
	client := slack.New(cfg.BotToken, slack.OptionAppLevelToken(cfg.AppToken))
	socket := socketmode.New(client)
	return &Runtime{
		cfg:      cfg,
		log:      log,
		client:   client,
		socket:   socket,
		repo:     repo,
		memory:   memorySvc,
		blockkit: blockTools,
		agents:   agentRuntime,
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
	channel := inner.Channel
	teamID := evt.TeamID
	user := inner.User
	text := strings.TrimSpace(inner.Text)
	if text == "" {
		return
	}
	_, _ = r.memory.Save(ctx, memory.Entry{
		SlackTeamID:    teamID,
		SlackChannelID: channel,
		UserID:         user,
		Content:        text,
	})
}

func (r *Runtime) handleInteraction(_ context.Context, cb slack.InteractionCallback) {
	r.log.Info("received interaction callback", "type", cb.Type, "user", cb.User.ID)
}
