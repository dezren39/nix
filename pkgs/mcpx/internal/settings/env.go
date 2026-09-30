package settings

import "github.com/dezren39/mcpx/internal/defaults"

// envSettings are knobs that existed only as an MCPX_ variable some package
// read by hand (#172).
//
// Each worked, and none could be found: no configuration key, no flag, no
// row in `mcpx settings`, nothing in the generated documentation. The names
// are kept -- the tests and the harness already use them -- so each carries
// an explicit Env rather than the one its path would derive.
//
// Variables mcpx *writes* for a script to read are not here. They are not
// configuration, and they are declared in ScriptEnv.
func envSettings() []Setting {
	return []Setting{
		{
			Path: "paths.configFile", Kind: KindString, Default: "",
			Scope: ScopeClient, Bootstrap: true,
			Env: "MCPX_CONFIG", Flag: "config",
			Name:  "Configuration file",
			Short: "read exactly this configuration file instead of searching",
			Long: "Every other setting can come from a configuration file, so this " +
				"one cannot: it decides which file that is. The environment and " +
				"the global --config flag (given before the command) are the only " +
				"ways to set it, and a configuration file naming it is refused. " +
				"Unset, mcpx merges every file on the search path, nearest first.",
		},
		{
			Path: "logging.trace", Kind: KindBool, Default: defaults.Flag(defaults.LogTrace),
			Scope: ScopeDaemon,
			Env:   "MCPX_TRACE",
			Name:  "Trace tool calls",
			Short: "log which instance served every tool call",
			Long: "One line per call in the daemon's log, naming the server, the " +
				"tool, the session and the instance and pid that answered. It is " +
				"the quickest way to see whether two callers really shared a " +
				"process or were given one each. Read when the daemon starts.",
		},
	}
}
