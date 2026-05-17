package blockkit

import "testing"

func TestRegistryStoreConsume(t *testing.T) {
	reg := NewRegistry()
	tools := NewTools(reg)

	ph := tools.Notice("info", "hello world")
	if ph == "" {
		t.Fatal("expected placeholder")
	}

	text, blocks := tools.ResolvePlaceholders(ph)
	if text != "" {
		t.Fatalf("expected placeholders to be stripped, got %q", text)
	}
	if len(blocks) == 0 {
		t.Fatal("expected resolved blocks")
	}
}

func TestActionsLimit(t *testing.T) {
	reg := NewRegistry()
	tools := NewTools(reg)
	ph := tools.Actions([]ActionButton{
		{Text: "1", URL: "https://example.com/1"},
		{Text: "2", URL: "https://example.com/2"},
		{Text: "3", URL: "https://example.com/3"},
		{Text: "4", URL: "https://example.com/4"},
		{Text: "5", URL: "https://example.com/5"},
		{Text: "6", URL: "https://example.com/6"},
	})
	if ph == "" {
		t.Fatal("expected placeholder for actions")
	}
}
