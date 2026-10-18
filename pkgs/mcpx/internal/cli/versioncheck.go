package cli

import (
	"context"
	"fmt"
)

// daemonVersionNotice is the one line printed when the daemon a command
// connected to reports a different version than this CLI (#344).
//
// After an upgrade, a daemon started by the old binary keeps running and
// the new CLI talks to it; nothing compared the two, so a command or field
// the old daemon lacks failed confusingly instead of saying why. The
// notice names both versions and the restart that fixes it.
//
// Empty when there is nothing to say: equal versions, or either side not
// reporting one (a daemon older than the health version field is not
// evidence of a mismatch). A remote daemon cannot be restarted from here,
// so its line says to restart it where it runs.
func daemonVersionNotice(cliVersion, daemonVersion string, remote bool) string {
	if cliVersion == "" || daemonVersion == "" || cliVersion == daemonVersion {
		return ""
	}
	if remote {
		return fmt.Sprintf("daemon is version %s, this CLI is %s; restart the daemon where it runs to match",
			daemonVersion, cliVersion)
	}
	return fmt.Sprintf("daemon is version %s, this CLI is %s; restart it with `mcpx restart --daemon` (a service-managed daemon restarts itself after `mcpx stop`)",
		daemonVersion, cliVersion)
}

// daemonVersionWarning asks the connected daemon for its version and
// returns the mismatch notice, or "" when the versions agree, the daemon
// reports none, or the question cannot be asked. A failed health read is
// not worth failing the command over: the daemon just answered the
// connection ladder, so this stays best-effort.
func (a *App) daemonVersionWarning(ctx context.Context, c *Client) string {
	dv, err := c.DaemonVersion(ctx)
	if err != nil {
		return ""
	}
	return daemonVersionNotice(a.Version, dv, c.Remote())
}

// checkDaemonVersion warns once per command when the ladder reached a
// daemon this process did not start. A spawned or in-process daemon runs
// this same binary by construction, so only the socket and url rungs --
// a daemon that was already running, possibly from before an upgrade --
// can disagree.
func (a *App) checkDaemonVersion(ctx context.Context, c *Client, ladder *Ladder) {
	if ladder == nil || (ladder.Chosen != "socket" && ladder.Chosen != "url") {
		return
	}
	if msg := a.daemonVersionWarning(ctx, c); msg != "" {
		a.notice(msg)
	}
}
