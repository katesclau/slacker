package slackruntime

import "testing"

func TestFormatProgress(t *testing.T) {
	t.Parallel()

	got := formatProgress([]string{"Connecting MCP servers: atlassian", "Calling `list_projects`"})
	want := ":hourglass_flowing_sand: Working...\n• Connecting MCP servers: atlassian\n• Calling `list_projects`"
	if got != want {
		t.Fatalf("formatProgress() = %q, want %q", got, want)
	}
}

func TestProgressPublisherDedupeConsecutive(t *testing.T) {
	t.Parallel()

	p := &progressPublisher{}
	p.Push("Calling `list_projects`")
	p.Push("Calling `list_projects`")
	p.Push("Finished `list_projects`")
	if len(p.steps) != 2 {
		t.Fatalf("expected 2 unique steps, got %d: %#v", len(p.steps), p.steps)
	}
}
