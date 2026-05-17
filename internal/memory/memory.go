package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Entry struct {
	ID             string
	SlackTeamID    string
	SlackChannelID string
	UserID         string
	Content        string
	Metadata       map[string]any
	Embedding      []float32
	CreatedAt      time.Time
}

type Service struct {
	db    *pgxpool.Pool
	redis *redis.Client
}

func NewService(db *pgxpool.Pool, redisClient *redis.Client) *Service {
	return &Service{db: db, redis: redisClient}
}

func (s *Service) Save(ctx context.Context, entry Entry) (string, error) {
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	meta := entry.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	metaJSON, _ := json.Marshal(meta)
	vectorLiteral := vectorAsLiteral(entry.Embedding)

	_, err := s.db.Exec(ctx, `
		INSERT INTO memory_entries (id, slack_team_id, slack_channel_id, user_id, content, metadata_json, embedding, created_at)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::vector,$8)
	`, entry.ID, entry.SlackTeamID, entry.SlackChannelID, entry.UserID, entry.Content, string(metaJSON), vectorLiteral, entry.CreatedAt)
	if err != nil {
		return "", err
	}

	cacheKey := recentCacheKey(entry.SlackTeamID, entry.SlackChannelID)
	_, _ = s.redis.LPush(ctx, cacheKey, entry.Content).Result()
	_, _ = s.redis.LTrim(ctx, cacheKey, 0, 99).Result()
	_, _ = s.redis.Expire(ctx, cacheKey, 24*time.Hour).Result()

	return entry.ID, nil
}

func (s *Service) Recent(ctx context.Context, teamID string, channelID string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 20
	}
	cacheKey := recentCacheKey(teamID, channelID)
	cached, err := s.redis.LRange(ctx, cacheKey, 0, int64(limit-1)).Result()
	if err == nil && len(cached) > 0 {
		return cached, nil
	}

	rows, err := s.db.Query(ctx, `
		SELECT content
		FROM memory_entries
		WHERE slack_team_id = $1 AND slack_channel_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, teamID, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0, limit)
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		out = append(out, content)
	}
	return out, rows.Err()
}

func (s *Service) Search(ctx context.Context, teamID string, channelID string, embedding []float32, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 8
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, slack_team_id, slack_channel_id, user_id, content, metadata_json, created_at
		FROM memory_entries
		WHERE slack_team_id = $1 AND slack_channel_id = $2
		ORDER BY embedding <=> $3::vector
		LIMIT $4
	`, teamID, channelID, vectorAsLiteral(embedding), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Entry, 0, limit)
	for rows.Next() {
		var e Entry
		var metadataRaw []byte
		if err := rows.Scan(&e.ID, &e.SlackTeamID, &e.SlackChannelID, &e.UserID, &e.Content, &metadataRaw, &e.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metadataRaw, &e.Metadata)
		out = append(out, e)
	}
	return out, rows.Err()
}

func recentCacheKey(teamID string, channelID string) string {
	return fmt.Sprintf("memory:recent:%s:%s", teamID, channelID)
}

func vectorAsLiteral(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	out := "["
	for i, val := range v {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf("%f", val)
	}
	out += "]"
	return out
}
