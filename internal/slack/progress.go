package slackruntime

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/katesclau/slacker/internal/agents"
	"github.com/slack-go/slack"
)

const (
	progressMaxSteps     = 8
	progressMinInterval  = 400 * time.Millisecond
	progressWriteTimeout = 5 * time.Second
)

type progressPublisher struct {
	runtime    *Runtime
	channelID  string
	thinkingTS string
	steps      []string
	lastSent   time.Time
	mu         sync.Mutex
}

func newProgressPublisher(r *Runtime, channelID, thinkingTS string) *progressPublisher {
	if r == nil || r.client == nil || strings.TrimSpace(thinkingTS) == "" {
		return nil
	}
	return &progressPublisher{
		runtime:    r,
		channelID:  channelID,
		thinkingTS: thinkingTS,
	}
}

func (p *progressPublisher) Push(message string) {
	if p == nil {
		return
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if n := len(p.steps); n > 0 && p.steps[n-1] == message {
		return
	}
	p.steps = append(p.steps, message)
	if len(p.steps) > progressMaxSteps {
		p.steps = p.steps[len(p.steps)-progressMaxSteps:]
	}
	if !p.lastSent.IsZero() && time.Since(p.lastSent) < progressMinInterval {
		return
	}
	p.flushLocked()
}

func (p *progressPublisher) Handle(ev agents.ProgressEvent) {
	p.Push(ev.Message)
}

func (p *progressPublisher) Flush() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flushLocked()
}

func (p *progressPublisher) flushLocked() {
	if p.runtime == nil || p.runtime.client == nil || len(p.steps) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), progressWriteTimeout)
	defer cancel()
	_, _, _, err := p.runtime.client.UpdateMessageContext(
		ctx,
		p.channelID,
		p.thinkingTS,
		slack.MsgOptionText(formatProgress(p.steps), false),
	)
	if err != nil && p.runtime.log != nil {
		p.runtime.log.Debug("failed updating slack progress", "error", err)
	}
	p.lastSent = time.Now()
}

func formatProgress(steps []string) string {
	var b strings.Builder
	b.WriteString(":hourglass_flowing_sand: Working...\n")
	for _, step := range steps {
		b.WriteString("• ")
		b.WriteString(step)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
