package cli

import (
	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/daemon"
)

// resolvePathsForConfig keys the daemon to whichever config this invocation
// will actually use.
//
// Without this, every repo on the machine would share one daemon: the first
// project to run a command would decide which MCP servers exist, and a project
// with its own .mcpx.json would silently get someone else's servers. Editing a
// config would be equally confusing, because the running daemon would keep
// serving the old one.
func (a *App) resolvePathsForConfig() (daemon.Paths, *config.Config) {
	base := a.Paths
	cfg, err := config.Load(a.ConfigPath)
	if err != nil || cfg == nil || len(cfg.Sources) == 0 {
		// No config, or an unreadable one: fall back to the unkeyed default so
		// that `mcpx init` and `mcpx --help` still work.
		return base, cfg
	}
	return base.ForConfig(daemon.FingerprintConfig(cfg.Sources)), cfg
}
