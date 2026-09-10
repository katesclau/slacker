package openaiadapter

import (
	"encoding/json"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Asserts the serialized request, not the param structs that build it. openai-go tags
// these fields omitzero, so an unset param.Opt drops the field from the payload and still
// compiles -- a tool call would silently lose its correlation id with a green build.
func TestGetRequestInputWireFormat(t *testing.T) {
	req := &model.LLMRequest{Contents: []*genai.Content{
		{Role: "model", Parts: []*genai.Part{
			{Text: "calling a tool"},
			{FunctionCall: &genai.FunctionCall{
				ID:   "call_xyz789",
				Name: "list_repos",
				Args: map[string]any{"org": "katesclau"},
			}},
		}},
		{Role: "user", Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{
				ID:       "call_xyz789",
				Name:     "list_repos",
				Response: map[string]any{"ok": true},
			}},
		}},
	}}

	raw, err := json.Marshal(getRequestInput(req))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := []map[string]any{
		{
			"type":    "message",
			"role":    "assistant",
			"status":  "completed",
			"content": []any{map[string]any{"type": "output_text", "text": "calling a tool"}},
		},
		{
			"type":      "function_call",
			"call_id":   "call_xyz789",
			"name":      "list_repos",
			"arguments": `{"org":"katesclau"}`,
		},
		{
			"type":    "function_call_output",
			"call_id": "call_xyz789",
			"output":  `{"ok":true}`,
		},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d: %s", len(got), len(want), raw)
	}
	for i := range want {
		for key, wantVal := range want[i] {
			gotVal := got[i][key]
			if !jsonEqual(gotVal, wantVal) {
				t.Errorf("item %d, %q = %#v, want %#v", i, key, gotVal, wantVal)
			}
		}
	}
}

// An empty request must still produce a non-empty prompt, since the API rejects a blank one.
func TestGetRequestInputFallsBackToPlaceholder(t *testing.T) {
	raw, err := json.Marshal(getRequestInput(nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `" "` {
		t.Errorf("got %s, want a single-space string", raw)
	}
}

func jsonEqual(a, b any) bool {
	ra, err := json.Marshal(a)
	if err != nil {
		return false
	}
	rb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ra) == string(rb)
}
