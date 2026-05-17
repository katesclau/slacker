package prompt

import (
	"context"
	"testing"

	"github.com/katesclau/slacker/internal/store/postgres"
)

func TestSaveVersion(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStore{}
	svc := NewService(repo, store)

	out, err := svc.SaveVersion(context.Background(), SaveRequest{
		DocumentName: "system-prompt",
		Content:      []byte("hello"),
		CreatedBy:    "U1",
		Description:  "initial",
	})
	if err != nil {
		t.Fatalf("save version: %v", err)
	}
	if out.Version != 1 {
		t.Fatalf("expected version 1, got %d", out.Version)
	}
}

type fakeRepo struct{}

func (f *fakeRepo) ListPromptVersions(_ context.Context, _ string) ([]postgres.PromptVersion, error) {
	return nil, nil
}

func (f *fakeRepo) SavePromptVersion(_ context.Context, _ string, _ string, _ string, _ string, _ string) (string, int, error) {
	return "version-id", 1, nil
}

type fakeStore struct{}

func (f *fakeStore) PutVersionedPrompt(_ context.Context, _ string, _ int, _ []byte) (string, string, error) {
	return "prompts/system/v1.txt", "abc123", nil
}
