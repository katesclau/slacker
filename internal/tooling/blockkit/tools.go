package blockkit

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/slack-go/slack"
)

// Tools ports the core slack_block_* behavior from mcp-slackitt.
// Each method stores generated blocks and returns a placeholder.
type Tools struct {
	registry *Registry
}

var placeholderPattern = regexp.MustCompile(`%%slack-block-[a-f0-9-]+%%`)

func NewTools(registry *Registry) *Tools {
	return &Tools{registry: registry}
}

func (t *Tools) Header(text string) string {
	block := slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, trimTo(text, 150), false, false))
	return t.registry.Store(block)
}

func (t *Tools) Divider() string {
	return t.registry.Store(slack.NewDividerBlock())
}

func (t *Tools) Context(text string) string {
	elem := slack.NewTextBlockObject(slack.MarkdownType, text, false, false)
	return t.registry.Store(slack.NewContextBlock("", elem))
}

func (t *Tools) Notice(level string, text string) string {
	emoji := ":information_source:"
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "success":
		emoji = ":white_check_mark:"
	case "warning":
		emoji = ":warning:"
	case "error":
		emoji = ":x:"
	}
	body := fmt.Sprintf("%s %s", emoji, text)
	return t.registry.Store(slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, body, false, false), nil, nil))
}

func (t *Tools) KV(fields map[string]string) string {
	items := make([]*slack.TextBlockObject, 0, len(fields))
	for k, v := range fields {
		items = append(items, slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("*%s*\n%s", k, v), false, false))
	}
	return t.registry.Store(slack.NewSectionBlock(nil, items, nil))
}

func (t *Tools) Actions(buttons []ActionButton) string {
	if len(buttons) > 5 {
		buttons = buttons[:5]
	}
	elements := make([]slack.BlockElement, 0, len(buttons))
	for i, b := range buttons {
		if strings.TrimSpace(b.URL) == "" {
			continue
		}
		elements = append(elements, slack.NewButtonBlockElement(
			fmt.Sprintf("action-%d", i+1),
			"",
			slack.NewTextBlockObject(slack.PlainTextType, trimTo(b.Text, 75), false, false),
		).WithURL(b.URL))
	}
	return t.registry.Store(slack.NewActionBlock("", elements...))
}

func (t *Tools) JSON(raw string) (string, error) {
	var blocks []slack.Block
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
			return "", err
		}
	} else {
		var block slack.Block
		if err := json.Unmarshal([]byte(raw), &block); err != nil {
			return "", err
		}
		blocks = []slack.Block{block}
	}
	return t.registry.Store(blocks...), nil
}

func (t *Tools) ResolvePlaceholders(text string) (string, []slack.Block) {
	collected := make([]slack.Block, 0, 4)
	matches := placeholderPattern.FindAllString(text, -1)
	for _, match := range matches {
		if blocks, ok := t.registry.Consume(match); ok {
			collected = append(collected, blocks...)
		}
	}
	clean := placeholderPattern.ReplaceAllString(text, "")
	return strings.TrimSpace(clean), collected
}

type ActionButton struct {
	Text string
	URL  string
}

func trimTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
