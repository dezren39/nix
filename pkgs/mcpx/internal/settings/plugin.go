package settings

import "github.com/dezren39/mcpx/internal/defaults"

// pluginSettings are read by the opencode plugin, not by this binary.
//
// They are declared here anyway, and that is the point of the package: a knob
// is a knob regardless of which process reads it, and a user asking "what
// will the plugin do" should get an answer from `mcpx settings` rather than
// from reading TypeScript. The scope says who acts on it, so nothing pretends
// the daemon will honour a change to plugin.bin.
//
// The environment names here are load-bearing. The plugin reads
// process.env directly -- it cannot call into Go to resolve a setting -- so
// these spellings are the contract between the two, and every one of them is
// the name the derivation produces from its path. That is deliberate: a
// hand-written Env override would be a second name to keep in step.
func pluginSettings() []Setting {
	return []Setting{
		{
			Path: "plugin.bin", Kind: KindString, Default: defaults.PluginBin,
			Scope: ScopePlugin,
			Name:  "mcpx binary", Short: "the executable the plugin invokes",
			Long: "Resolved on PATH unless it is a path. Point it at a build under " +
				"test to try one without installing it.",
		},
		{
			Path: "plugin.binArgs", Kind: KindList, Default: defaults.CSV(defaults.PluginBinArgs),
			Scope: ScopePlugin, Repeatable: true,
			Name:  "Prepended arguments",
			Short: "arguments put in front of every plugin invocation of mcpx",
			Long: "Whatever the plugin runs, these come first. The case this exists " +
				"for is --config or --profile: the plugin should reach the same " +
				"servers a person at the prompt does, and without this there is no " +
				"way to say so except by changing the environment of the editor.",
		},
		{
			Path: "plugin.backend", Kind: KindEnum, Default: defaults.PluginBackend,
			Enum:  []string{"auto", "v1", "cli"},
			Scope: ScopePlugin,
			Name:  "Backend", Short: "whether the plugin talks to the daemon or spawns the binary",
			Long: "The socket is roughly a hundred times cheaper than a spawn, which " +
				"matters for something on the path of every tool call, so auto " +
				"prefers it and falls back. v1 refuses to fall back, which is what " +
				"you want when measuring; cli never uses the socket, which is the " +
				"escape hatch if the daemon is the thing under suspicion.",
		},
		{
			Path: "plugin.discoveryRetry", Kind: KindDuration,
			Default: defaults.Str(defaults.PluginDiscoveryRetry),
			Scope:   ScopePlugin,
			Name:    "Discovery retry", Short: "how long the plugin waits before looking for mcpx again",
			Long: "After a failed lookup the plugin stops asking for a while, so an " +
				"editor without mcpx installed does not pay for a failed spawn on " +
				"every keystroke.",
		},
		{
			Path: "plugin.toolTiming", Kind: KindBool, Default: defaults.Flag(defaults.PluginToolTiming),
			Scope: ScopePlugin,
			Name:  "Record tool timing", Short: "write every tool outcome into mcpx's log",
			Long: "Turns the editor's tool calls into records `mcpx stats` can " +
				"aggregate, so one query covers what mcpx did and what the harness " +
				"did around it.",
		},
		{
			Path: "plugin.env", Kind: KindEnum, Default: defaults.PluginEnv,
			Enum:  []string{"minimal", "standard", "full"},
			Scope: ScopePlugin,
			Name:  "Injected environment", Short: "how much session context is put into each command's environment",
			Long: "Full gives a called tool everything the editor knows -- session, " +
				"parent session, worktree, harness version -- which is what makes " +
				"session-scoped pooling work. Minimal is for when that is more " +
				"than you want leaving the process.",
		},
		{
			Path: "plugin.instructions", Kind: KindBool, Default: defaults.Flag(defaults.PluginInstructions),
			Scope: ScopePlugin,
			Name:  "System prompt", Short: "add mcpx usage guidance to the system prompt",
		},
		{
			Path: "plugin.tools", Kind: KindBool, Default: defaults.Flag(defaults.PluginTools),
			Scope: ScopePlugin,
			Name:  "Offer tools", Short: "expose mcpx itself as tools the model can call",
		},
		{
			Path: "plugin.remember", Kind: KindEnum, Default: defaults.PluginRemember,
			Enum:  []string{"session", "until-gone", "indefinite"},
			Scope: ScopePlugin,
			Name:  "Remember the daemon", Short: "how long a chosen daemon stays chosen",
			Long: "The plugin walks a ladder to find a daemon and writes down what " +
				"it found. Session forgets at the end of the session, until-gone " +
				"keeps the choice while the socket exists, indefinite keeps it " +
				"until somebody changes it.",
		},
		{
			Path: "plugin.annotate", Kind: KindBool, Default: defaults.Flag(defaults.PluginAnnotate),
			Scope: ScopePlugin,
			Name:  "Annotate tool output", Short: "add mcpx's own notes to a tool result",
		},
		// The two below default to "auto" because the plugin works the answer
		// out at boot. The other spellings are the ones the plugin's truthy()
		// accepts; listing fewer would let a value the plugin honours make
		// every mcpx command refuse to start.
		{
			Path: "plugin.headless", Kind: KindEnum, Default: defaults.PluginHeadless,
			Enum:  pluginTriState,
			Scope: ScopePlugin,
			Name:  "Headless", Short: "suppress toasts, because nobody is looking at a screen",
			Long: "Auto decides from the process shape: `opencode tui` runs the " +
				"plugin in a worker and the interface on the main thread, while " +
				"run, serve and CI run it on the main thread with nothing attached.",
		},
		{
			Path: "plugin.daemonTools", Kind: KindEnum, Default: defaults.PluginDaemonTools,
			Enum:  pluginTriState,
			Scope: ScopePlugin,
			Name:  "Daemon tools", Short: "offer the tools that list and choose between daemons",
			Long: "Auto offers them when plugin.tools is on or when discovery found " +
				"more than one daemon and somebody may want to choose.",
		},
	}
}

var pluginTriState = []string{"auto", "true", "false", "1", "0", "yes", "no", "on", "off"}
