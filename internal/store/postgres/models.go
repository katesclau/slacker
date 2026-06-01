package postgres

import "time"

type User struct {
	ID          string
	SlackUserID string
	SlackTeamID string
	DisplayName string
	Email       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Channel struct {
	ID             string
	SlackChannelID string
	SlackTeamID    string
	Name           string
	SettingsJSON   []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Agent struct {
	ID          string
	Name        string
	Description string
	ConfigJSON  []byte
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Permission struct {
	ID         string
	SubjectRef string
	ObjectRef  string
	Action     string
	CreatedAt  time.Time
}

type MCPServer struct {
	Name            string
	ResourceURL     string
	IssuerURL       string
	ClientID        string
	ClientSecretEnc string
	Enabled         bool
	ScopesCSV       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type PromptDocument struct {
	ID        string
	Name      string
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type PromptVersion struct {
	ID          string
	DocumentID  string
	Version     int
	ObjectKey   string
	ContentSHA  string
	CreatedBy   string
	CreatedAt   time.Time
	Description string
}

type ChatThread struct {
	ID             string
	SlackTeamID    string
	SlackChannelID string
	SlackThreadTS  string
	SessionID      string
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
