package blockkit

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

type ADKToolset struct {
	registry *Registry
}

func NewADKToolset(registry *Registry) (tool.Toolset, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry is required")
	}
	return &ADKToolset{registry: registry}, nil
}

func (t *ADKToolset) Name() string {
	return "slackblocks"
}

func (t *ADKToolset) Tools(_ agent.ReadonlyContext) ([]tool.Tool, error) {
	tools := NewTools(t.registry)

	headerTool, err := functiontool.New(functiontool.Config{
		Name:        "slack_block_header",
		Description: "Render a Slack header block and return a placeholder string.",
	}, func(_ agent.Context, args struct {
		Text string `json:"text"`
	}) (map[string]any, error) {
		return map[string]any{"placeholder": tools.Header(args.Text)}, nil
	})
	if err != nil {
		return nil, err
	}

	dividerTool, err := functiontool.New(functiontool.Config{
		Name:        "slack_block_divider",
		Description: "Render a Slack divider block and return a placeholder string.",
	}, func(_ agent.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"placeholder": tools.Divider()}, nil
	})
	if err != nil {
		return nil, err
	}

	noticeTool, err := functiontool.New(functiontool.Config{
		Name:        "slack_block_notice",
		Description: "Render a Slack notice block and return a placeholder string.",
	}, func(_ agent.Context, args struct {
		Level string `json:"level"`
		Text  string `json:"text"`
	}) (map[string]any, error) {
		return map[string]any{"placeholder": tools.Notice(args.Level, args.Text)}, nil
	})
	if err != nil {
		return nil, err
	}

	kvTool, err := functiontool.New(functiontool.Config{
		Name:        "slack_block_kv",
		Description: "Render key/value Slack fields block and return placeholder string.",
	}, func(_ agent.Context, args struct {
		Fields map[string]string `json:"fields"`
	}) (map[string]any, error) {
		return map[string]any{"placeholder": tools.KV(args.Fields)}, nil
	})
	if err != nil {
		return nil, err
	}

	actionsTool, err := functiontool.New(functiontool.Config{
		Name:        "slack_block_actions",
		Description: "Render URL action buttons and return placeholder string.",
	}, func(_ agent.Context, args struct {
		Buttons []ActionButton `json:"buttons"`
	}) (map[string]any, error) {
		return map[string]any{"placeholder": tools.Actions(args.Buttons)}, nil
	})
	if err != nil {
		return nil, err
	}

	return []tool.Tool{headerTool, dividerTool, noticeTool, kvTool, actionsTool}, nil
}
