package minio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Bucket    string
}

type Store struct {
	client *minio.Client
	bucket string
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check minio bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create minio bucket: %w", err)
		}
	}

	return &Store{client: client, bucket: cfg.Bucket}, nil
}

func (s *Store) PutVersionedPrompt(ctx context.Context, docName string, version int, content []byte) (objectKey string, contentSHA string, err error) {
	contentSHA = sha256Hex(content)
	objectKey = fmt.Sprintf("prompts/%s/v%d-%s.txt", sanitize(docName), version, contentSHA[:12])

	_, err = s.client.PutObject(ctx, s.bucket, objectKey, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{
		ContentType: "text/plain; charset=utf-8",
		UserMetadata: map[string]string{
			"prompt_name": docName,
			"sha256":      contentSHA,
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("put minio object: %w", err)
	}
	return objectKey, contentSHA, nil
}

func (s *Store) GetPrompt(ctx context.Context, objectKey string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	raw, err := io.ReadAll(obj)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func sanitize(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return "unnamed"
	}
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "/", "-")
	return name
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
