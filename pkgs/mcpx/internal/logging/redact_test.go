package logging_test

import (
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/logging"
)

func TestRedactElidesByNameAndByValue(t *testing.T) {
	// Matching only on the name was not enough. MCPX_SCRIPT_ENV carries
	// KEY=VALUE pairs for the script, so an innocuous name holds a secret as
	// its value.
	cases := []struct {
		name, value string
		leaks       string // must not appear in the output
	}{
		{"MCPX_API_TOKEN", "abcdef123456", "abcdef123456"},
		{"MCPX_SECRET_THING", "hunter2", "hunter2"},
		{"MCPX_AUTH", "Bearer xyzzy", "xyzzy"},
		{"MCPX_SCRIPT_ENV", "API_TOKEN=abcdef123456", "abcdef123456"},
		{"MCPX_SCRIPT_ENV", "PASSWORD=hunter2", "hunter2"},
		{"MCPX_HEADERS", "Authorization: Bearer abcdef123456", "abcdef123456"},
		{"MCPX_ANYTHING", "ghp_abcdefghijklmnopqrst", "ghp_abcdefghijklmnopqrst"},
		{"MCPX_ANYTHING", "sk-abcdefghijklmnopqrstuv", "sk-abcdefghijklmnopqrstuv"},
		{"MCPX_ANYTHING", "xoxb-1234567890-abcdef", "xoxb-1234567890-abcdef"},
		{"MCPX_ANYTHING", "AKIAIOSFODNN7EXAMPLE", "AKIAIOSFODNN7EXAMPLE"},
	}
	for _, c := range cases {
		got := logging.Redact(c.name, c.value)
		if strings.Contains(got, c.leaks) {
			t.Errorf("%s=%q leaked %q through as %q", c.name, c.value, c.leaks, got)
		}
	}
}

func TestRedactKeepsValuesWorthSeeing(t *testing.T) {
	// Over-eager elision makes the block useless; these are the values the
	// ambient env block exists to show.
	for _, c := range []struct{ name, value string }{
		{"MCPX_STATE_DIR", "/home/x/.local/state/mcpx"},
		{"MCPX_ENDPOINT", "http://127.0.0.1:54501"},
		{"MCPX_LOGGING_LEVEL", "debug"},
		{"MCPX_SCRIPT_ENV", "DEBUG=1"},
	} {
		if got := logging.Redact(c.name, c.value); got != c.value {
			t.Errorf("%s=%q should have been kept, got %q", c.name, c.value, got)
		}
	}
}

func TestRedactKeepsTheSurroundingContextOfAPartialMatch(t *testing.T) {
	// A variable holding several pairs is still worth seeing, minus the one
	// that matters.
	got := logging.Redact("MCPX_SCRIPT_ENV", "DEBUG=1 API_TOKEN=abcdef123456")
	if !strings.Contains(got, "DEBUG=1") {
		t.Errorf("the harmless pair should survive: %q", got)
	}
	if strings.Contains(got, "abcdef123456") {
		t.Errorf("the secret should not: %q", got)
	}
	if !strings.Contains(got, "<elided>") {
		t.Errorf("the elision should be visible: %q", got)
	}
}
