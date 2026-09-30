package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CallContext is what a caller knows about itself. The daemon resolves a
// server's Scope against this to decide which process the call belongs to.
//
// Cwd, PID and CallID are always available. SessionID and ParentSessionID are
// not discoverable by mcpx -- a subagent and its parent share a working
// directory and differ only in an identifier their host assigns -- so the
// caller supplies them, through MCPX_SESSION_ID / MCPX_PARENT_SESSION_ID or
// an explicit --session.
type CallContext struct {
	Cwd             string `json:"cwd,omitempty"`
	SessionID       string `json:"sessionId,omitempty"`
	ParentSessionID string `json:"parentSessionId,omitempty"`
	PID             int    `json:"pid,omitempty"`
	// CallID is unique per invocation and is the fallback whenever a more
	// specific identifier is missing, so an absent session degrades to
	// per-call isolation rather than to accidental sharing.
	CallID string `json:"callId,omitempty"`
	// Ephemeral marks a SessionID the caller minted for itself rather than
	// one its host assigned. Such a key can only ever have one user, so it is
	// safe to stop the instance the moment that caller exits instead of
	// waiting out an idle timer.
	Ephemeral bool `json:"ephemeral,omitempty"`
}

// CallerOwned reports whether a resolved key belongs solely to this caller and
// may be torn down when the caller finishes: a per-call key, or one built from
// an identity the caller minted for itself.
//
// Ephemeral says the *session id* is private, not that everything the caller
// touched is. Treating it as the latter stopped the shared global, repo and
// worktree instances at the end of every `mcpx exec` run without a session,
// so the next caller paid a cold start -- and a caller that had just been
// using one lost it.
func (c CallContext) CallerOwned(key string) bool {
	if strings.HasPrefix(key, "call:") {
		return true
	}
	if !c.Ephemeral {
		return false
	}
	return (c.SessionID != "" && key == "session:"+c.SessionID) ||
		(c.PID > 0 && key == "pid:"+strconv.Itoa(c.PID))
}

// Key resolves the instance key for a scope. The returned key is opaque; only
// equality matters. Degraded reports whether a more specific scope fell back
// to something weaker, which the daemon logs once so the cause is visible.
func (s Scope) Key(ctx CallContext) (key string, degraded string) {
	switch s {
	case ScopeGlobal:
		return "global", ""

	case ScopeCwd:
		if ctx.Cwd == "" {
			return fallback(ctx), "cwd is unknown"
		}
		return "cwd:" + canonical(ctx.Cwd), ""

	case ScopeRepo, ScopeWorktree:
		if ctx.Cwd == "" {
			return fallback(ctx), "cwd is unknown"
		}
		// A directory outside any repository -- or one whose repository
		// cannot be read -- keys by itself: narrower than the scope asked
		// for, never wider.
		w, _, err := locateGit(ctx.Cwd)
		switch {
		case err != nil:
			return "cwd:" + canonical(ctx.Cwd), err.Error()
		case s == ScopeRepo:
			return "repo:" + w.commonDir, ""
		case w.worktree != "":
			return "worktree:" + w.worktree, ""
		default:
			return "cwd:" + canonical(ctx.Cwd), "no worktree: " + w.noWorktree
		}

	case ScopeSession:
		if ctx.SessionID != "" {
			return "session:" + ctx.SessionID, ""
		}
		return fallback(ctx), "no session id; set MCPX_SESSION_ID or pass --session"

	case ScopeParentSession:
		if ctx.ParentSessionID != "" {
			return "psession:" + ctx.ParentSessionID, ""
		}
		if ctx.SessionID != "" {
			return "session:" + ctx.SessionID, "no parent session id; using the session id"
		}
		return fallback(ctx), "no parent or session id; set MCPX_PARENT_SESSION_ID"

	case ScopePid:
		if ctx.PID > 0 {
			return "pid:" + strconv.Itoa(ctx.PID), ""
		}
		return fallback(ctx), "caller pid is unknown"

	case ScopeCall:
		return fallback(ctx), ""
	}
	return fallback(ctx), fmt.Sprintf("unknown scope %q", s)
}

// fallback is per-call isolation: the safe direction to fail, since sharing a
// stateful process by accident corrupts results while over-isolating only
// costs a process.
func fallback(ctx CallContext) string {
	if ctx.CallID != "" {
		return "call:" + ctx.CallID
	}
	return "call:anonymous"
}

// WatchesPID reports whether this scope ties a process's lifetime to a caller.
func (s Scope) WatchesPID() bool { return s == ScopePid }

// PIDOf extracts the watched pid from a key produced by ScopePid.
func PIDOf(key string) (int, bool) {
	rest, ok := strings.CutPrefix(key, "pid:")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil
}

// canonical resolves a path for use as a key. Two callers naming one directory
// differently -- /tmp and /private/tmp on macOS, or any symlinked checkout --
// must land on the same process, so every path that becomes a key goes through
// here. The input is returned unchanged when it cannot be resolved, which is
// the right failure: an unresolvable path is still consistently itself.
func canonical(p string) string {
	if p == "" {
		return p
	}
	if !filepath.IsAbs(p) {
		if wd, err := os.Getwd(); err == nil {
			p = filepath.Join(wd, p)
		}
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}
