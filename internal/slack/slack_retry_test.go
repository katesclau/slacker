package slackruntime

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSlackRetryFeedbackText(t *testing.T) {
	t.Parallel()

	got := slackRetryFeedbackText(1, 3, errors.New("context deadline exceeded"))
	if !strings.Contains(got, "Retrying 1/3") || !strings.Contains(got, "context deadline exceeded") {
		t.Fatalf("unexpected retry feedback: %q", got)
	}

	got = slackRetryExhaustedText(3, errors.New("context deadline exceeded"))
	if !strings.Contains(got, "3 attempts") || !strings.Contains(got, "context deadline exceeded") {
		t.Fatalf("unexpected exhausted feedback: %q", got)
	}
}

func TestSlackRetryBackoff(t *testing.T) {
	t.Parallel()

	if slackRetryBackoff(1) != 2*time.Second || slackRetryBackoff(2) != 4*time.Second {
		t.Fatalf("unexpected backoff: %s %s", slackRetryBackoff(1), slackRetryBackoff(2))
	}
}
