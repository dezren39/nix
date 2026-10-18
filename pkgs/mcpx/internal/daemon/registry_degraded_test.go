package daemon

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

// TestWarnDegradedNamesResolvedKey is the regression test for #191: the
// degraded-scope log line used to claim every scope "degraded to per-call",
// even when Scope.Key fell back to a directory (shared by every caller in
// it) or to the session key. The line must name the resolved key.
func TestWarnDegradedNamesResolvedKey(t *testing.T) {
	cases := []struct {
		name   string
		scope  config.Scope
		key    string
		reason string
	}{
		{"worktree to cwd", config.ScopeWorktree, "cwd:/work/bare.git", "no worktree: core.bare is true"},
		{"repo to cwd", config.ScopeRepo, "cwd:/work/plain", "not a git repository"},
		{"parent-session to session", config.ScopeParentSession, "session:s1", "no parent session id; using the session id"},
		{"session to per-call", config.ScopeSession, "call:c1", "no session id; set MCPX_SESSION_ID or pass --session"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var lines []string
			r := &Registry{logf: func(format string, args ...any) {
				lines = append(lines, fmt.Sprintf(format, args...))
			}}
			r.warnDegraded("bytree", tc.scope, tc.key, tc.reason)
			if len(lines) != 1 {
				t.Fatalf("logged %d lines, want 1: %v", len(lines), lines)
			}
			want := fmt.Sprintf("server %q: scope %q degraded to %s: %s", "bytree", tc.scope, tc.key, tc.reason)
			if lines[0] != want {
				t.Fatalf("log line = %q, want %q", lines[0], want)
			}
			if strings.Contains(lines[0], "per-call") && !strings.HasPrefix(tc.key, "call:") {
				t.Fatalf("log line claims per-call for a shared key: %q", lines[0])
			}
		})
	}
}

// TestWarnDegradedDedupesPerResolvedKey: the same degradation warns once,
// but the same reason resolving to a different key is a different
// degradation and must warn again — the two directories are not shared.
func TestWarnDegradedDedupesPerResolvedKey(t *testing.T) {
	var lines []string
	r := &Registry{logf: func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	}}
	r.warnDegraded("bytree", config.ScopeWorktree, "cwd:/work/a", "no worktree")
	r.warnDegraded("bytree", config.ScopeWorktree, "cwd:/work/a", "no worktree")
	r.warnDegraded("bytree", config.ScopeWorktree, "cwd:/work/b", "no worktree")
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want 2 (one per distinct key): %v", len(lines), lines)
	}
}
