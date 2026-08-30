package agents

import (
	"strings"
	"testing"
)

func TestDefaultInstructionCoversCoreFlows(t *testing.T) {
	t.Parallel()

	for _, needle := range []string{"/slacker", "@slacker-dev", "/slacker-config", "OAuth", "Connect"} {
		if !strings.Contains(defaultInstruction, needle) {
			t.Fatalf("default instruction missing %q", needle)
		}
	}
}
