package prompt

import (
	"context"
	"fmt"

	"github.com/katesclau/slacker/internal/store/postgres"
)

type Service struct {
	repo        Repository
	objectStore ObjectStore
}

type SaveRequest struct {
	DocumentName string
	Content      []byte
	CreatedBy    string
	Description  string
}

type SaveResult struct {
	VersionID string
	Version   int
	ObjectKey string
	SHA256    string
}

type Repository interface {
	ListPromptVersions(ctx context.Context, docName string) ([]postgres.PromptVersion, error)
	SavePromptVersion(ctx context.Context, docName string, createdBy string, objectKey string, contentSHA string, description string) (string, int, error)
}

type ObjectStore interface {
	PutVersionedPrompt(ctx context.Context, docName string, version int, content []byte) (objectKey string, contentSHA string, err error)
}

func NewService(repo Repository, objectStore ObjectStore) *Service {
	return &Service{
		repo:        repo,
		objectStore: objectStore,
	}
}

func (s *Service) SaveVersion(ctx context.Context, req SaveRequest) (SaveResult, error) {
	if req.DocumentName == "" {
		return SaveResult{}, fmt.Errorf("document name is required")
	}
	if len(req.Content) == 0 {
		return SaveResult{}, fmt.Errorf("content is required")
	}
	nextVersion := 1
	existing, err := s.repo.ListPromptVersions(ctx, req.DocumentName)
	if err != nil {
		return SaveResult{}, err
	}
	if len(existing) > 0 {
		nextVersion = existing[0].Version + 1
	}

	objectKey, sha, err := s.objectStore.PutVersionedPrompt(ctx, req.DocumentName, nextVersion, req.Content)
	if err != nil {
		return SaveResult{}, err
	}

	versionID, version, err := s.repo.SavePromptVersion(ctx, req.DocumentName, req.CreatedBy, objectKey, sha, req.Description)
	if err != nil {
		return SaveResult{}, err
	}

	return SaveResult{
		VersionID: versionID,
		Version:   version,
		ObjectKey: objectKey,
		SHA256:    sha,
	}, nil
}
