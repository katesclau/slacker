package slackruntime

import "testing"

func TestMentionsBot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		id   string
		tag  string
		want bool
	}{
		{name: "plain mention by id", text: "<@U0SLACKER> list repos", id: "U0SLACKER", want: true},
		{name: "mention with configured tag label", text: "hey <@U0SLACKER|slacker-dev> hello", id: "U999", tag: "slacker-dev", want: true},
		{name: "plain @tag", text: "@slacker-dev list repos", tag: "slacker-dev", want: true},
		{name: "tag with leading at in config", text: "@slacker-dev hello", tag: "@slacker-dev", want: true},
		{name: "other user", text: "<@U999> hello", id: "U0SLACKER", tag: "slacker-dev", want: false},
		{name: "no mention", text: "hello there", id: "U0SLACKER", tag: "slacker-dev", want: false},
		{name: "empty identity ignored", text: "<@U0SLACKER> hello", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := mentionsBot(tc.text, tc.id, tc.tag); got != tc.want {
				t.Fatalf("mentionsBot(%q, %q, %q)=%v, want %v", tc.text, tc.id, tc.tag, got, tc.want)
			}
		})
	}
}

func TestStripBotMentions(t *testing.T) {
	t.Parallel()

	got := stripBotMentions("<@U0SLACKER|slacker-dev> @default_agent list pull requests", "U0SLACKER", "slacker-dev")
	if got != "@default_agent list pull requests" {
		t.Fatalf("stripped text=%q", got)
	}
	got = stripBotMentions("@slacker-dev @default_agent list pull requests", "", "slacker-dev")
	if got != "@default_agent list pull requests" {
		t.Fatalf("stripped tag text=%q", got)
	}
	if got := stripBotMentions("no mention here", "U0SLACKER", "slacker-dev"); got != "no mention here" {
		t.Fatalf("unexpected strip: %q", got)
	}
}
