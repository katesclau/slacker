package slackruntime

import (
	"fmt"
	"regexp"
	"strings"
)

func mentionsBot(text, botUserID, botUserTag string) bool {
	for _, re := range botMentionRegexps(botUserID, botUserTag) {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func stripBotMentions(text, botUserID, botUserTag string) string {
	out := text
	for _, re := range botMentionRegexps(botUserID, botUserTag) {
		out = re.ReplaceAllString(out, "")
	}
	return strings.TrimSpace(out)
}

func botMentionRegexps(botUserID, botUserTag string) []*regexp.Regexp {
	var out []*regexp.Regexp
	if id := strings.TrimSpace(botUserID); id != "" {
		out = append(out, regexp.MustCompile(fmt.Sprintf(`(?i)<@%s(?:\|[^>]+)?>`, regexp.QuoteMeta(id))))
	}
	if tag := normalizeConfiguredBotUserTag(botUserTag); tag != "" {
		quoted := regexp.QuoteMeta(tag)
		out = append(out,
			regexp.MustCompile(fmt.Sprintf(`(?i)<@[A-Z0-9]+\|%s>`, quoted)),
			regexp.MustCompile(fmt.Sprintf(`(?i)(?:^|[\s])@%s\b`, quoted)),
		)
	}
	return out
}

func normalizeConfiguredBotUserTag(tag string) string {
	return strings.TrimPrefix(strings.TrimSpace(tag), "@")
}
