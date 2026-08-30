package agents

import (
	"testing"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestSummarizeToolCall(t *testing.T) {
	t.Parallel()

	got := summarizeToolCall("jira_search", map[string]any{"jql": "project = FOX"})
	if got != "Calling `jira_search` (project = FOX)" {
		t.Fatalf("unexpected summary: %q", got)
	}
	if got := summarizeToolCall("list_projects", nil); got != "Calling `list_projects`" {
		t.Fatalf("unexpected empty-args summary: %q", got)
	}
}

func TestProgressEventsFromSession(t *testing.T) {
	t.Parallel()

	ev := &session.Event{}
	ev.Content = &genai.Content{
		Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{Name: "list_projects", Args: map[string]any{"query": "FOX"}}},
			{FunctionResponse: &genai.FunctionResponse{Name: "list_projects"}},
		},
	}
	got := progressEventsFromSession(ev)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].Message != "Calling `list_projects` (FOX)" {
		t.Fatalf("unexpected call event: %#v", got[0])
	}
	if got[1].Message != "Finished `list_projects`" {
		t.Fatalf("unexpected result event: %#v", got[1])
	}
}
