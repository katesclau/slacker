package agents

import (
	"fmt"
	"strings"

	"google.golang.org/adk/v2/session"
)

type ProgressEvent struct {
	Message string
	Tool    string
}

func progressEventsFromSession(ev *session.Event) []ProgressEvent {
	if ev == nil || ev.Content == nil {
		return nil
	}
	var out []ProgressEvent
	for _, part := range ev.Content.Parts {
		if part == nil {
			continue
		}
		if part.FunctionCall != nil {
			name := strings.TrimSpace(part.FunctionCall.Name)
			out = append(out, ProgressEvent{
				Tool:    name,
				Message: summarizeToolCall(name, part.FunctionCall.Args),
			})
			continue
		}
		if part.FunctionResponse != nil {
			name := strings.TrimSpace(part.FunctionResponse.Name)
			if name == "" {
				name = "MCP tool"
			}
			out = append(out, ProgressEvent{
				Tool:    name,
				Message: fmt.Sprintf("Finished `%s`", name),
			})
		}
	}
	return out
}

func summarizeToolCall(name string, args map[string]any) string {
	if name == "" {
		name = "MCP tool"
	}
	if hint := firstStringArg(args, "query", "q", "jql", "search", "name", "project", "id", "summary", "title"); hint != "" {
		return fmt.Sprintf("Calling `%s` (%s)", name, truncateRunes(hint, 80))
	}
	return fmt.Sprintf("Calling `%s`", name)
}

func firstStringArg(args map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := args[key]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s != "" {
			return s
		}
	}
	return ""
}

func truncateRunes(text string, max int) string {
	if max <= 0 || text == "" {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "…"
}
