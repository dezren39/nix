package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

	case ScopeRepo:
		if dir, err := gitDir(ctx.Cwd, "--git-common-dir"); err == nil {
			return "repo:" + dir, ""
		}
		if ctx.Cwd != "" {
			return "cwd:" + canonical(ctx.Cwd), gitReason("repository")
		}
		return fallback(ctx), gitReason("repository") + " and cwd is unknown"

	case ScopeWorktree:
		if dir, err := gitDir(ctx.Cwd, "--show-toplevel"); err == nil {
			return "worktree:" + dir, ""
		}
		if ctx.Cwd != "" {
			return "cwd:" + canonical(ctx.Cwd), gitReason("worktree")
		}
		return fallback(ctx), gitReason("worktree") + " and cwd is unknown"

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

// gitDir caches git lookups; a cwd's repo does not change within a daemon's
// lifetime often enough to matter, and the alternative is forking git on every
// single tool call.
var (
	gitCacheMu sync.RWMutex
	gitCache   = map[string]string{}
)

func gitDir(cwd, flag string) (string, error) {
	if cwd == "" {
		return "", fmt.Errorf("no cwd")
	}
	ck := flag + "\x00" + cwd

	gitCacheMu.RLock()
	hit, ok := gitCache[ck]
	gitCacheMu.RUnlock()
	if ok {
		if hit == "" {
			return "", fmt.Errorf("not a git repository")
		}
		return hit, nil
	}

	// --path-format=absolute (git 2.31, 2021) saves absolutising by hand.
	// --git-common-dir otherwise answers relative to the caller's directory,
	// which would make one clone look like several.
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", flag)
	cmd.Dir = cwd
	out, err := cmd.Output()
	dir := strings.TrimSpace(string(out))
	if err != nil || dir == "" {
		// Older git, or no git at all: fall back to the relative answer and
		// absolutise it against the directory we asked from.
		cmd = exec.Command("git", "rev-parse", flag)
		cmd.Dir = cwd
		out, err = cmd.Output()
		dir = strings.TrimSpace(string(out))
		if dir != "" && !filepath.IsAbs(dir) {
			dir = filepath.Clean(filepath.Join(cwd, dir))
		}
	}
	if dir != "" {
		dir = canonical(dir)
	}
	if err != nil || dir == "" {
		gitCacheMu.Lock()
		gitCache[ck] = ""
		gitCacheMu.Unlock()
		return "", fmt.Errorf("not a git repository")
	}

	gitCacheMu.Lock()
	gitCache[ck] = dir
	gitCacheMu.Unlock()
	return dir, nil
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

// gitReason distinguishes "this is not a repository" from "git is not
// installed". They look identical from the exit status and need different
// fixes, so the message says which one happened.
func gitReason(what string) string {
	if _, err := exec.LookPath("git"); err != nil {
		return "git is not installed, so the " + what + " cannot be determined"
	}
	return "not a git " + what
}
