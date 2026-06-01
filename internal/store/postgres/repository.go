package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/katesclau/slacker/internal/mcpauth"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) UpsertUser(ctx context.Context, user User) error {
	if user.ID == "" {
		user.ID = uuid.NewString()
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO users (id, slack_user_id, slack_team_id, display_name, email)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (slack_user_id, slack_team_id)
		DO UPDATE SET display_name = EXCLUDED.display_name, email = EXCLUDED.email, updated_at = now()
	`, user.ID, user.SlackUserID, user.SlackTeamID, user.DisplayName, user.Email)
	return err
}

func (r *Repository) UpsertChannel(ctx context.Context, channel Channel) error {
	if channel.ID == "" {
		channel.ID = uuid.NewString()
	}
	if len(channel.SettingsJSON) == 0 {
		channel.SettingsJSON = []byte("{}")
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO channels (id, slack_channel_id, slack_team_id, name, settings_json)
		VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT (slack_channel_id, slack_team_id)
		DO UPDATE SET name = EXCLUDED.name, settings_json = EXCLUDED.settings_json, updated_at = now()
	`, channel.ID, channel.SlackChannelID, channel.SlackTeamID, channel.Name, string(channel.SettingsJSON))
	return err
}

func (r *Repository) CreateAgent(ctx context.Context, a Agent) (string, error) {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	if len(a.ConfigJSON) == 0 {
		a.ConfigJSON = []byte("{}")
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO agents (id, name, description, config_json, created_by)
		VALUES ($1, $2, $3, $4::jsonb, $5)
	`, a.ID, a.Name, a.Description, string(a.ConfigJSON), a.CreatedBy)
	return a.ID, err
}

func (r *Repository) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, description, config_json, created_by, created_at, updated_at
		FROM agents
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Agent{}
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.ConfigJSON, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) GetAgentByName(ctx context.Context, name string) (*Agent, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, name, description, config_json, created_by, created_at, updated_at
		FROM agents
		WHERE name = $1
	`, name)

	var a Agent
	if err := row.Scan(&a.ID, &a.Name, &a.Description, &a.ConfigJSON, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (r *Repository) GrantPermission(ctx context.Context, p Permission) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO permissions (id, subject_ref, object_ref, action)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING
	`, p.ID, p.SubjectRef, p.ObjectRef, p.Action)
	return err
}

func (r *Repository) PutDelegatedOAuthToken(ctx context.Context, rec mcpauth.TokenRecord) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO mcp_oauth_tokens (
			mcp_server, slack_team_id, slack_user_id, resource, issuer, client_id,
			enc_access_token, enc_refresh_token, scope_csv, expires_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (mcp_server, slack_team_id, slack_user_id)
		DO UPDATE SET
			resource = EXCLUDED.resource,
			issuer = EXCLUDED.issuer,
			client_id = EXCLUDED.client_id,
			enc_access_token = EXCLUDED.enc_access_token,
			enc_refresh_token = EXCLUDED.enc_refresh_token,
			scope_csv = EXCLUDED.scope_csv,
			expires_at = EXCLUDED.expires_at,
			updated_at = EXCLUDED.updated_at
	`, rec.MCPServer, rec.SlackTeamID, rec.SlackUserID, rec.Resource, rec.Issuer, rec.ClientID,
		rec.EncAccess, rec.EncRefresh, rec.Scope, rec.ExpiresAt, rec.CreatedAt, rec.LastUpdatedAt)
	return err
}

func (r *Repository) GetDelegatedOAuthToken(ctx context.Context, teamID string, userID string, mcpServer string) (*mcpauth.TokenRecord, error) {
	row := r.db.QueryRow(ctx, `
		SELECT mcp_server, slack_team_id, slack_user_id, resource, issuer, client_id,
		       enc_access_token, enc_refresh_token, scope_csv, expires_at, created_at, updated_at
		FROM mcp_oauth_tokens
		WHERE mcp_server = $1 AND slack_team_id = $2 AND slack_user_id = $3
	`, mcpServer, teamID, userID)

	var out mcpauth.TokenRecord
	if err := row.Scan(
		&out.MCPServer, &out.SlackTeamID, &out.SlackUserID, &out.Resource, &out.Issuer, &out.ClientID,
		&out.EncAccess, &out.EncRefresh, &out.Scope, &out.ExpiresAt, &out.CreatedAt, &out.LastUpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *Repository) SavePromptVersion(ctx context.Context, docName string, createdBy string, objectKey string, contentSHA string, description string) (string, int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	docID, err := r.ensurePromptDocument(ctx, tx, docName, createdBy)
	if err != nil {
		return "", 0, err
	}

	var nextVersion int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM prompt_versions WHERE document_id = $1`, docID).Scan(&nextVersion); err != nil {
		return "", 0, err
	}

	versionID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO prompt_versions (id, document_id, version, object_key, content_sha256, created_by, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, versionID, docID, nextVersion, objectKey, contentSHA, createdBy, description)
	if err != nil {
		return "", 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	return versionID, nextVersion, nil
}

func (r *Repository) ensurePromptDocument(ctx context.Context, tx pgx.Tx, name string, createdBy string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM prompt_documents WHERE name = $1`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}

	id = uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO prompt_documents (id, name, created_by)
		VALUES ($1, $2, $3)
	`, id, name, createdBy)
	return id, err
}

func (r *Repository) ListPromptVersions(ctx context.Context, docName string) ([]PromptVersion, error) {
	rows, err := r.db.Query(ctx, `
		SELECT pv.id, pv.document_id, pv.version, pv.object_key, pv.content_sha256, pv.created_by, pv.created_at, COALESCE(pv.description, '')
		FROM prompt_versions pv
		JOIN prompt_documents pd ON pd.id = pv.document_id
		WHERE pd.name = $1
		ORDER BY pv.version DESC
	`, docName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PromptVersion{}
	for rows.Next() {
		var p PromptVersion
		if err := rows.Scan(&p.ID, &p.DocumentID, &p.Version, &p.ObjectKey, &p.ContentSHA, &p.CreatedBy, &p.CreatedAt, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) ListMCPServers(ctx context.Context) ([]MCPServer, error) {
	rows, err := r.db.Query(ctx, `
		SELECT name, resource_url, issuer_url, client_id, client_secret_enc, enabled, scopes_csv, created_at, updated_at
		FROM mcp_servers
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MCPServer{}
	for rows.Next() {
		var s MCPServer
		if err := rows.Scan(&s.Name, &s.ResourceURL, &s.IssuerURL, &s.ClientID, &s.ClientSecretEnc, &s.Enabled, &s.ScopesCSV, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertMCPServer(ctx context.Context, server MCPServer) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO mcp_servers (name, resource_url, issuer_url, client_id, client_secret_enc, enabled, scopes_csv)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (name)
		DO UPDATE SET
			resource_url = EXCLUDED.resource_url,
			issuer_url = EXCLUDED.issuer_url,
			client_id = EXCLUDED.client_id,
			client_secret_enc = EXCLUDED.client_secret_enc,
			enabled = EXCLUDED.enabled,
			scopes_csv = EXCLUDED.scopes_csv,
			updated_at = now()
	`, server.Name, server.ResourceURL, server.IssuerURL, server.ClientID, server.ClientSecretEnc, server.Enabled, server.ScopesCSV)
	return err
}

func (r *Repository) SetEnabledMCPServers(ctx context.Context, names []string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `UPDATE mcp_servers SET enabled = false, updated_at = now()`); err != nil {
		return err
	}
	if len(names) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE mcp_servers SET enabled = true, updated_at = now() WHERE name = ANY($1)`, names); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) DeleteMCPServer(ctx context.Context, name string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM mcp_servers WHERE name = $1`, name)
	return err
}

func (r *Repository) UserHasEnabledMCPAccess(ctx context.Context, teamID string, userID string) (bool, error) {
	row := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM mcp_oauth_tokens tok
			JOIN mcp_servers srv ON srv.name = tok.mcp_server
			WHERE tok.slack_team_id = $1
			  AND tok.slack_user_id = $2
			  AND tok.expires_at > now()
			  AND srv.enabled = true
		)
	`, teamID, userID)
	var exists bool
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *Repository) UpsertChatThread(ctx context.Context, thread ChatThread) error {
	if thread.ID == "" {
		thread.ID = uuid.NewString()
	}
	if thread.CreatedBy == "" {
		thread.CreatedBy = "system"
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO chat_threads (id, slack_team_id, slack_channel_id, slack_thread_ts, session_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (slack_team_id, slack_channel_id, slack_thread_ts)
		DO UPDATE SET session_id = EXCLUDED.session_id, updated_at = now()
	`, thread.ID, thread.SlackTeamID, thread.SlackChannelID, thread.SlackThreadTS, thread.SessionID, thread.CreatedBy)
	return err
}

func (r *Repository) GetChatThread(ctx context.Context, teamID string, channelID string, threadTS string) (*ChatThread, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, slack_team_id, slack_channel_id, slack_thread_ts, session_id, created_by, created_at, updated_at
		FROM chat_threads
		WHERE slack_team_id = $1 AND slack_channel_id = $2 AND slack_thread_ts = $3
	`, teamID, channelID, threadTS)

	var out ChatThread
	if err := row.Scan(
		&out.ID, &out.SlackTeamID, &out.SlackChannelID, &out.SlackThreadTS, &out.SessionID, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func MustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("marshal json: %w", err))
	}
	return raw
}

func NowUTC() time.Time {
	return time.Now().UTC()
}
