package settings_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/settings"
)

// TestDerivedNamesKeepAcronymsWhole pins how a path becomes a flag and a
// variable. The splitter used to start a word at every capital, so
// proto.askTTL was --proto-ask-t-t-l and MCPX_PROTO_ASK_T_T_L.
func TestDerivedNamesKeepAcronymsWhole(t *testing.T) {
	for _, c := range []struct{ path, flag, env string }{
		{"log.level", "log-level", "MCPX_LOG_LEVEL"},
		{"daemon.idleExit", "daemon-idle-exit", "MCPX_DAEMON_IDLE_EXIT"},
		{"proto.askTTL", "proto-ask-ttl", "MCPX_PROTO_ASK_TTL"},
		{"proto.stateTTL", "proto-state-ttl", "MCPX_PROTO_STATE_TTL"},
		{"daemon.leaseTTL", "daemon-lease-ttl", "MCPX_DAEMON_LEASE_TTL"},
		{"proto.serveMCP", "proto-serve-mcp", "MCPX_PROTO_SERVE_MCP"},
		// A run followed by a lower-case letter: its last capital starts
		// the next word.
		{"x.HTTPTimeout", "x-http-timeout", "MCPX_X_HTTP_TIMEOUT"},
		{"x.fooHTTPBar", "x-foo-http-bar", "MCPX_X_FOO_HTTP_BAR"},
		{"x.mcpServer", "x-mcp-server", "MCPX_X_MCP_SERVER"},
		{"x.aB", "x-a-b", "MCPX_X_A_B"},
		// Digits belong to the word they follow; a capital after one starts
		// a new word.
		{"x.oauth2Scope", "x-oauth2-scope", "MCPX_X_OAUTH2_SCOPE"},
		{"x.http2", "x-http2", "MCPX_X_HTTP2"},
		{"x.maxMB2", "x-max-mb2", "MCPX_X_MAX_MB2"},
		{"x.v2API", "x-v2-api", "MCPX_X_V2_API"},
	} {
		s := settings.Setting{Path: c.path}
		if got := s.FlagName(); got != c.flag {
			t.Errorf("%s: flag %q, want %q", c.path, got, c.flag)
		}
		if got := s.EnvName(); got != c.env {
			t.Errorf("%s: variable %q, want %q", c.path, got, c.env)
		}
	}
}

// singleLetterWord finds a one-letter word in a dash- or underscore-separated
// name -- the signature of an acronym split into letters.
var singleLetterWord = regexp.MustCompile(`(^|[-_])[a-zA-Z]([-_]|$)`)

// TestNoDeclaredNameHasASingleLetterWord is the guard for the next acronym.
// No setting in the registry is named with a one-letter word, so one that
// appears is a splitter that has cut a TTL, an MCP or an ID into letters.
func TestNoDeclaredNameHasASingleLetterWord(t *testing.T) {
	for _, set := range settings.Registry() {
		flag := set.FlagName()
		env := strings.TrimPrefix(set.EnvName(), "MCPX_")
		for _, n := range []string{flag, env} {
			if singleLetterWord.MatchString(n) {
				t.Errorf("%s derives %q, which has a one-letter word; "+
					"an acronym in the path has been split into letters",
					set.Path, n)
			}
		}
	}
}
