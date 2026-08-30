package openaiadapter

import "testing"

func TestCompleteRequiresPrompt(t *testing.T) {
	t.Parallel()

	adapter := &ModelAdapter{}
	if _, err := adapter.Complete(t.Context(), "gpt-5-mini", "system", "   "); err == nil {
		t.Fatal("expected empty prompt to fail")
	}
}

func TestCompleteRequiresAdapter(t *testing.T) {
	t.Parallel()

	var adapter *ModelAdapter
	if _, err := adapter.Complete(t.Context(), "gpt-5-mini", "system", "hello"); err == nil {
		t.Fatal("expected nil adapter to fail")
	}
}
