package slackruntime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/katesclau/slacker/internal/store/postgres"
)

func TestShouldTriggerMCPAccessFlowEmptyText(t *testing.T) {
	t.Parallel()

	r := &Runtime{}
	if r.shouldTriggerMCPAccessFlow(context.Background(), "   ") {
		t.Fatal("expected empty text not to trigger oauth flow")
	}
}

func TestOAuthEnabledMCPServers(t *testing.T) {
	t.Parallel()

	servers := []postgres.MCPServer{
		{Name: "github", IssuerURL: "https://github.com/login/oauth", Enabled: true},
		{Name: "jira", IssuerURL: "https://auth.atlassian.com", Enabled: false},
		{Name: "local", IssuerURL: "", Enabled: true},
		{Name: "linear", IssuerURL: "https://linear.app/oauth", Enabled: true},
	}
	got := oauthEnabledMCPServers(servers)
	if len(got) != 2 {
		t.Fatalf("expected 2 oauth-enabled servers, got %d", len(got))
	}
	if got[0].Name != "github" || got[1].Name != "linear" {
		t.Fatalf("unexpected servers: %#v", got)
	}
}

func TestParseMCPAccessDecision(t *testing.T) {
	t.Parallel()

	servers := []postgres.MCPServer{
		{Name: "github"},
		{Name: "jira"},
	}

	cases := []struct {
		name        string
		raw         string
		wantNeeded  bool
		wantServers []string
	}{
		{
			name:        "json match",
			raw:         `{"needed":true,"servers":["github"]}`,
			wantNeeded:  true,
			wantServers: []string{"github"},
		},
		{
			name:        "fenced json and unknown server ignored",
			raw:         "```json\n{\"needed\":true,\"servers\":[\"Jira\",\"unknown\"]}\n```",
			wantNeeded:  true,
			wantServers: []string{"jira"},
		},
		{
			name:        "needed without configured server",
			raw:         `{"needed":true,"servers":[]}`,
			wantNeeded:  true,
			wantServers: nil,
		},
		{
			name:        "plain text is not a match",
			raw:         "yes, github",
			wantNeeded:  false,
			wantServers: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			needed, matched := parseMCPAccessDecision(tc.raw, servers)
			if needed != tc.wantNeeded {
				t.Fatalf("needed=%v, want %v", needed, tc.wantNeeded)
			}
			if len(matched) != len(tc.wantServers) {
				t.Fatalf("matched=%v, want %v", matched, tc.wantServers)
			}
			for i := range matched {
				if matched[i] != tc.wantServers[i] {
					t.Fatalf("matched=%v, want %v", matched, tc.wantServers)
				}
			}
		})
	}
}

func TestTextMentionsOAuthMCPServer(t *testing.T) {
	t.Parallel()

	servers := []postgres.MCPServer{
		{Name: "linear", ResourceURL: "https://mcp.linear.app/sse", IssuerURL: "https://linear.app/oauth"},
	}
	if !textMentionsOAuthMCPServer("please check Linear issues", servers) {
		t.Fatal("expected server name mention to match")
	}
	if !textMentionsOAuthMCPServer("use mcp.linear.app", servers) {
		t.Fatal("expected resource host mention to match")
	}
	if textMentionsOAuthMCPServer("what is for lunch?", servers) {
		t.Fatal("expected unrelated text not to match")
	}
}

func TestClassifyOAuthMCPAccessUsesModelAndConfiguredServers(t *testing.T) {
	t.Parallel()

	servers := []postgres.MCPServer{
		{Name: "github", ResourceURL: "https://api.githubcopilot.com/mcp/", IssuerURL: "https://github.com/login/oauth", Enabled: true},
		{Name: "jira", ResourceURL: "https://mcp.atlassian.com/v1/mcp", IssuerURL: "https://auth.atlassian.com", Enabled: true},
	}
	stub := &stubCompleter{reply: `{"needed":true,"servers":["jira"]}`}
	r := &Runtime{model: stub}

	if !r.classifyOAuthMCPAccess(context.Background(), "create a Jira ticket for this bug", servers) {
		t.Fatal("expected classifier to match jira")
	}
	if stub.lastModel != mcpAccessClassifierModel {
		t.Fatalf("expected light model %q, got %q", mcpAccessClassifierModel, stub.lastModel)
	}
	if !strings.Contains(stub.lastUser, "github") || !strings.Contains(stub.lastUser, "jira") || !strings.Contains(stub.lastUser, "create a Jira ticket") {
		t.Fatalf("classifier prompt missing configured servers or user text: %q", stub.lastUser)
	}
}

func TestClassifyOAuthMCPAccessIgnoresHallucinatedServers(t *testing.T) {
	t.Parallel()

	servers := []postgres.MCPServer{
		{Name: "github", IssuerURL: "https://github.com/login/oauth", Enabled: true},
	}
	r := &Runtime{model: &stubCompleter{reply: `{"needed":true,"servers":["notion"]}`}}
	if r.classifyOAuthMCPAccess(context.Background(), "open my Notion page", servers) {
		t.Fatal("expected hallucinated server not to trigger oauth flow")
	}
}

func TestClassifyOAuthMCPAccessFallsBackWhenModelFails(t *testing.T) {
	t.Parallel()

	servers := []postgres.MCPServer{
		{Name: "linear", IssuerURL: "https://linear.app/oauth", Enabled: true},
	}
	r := &Runtime{model: &stubCompleter{err: errors.New("openai unavailable")}}
	if !r.classifyOAuthMCPAccess(context.Background(), "list linear projects", servers) {
		t.Fatal("expected name fallback after classifier failure")
	}
	if r.classifyOAuthMCPAccess(context.Background(), "hello there", servers) {
		t.Fatal("expected unrelated text not to match after classifier failure")
	}
}

type stubCompleter struct {
	reply     string
	err       error
	lastModel string
	lastUser  string
}

func (s *stubCompleter) Complete(_ context.Context, modelName, _, user string) (string, error) {
	s.lastModel = modelName
	s.lastUser = user
	return s.reply, s.err
}
