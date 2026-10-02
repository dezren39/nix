package settings

// Registry is every setting mcpx has.
//
// One entry here makes a knob readable from a configuration file, an
// environment variable and a flag. There is no second place to update, which
// is the whole point: the previous arrangement had four, and they drifted.
//
// Defaults are written as strings in the syntax a user would type, so the
// default goes through the same parser as an override. A default that skips
// validation is a default that can be invalid, and that failure surfaces on
// someone else's machine.
func Registry() []Setting {
	var s []Setting
	s = append(s, poolSettings()...)
	s = append(s, loggingSettings()...)
	s = append(s, scriptSettings()...)
	s = append(s, execSettings()...)
	s = append(s, pathSettings()...)
	s = append(s, daemonSettings()...)
	s = append(s, outputSettings()...)
	s = append(s, protoSettings()...)
	s = append(s, transportSettings()...)
	s = append(s, plumbingSettings()...)
	s = append(s, consumerSettings()...)
	s = append(s, wireSettings()...)
	s = append(s, pluginSettings()...)
	s = append(s, protoMessagesSettings()...)
	s = append(s, protoTasksSettings()...)
	s = append(s, upstreamSettings()...)
	s = append(s, envSettings()...)
	s = append(s, presetSettings()...)
	s = append(s, specSettings()...)
	return s
}

// presetSettings are named bundles of mcpx's own flags. They are applied as
// their own layer, above the environment and below the command line, so an
// explicit flag always beats a preset and `mcpx settings` can say which
// preset a value came from.
func presetSettings() []Setting {
	return []Setting{
		{
			Path: "presets", Kind: KindString, Default: "",
			Scope: ScopeClient,
			Name:  "Preset definitions",
			Short: "named flag bundles: {name: [\"--json\", \"--timeout=30s\"]}",
			Long: "A JSON object of preset name to a list of mcpx flags. A preset " +
				"applies those of its flags the running command accepts; the rest " +
				"are skipped, because one preset serves several commands. Positional " +
				"arguments are not allowed.",
		},
		{
			Path: "preset", Kind: KindList, Default: "",
			Scope: ScopeClient,
			Name:  "Presets",
			Short: "which presets to apply, in order",
			Long: "Later presets win over earlier ones; any flag given on the command " +
				"line wins over every preset.",
		},
	}
}

func poolSettings() []Setting {
	return []Setting{
		{
			Path: "pool.max", Kind: KindInt, Default: "4",
			Scope: ScopeDaemon, Hot: true,
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Name:     "Maximum instances",
			Short:    "how many copies of one server may run at once",
			Long: "A shared server is reused by every caller, so the ceiling only " +
				"matters for exclusive ones. Raising it trades memory for parallelism.",
		},
		{
			Path: "pool.min", Kind: KindInt, Default: "0",
			Scope: ScopeDaemon, Hot: true,
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Name:     "Warm instances",
			Short:    "how many copies to keep started even when idle",
			Long: "Above zero, that many instances survive the idle timeout. This is " +
				"the knob for a server whose startup is slow enough to notice.",
		},
		{
			Path: "pool.idleTimeout", Kind: KindDuration, Default: "5m",
			Scope: ScopeDaemon, Hot: true,
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Name:     "Idle timeout", Short: "how long an unused server lingers before it is stopped",
		},
		{
			Path: "pool.callTimeout", Kind: KindDuration, Default: "120s",
			Scope: ScopeDaemon, Hot: true,
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Name:     "Call timeout", Short: "how long one tool call may take",
		},
		{
			Path: "pool.startTimeout", Kind: KindDuration, Default: "60s",
			Scope: ScopeDaemon, Hot: true,
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Name:     "Start timeout", Short: "how long a server has to become ready",
		},
		{
			Path: "pool.sharing", Kind: KindEnum, Default: "shared",
			Scope:    ScopeDaemon,
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Enum:     []string{"shared", "exclusive"},
			Name:     "Sharing", Short: "whether callers reuse one instance or each get their own",
		},
		{
			Path: "pool.scope", Kind: KindEnum, Default: "global",
			Scope:    ScopeDaemon,
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Enum:     []string{"global", "repo", "worktree", "cwd", "session", "parent-session", "pid", "call"},
			Name:     "Scope", Short: "what counts as the same caller for sharing purposes",
		},
	}
}

func loggingSettings() []Setting {
	return []Setting{
		{
			Path: "logging.format", Kind: KindEnum, Default: "text",
			Scope:       ScopeClient,
			Enum:        []string{"text", "json", "json-pretty", "logfmt", "compact", "bare"},
			FlagAliases: []string{"format"},
			Name:        "Log format", Short: "how records are rendered",
		},
		{
			Path: "logging.level", Kind: KindEnum, Default: "info",
			Scope: ScopeDaemon, Hot: true,
			Enum:        []string{"debug", "info", "warn", "error"},
			FlagAliases: []string{"log-level"},
			EnvAliases:  []string{"MCPX_LOG_LEVEL"},
			Name:        "Log level", Short: "the lowest level that is kept",
		},
		{
			Path: "logging.source", Kind: KindEnum, Default: "warn",
			Scope: ScopeDaemon, Hot: true,
			Enum: []string{"none", "debug", "info", "warn", "error", "all"},
			Bare: "all", FlagAliases: []string{"log-source"},
			Name:  "Source capture",
			Short: "from which level upward to record the calling file and line",
			Long: "Capturing a stack costs roughly fifty times what emitting a record " +
				"costs, so it is worth paying only where someone will read it.",
		},
		{
			Path: "logging.dir", Kind: KindString, Default: "",
			Scope:       ScopeDaemon,
			FlagAliases: []string{"log-dir"},
			Name:        "Log directory", Short: "where the JSONL files are written",
			Long: "Empty means the state directory. The files are the durable record; " +
				"what appears on a terminal is a rendering of them.",
		},
		{
			Path: "logging.include", Kind: KindList, Default: "host,user,process,version",
			Scope:       ScopeDaemon,
			FlagAliases: []string{"include"},
			Repeatable:  true,
			Name:        "Ambient blocks",
			Short:       "which context blocks are attached to lifecycle records",
			Long: "One of host, user, process, network, version, env; or all, or none. " +
				"Network is off by default because enumerating interfaces costs " +
				"milliseconds and rarely answers a question anyone asked.",
		},
		{
			Path: "logging.maxBytes", Kind: KindBytes, Default: "16MB",
			Scope: ScopeDaemon,
			Name:  "Rotate at size", Short: "roll the log file once it reaches this size",
		},
		{
			Path: "logging.maxLines", Kind: KindInt, Default: "0",
			Scope: ScopeDaemon,
			Name:  "Rotate at lines", Short: "roll the log file once it holds this many records",
			Long: "Zero disables the check. Size is usually the better trigger, but a " +
				"line ceiling is predictable in a way bytes are not when record " +
				"width varies wildly.",
		},
		{
			Path: "logging.maxAge", Kind: KindDuration, Default: "24h",
			Scope: ScopeDaemon,
			Name:  "Rotate at age", Short: "roll the log file once it is this old",
		},
		{
			Path: "logging.keep", Kind: KindInt, Default: "8",
			Scope:       ScopeDaemon,
			FlagAliases: []string{"keep"},
			Name:        "Retention", Short: "how many rolled files to keep",
		},
		{
			Path: "logging.file", Kind: KindBool, Default: "true",
			Scope: ScopeDaemon,
			Name:  "Write files", Short: "whether the durable JSONL log is written at all",
		},
	}
}

// runCommands are the commands that execute user code. The script settings
// only appear in their help, because a flag offered where it does nothing is
// worse than one that is missing: it implies an effect.
var runCommands = []string{"run", "exec"}

func scriptSettings() []Setting {
	srcLong := "Accepts inline source, a path to a file, or -- where permitted -- a " +
		"directory whose files are concatenated in natural order. An argument " +
		"that resolves to an existing path is treated as one; prefix with " +
		"@text: or @file: to say which you meant."

	return []Setting{
		{
			Path: "script.runtime", Commands: runCommands, Kind: KindString, Default: "auto",
			Scope:       ScopeClient,
			FlagAliases: []string{"runtime"},
			Name:        "Runtime", Short: "which JavaScript runtime executes the script",
			Long: "auto tries script.runtimeOrder. Otherwise deno, bun or node; a name " +
				"declared under script.runtimes; or a path or binary name whose " +
				"basename is one of the three. See docs/runtimes.md.",
		},
		{
			Path: "script.runtimes", Commands: runCommands, Kind: KindString, Default: "",
			Scope: ScopeClient,
			Name:  "Declared runtimes",
			Short: "named runtime binaries: {name: {kind, bin, args}}",
			Long: "A JSON object. kind is deno, bun or node and decides how the " +
				"command line is built; bin is a path or a name on PATH; args are " +
				"runtime options placed before the permission flags.",
		},
		{
			Path: "script.runtimeOrder", Commands: runCommands, Kind: KindList, Default: "deno,bun,node",
			Scope: ScopeClient,
			Name:  "Runtime order", Short: "the runtimes auto tries, in order",
			Long: "Entries are anything script.runtime accepts. A runtime that cannot " +
				"enforce the permissions asked for is skipped rather than used.",
		},
		{
			Path: "script.permissions", Commands: runCommands, Kind: KindString, Default: "all",
			Scope:       ScopeClient,
			FlagAliases: []string{"permissions"},
			EnvAliases:  []string{"MCPX_PERMISSIONS"},
			Name:        "Permissions", Short: "permission profiles, composed in order, or raw: flags",
			Long: "A comma-separated list of profile names applied in order: all, net, " +
				"read, readnet, strict, or one defined in script.profiles. raw: " +
				"introduces flags passed through verbatim and takes the rest of the " +
				"value. The default is wide open because the scripts are yours.",
		},
		{
			Path: "script.profiles", Commands: runCommands, Kind: KindString, Default: "",
			Scope: ScopeClient,
			Name:  "Permission profiles",
			Short: "user profiles: {name: {deno: [flags], node: [flags]}} or {name: \"raw flags\"}",
			Long: "Each replaces a built-in of the same name. A runtime kind a profile " +
				"does not list cannot run under it.",
		},
		{
			Path: "script.captureConsole", Commands: runCommands, Kind: KindBool, Default: "true",
			Scope: ScopeClient,
			Name:  "Capture console",
			Short: "route console calls into the log",
			Long: "When on, console.info and friends become records. console.log still " +
				"reaches stdout, because stdout is the script's result. Raw writes " +
				"through Deno.stdout.write are never captured; a script asking for " +
				"bytes gets bytes.",
		},
		{
			Path: "script.launcher", Commands: runCommands, Kind: KindSource, Default: "",
			Scope: ScopeClient,
			Name:  "Launcher",
			Short: "replace the generated launcher entirely",
			Long: "Given source or a file, that becomes the launcher, with @entry, " +
				"@globals, @before, @prefix, @onSuccess, @onError and @suffix " +
				"substituted. Given bare, there is no launcher at all and the " +
				"script is handed to the runtime untouched. " + srcLong,
			Bare: "none",
		},
		{
			Path: "script.before", Commands: runCommands, Kind: KindSource, Default: "", Repeatable: true,
			Scope: ScopeClient,
			Name:  "Before phase", Short: "runs first, ahead of the globals being installed",
			Long: srcLong,
		},
		{
			Path: "script.prefix", Commands: runCommands, Kind: KindSource, Default: "", Repeatable: true,
			Scope: ScopeClient,
			Name:  "Prefix phase", Short: "runs after globals are installed, before the script",
			Long: srcLong,
		},
		{
			Path: "script.onSuccess", Commands: runCommands, Kind: KindSource, Default: "", Repeatable: true,
			Scope: ScopeClient,
			Name:  "On success", Short: "runs when the script returns without throwing",
			Long: srcLong,
		},
		{
			Path: "script.onError", Commands: runCommands, Kind: KindSource, Default: "", Repeatable: true,
			Scope: ScopeClient,
			Name:  "On error",
			Short: "runs when the script throws; the error still propagates",
			Long: "A hook, not a handler. The error is rethrown afterwards, so the exit " +
				"status still reflects what happened. " + srcLong,
		},
		{
			Path: "script.suffix", Commands: runCommands, Kind: KindSource, Default: "", Repeatable: true,
			Scope: ScopeClient,
			Name:  "Suffix phase", Short: "runs last on both paths, like a finally",
			Long: srcLong,
		},
		{
			Path: "script.typecheck", Commands: runCommands, Kind: KindEnum, Default: "off",
			Scope: ScopeClient,
			Enum:  []string{"off", "on", "strict"},
			Name:  "Type check",
			Short: "check the generated program before running it",
			Long: "Resolves and checks every import without executing anything. Costs " +
				"a second or two on a cold module cache, which is why it is off by " +
				"default rather than on.",
		},
		{
			Path: "script.env", Commands: runCommands, Kind: KindList, Default: "", Repeatable: true,
			Scope:       ScopeClient,
			FlagAliases: []string{"env"},
			Name:        "Extra environment", Short: "KEY=VALUE pairs added to the script's environment",
		},
	}
}

func pathSettings() []Setting {
	spliceLong := "A list. A null entry stands for whatever the layer below provided, " +
		"so [\"./mine\", null] searches yours first and then the usual places. " +
		"Without a null the list replaces outright. An entry naming a file " +
		"rather than a directory means exactly that file."

	return []Setting{
		{
			Path: "paths.config", Kind: KindPathList, Default: "", Repeatable: true,
			Scope: ScopeClient,
			Name:  "Config search path", Short: "where configuration files are looked for",
			Long: spliceLong,
		},
		{
			Path: "paths.scripts", Kind: KindPathList, Default: "", Repeatable: true,
			Scope: ScopeClient,
			Name:  "Script search path", Short: "where named scripts are looked for",
			Long: spliceLong,
		},
		{
			Path: "paths.placeholders", Kind: KindPathList, Default: "", Repeatable: true,
			Scope: ScopeClient,
			Name:  "Placeholder search path",
			Short: "directories of files declaring launcher placeholders",
			Long: "A file may declare the @name it provides, with a " +
				"// @mcpx:placeholder comment, an exported MCPX_PLACEHOLDER " +
				"constant, or by its filename. Declaring one makes it " +
				"addressable from a launcher template. " + spliceLong,
		},
		{
			Path: "paths.adapters", Kind: KindPathList, Default: "", Repeatable: true,
			Scope: ScopeClient,
			Name:  "Adapter declarations",
			Short: "files declaring command-line programs as MCP servers",
			Long: "An enormous amount of capability already exists as command-line " +
				"programs, and writing a server to wrap one is a day's work that " +
				"produces a process whose only job is to shell out. A declaration " +
				"names the binary, the subcommands worth exposing and what each " +
				"takes. " + spliceLong,
		},
		{
			Path: "paths.apis", Kind: KindPathList, Default: "", Repeatable: true,
			Scope: ScopeClient,
			Name:  "OpenAPI declarations",
			Short: "files naming OpenAPI documents to expose as tools",
			Long: "An enormous amount of capability is already described by a " +
				"specification somebody else maintains. Turning one into tools is " +
				"a mechanical transformation, and a specification is a better " +
				"source than a hand-written wrapper because it is already correct " +
				"and it changes when the service does. " + spliceLong,
		},
		{
			Path: "paths.state", Kind: KindString, Default: "",
			Scope:      ScopeClient,
			EnvAliases: []string{"MCPX_STATE_DIR"},
			Name:       "State directory", Short: "where the daemon socket, logs and index live",
		},
		{
			Path: "paths.cache", Kind: KindString, Default: "",
			Scope:      ScopeClient,
			EnvAliases: []string{"MCPX_CACHE_DIR"},
			Name:       "Cache directory", Short: "where generated clients and schemas are kept",
		},
	}
}

func daemonSettings() []Setting {
	return []Setting{
		{
			Path: "daemon.reapInterval", Kind: KindDuration, Default: "30s",
			Scope:    ScopeDaemon,
			Plumbing: true,
			Name:     "Reap interval", Short: "how often idle instances are swept",
		},
		{
			Path: "daemon.saveInterval", Kind: KindDuration, Default: "5m",
			Scope:    ScopeDaemon,
			Plumbing: true,
			Name:     "Save interval", Short: "how often daemon state is written to disk",
		},
		{
			Path: "daemon.port", Kind: KindInt, Default: "0",
			Scope:       ScopeDaemon,
			Commands:    []string{"daemon", "status"},
			FlagAliases: []string{"port"},
			Name:        "Port", Short: "listen on a TCP port instead of choosing one",
		},
		{
			Path: "daemon.endpoint", Kind: KindString, Default: "",
			Scope: ScopeClient,
			Name:  "Daemon endpoint",
			Short: "a daemon somewhere else, instead of the local socket",
			Long: "Empty uses the local unix socket, which is the fast path: no " +
				"network stack and filesystem permissions as the access control. " +
				"A URL points at a daemon on another machine -- one for a team, " +
				"one on a VPN -- and mcpx will not try to start that one, because " +
				"answering from a local daemon when a remote one is unreachable " +
				"would be worse than failing. unix:///path targets a different " +
				"socket on this machine.",
			Commands: []string{"run", "exec", "call", "ls", "types", "catalog",
				"search", "status", "client", "serve", "tui", "prompts", "resources"},
		},
		{
			Path: "daemon.address", Kind: KindString, Default: "127.0.0.1",
			Scope: ScopeDaemon, FlagAliases: []string{"address"},
			Name:  "Bind address",
			Short: "which interface the daemon listens on",
			Long: "Loopback by default. The API is unauthenticated, so the network " +
				"it is reachable from is the access control -- widening that has " +
				"to be a decision somebody made rather than a default they " +
				"inherited. 0.0.0.0 exposes it to everything that can route to " +
				"this host.",
			Commands: []string{"daemon"},
		},
		{
			Path: "daemon.inline", Kind: KindBool, Default: "false",
			Scope: ScopeClient,
			Name:  "Run without a daemon",
			Short: "as a last resort, run servers inside this process",
			Long: "The final rung of the connection ladder, tried only after an " +
				"existing daemon and a spawned one have both failed. Servers start " +
				"when first called and die when the command exits, so nothing is " +
				"pooled between commands and a stateful server -- a browser -- " +
				"cannot outlive one. Slower every time, but it works in a sandbox " +
				"with no fork, on a read-only filesystem, or in a container whose " +
				"init will not reap. Off by default because a pool that silently " +
				"stops pooling is a performance bug nobody can see.",
		},
		{
			Path: "daemon.autostart", Kind: KindBool, Default: "true",
			Scope: ScopeClient,
			Name:  "Autostart",
			Short: "start the daemon on demand when it is not running",
			Long: "With this off, a command that needs the daemon fails instead of " +
				"starting one. Useful when the daemon is run as a service and an " +
				"accidental second one would be confusing.",
		},
	}
}

func outputSettings() []Setting {
	return []Setting{
		{
			Path: "registry.url", Kind: KindString,
			Scope:   ScopeClient,
			Default: "https://registry.modelcontextprotocol.io",
			Name:    "Registry",
			Short:   "where `mcpx registry` looks for servers",
			Long: "The official MCP Registry publishes an OpenAPI specification that " +
				"other registries implement, so this can point at a vendor's " +
				"subregistry or one an organisation runs internally to control " +
				"what its agents can install.",
			Commands: []string{"registry"},
		},
		{
			Path: "mcp.pageSize", Kind: KindInt, Default: "100",
			Scope: ScopeClient,
			Name:  "MCP page size",
			Short: "how many items one tools/list reply carries",
			Long: "mcpx fronts every tool of every configured server, and a client " +
				"with a frame limit has no other way to read the list than to page " +
				"through it.",
			Commands: []string{"serve"},
		},
		{
			Path: "mcp.passthrough", Kind: KindString, Default: "",
			Scope:       ScopeClient,
			FlagAliases: []string{"passthrough"},
			Name:        "MCP pass-through",
			Short:       "serve upstreams' tools, prompts and resources under their own names",
			Long: "Names a configured server -- or several, comma-separated -- whose " +
				"surface /mcp and `mcpx serve` offer unrenamed: its tools by their own " +
				"names (listed ahead of mcpx's gateway tools), its prompts without the " +
				"<namespace>_ prefix, and its resources at their own URIs rather than " +
				"mcpx://<namespace>/<uri>. Results are its own, verbatim, images, " +
				"structured content and JSON-RPC errors included. On a name collision " +
				"with a gateway tool the upstream's tool wins and the gateway tool of " +
				"that name is not offered over MCP. With several upstreams their " +
				"surfaces are merged; a tool or prompt name two of them offer is refused " +
				"with an error naming both, and a bare resource URI goes to the first " +
				"(in the order given) that lists it. For a gateway whose clients see " +
				"its servers as themselves: pooling, logging and policy in front. Empty " +
				"(the default) namespaces everything.",
			Commands: []string{"serve", "daemon"},
		},
		{
			Path: "output.json", Kind: KindBool, Default: "false",
			Scope:       ScopeClient,
			FlagAliases: []string{"json"},
			Name:        "JSON output", Short: "emit one machine-readable document",
		},
		{
			Path: "output.color", Kind: KindEnum, Default: "auto",
			Scope:    ScopeClient,
			Commands: []string{"tui"},
			Enum:     []string{"auto", "always", "never"},
			Name:     "Colour", Short: "whether to colourise the browser",
			Long: "Only the browser draws in colour; everything else is plain " +
				"text on purpose, because the other commands are read by " +
				"programs as often as by people. Auto asks the terminal.",
		},
		{
			Path: "catalog.budget", Kind: KindInt, Default: "2000",
			Scope: ScopeCall, Hot: true,
			// Only `catalog` renders a budgeted listing. It was offered on
			// types, ls and search as well, where --budget parsed and did
			// nothing -- a flag offered where it has no effect implies one.
			Commands:    []string{"catalog"},
			FlagAliases: []string{"budget"},
			Name:        "Catalog budget", Short: "token ceiling for the catalog listing",
		},
		{
			Path: "catalog.bias", Kind: KindList, Default: "", Repeatable: true,
			Scope: ScopeCall, Hot: true,
			Commands:    []string{"catalog"},
			FlagAliases: []string{"bias"},
			Name:        "Catalog bias", Short: "words that pull matching tools toward the front",
		},
		{
			Path: "catalog.instructions", Kind: KindBool, Default: "true",
			Scope: ScopeCall, Hot: true,
			// `types` is the only output that carries instructions; ls
			// prints a table and catalog prints signatures.
			Commands: []string{"types"},
			Name:     "Server instructions", Short: "include each server's own instructions",
		},
	}
}

// plumbingSettings are internals. They work, and they are documented, but
// there is no ordinary reason to change one.
//
// Each exists because the code has a guard that could reasonably go either
// way. Rather than pick for everyone and leave the other half stuck, the guard
// reads a switch. The cost is a longer list; the benefit is that nobody has to
// patch the binary to get past a decision that was never meant to be final.
func plumbingSettings() []Setting {
	return []Setting{
		{
			Path: "plumbing.allowTsJsOverlap", Kind: KindBool, Default: "false",
			Scope:    ScopeClient,
			Plumbing: true,
			Name:     "Allow .ts and .js side by side",
			Short:    "permit a script directory holding both foo.ts and foo.js",
			Long: "Off, that pair is an error, because which one runs is a coin flip " +
				"nobody should have to call. On, .ts wins and the .js is ignored.",
		},
		{
			Path: "plumbing.sourceDirRecursive", Kind: KindBool, Default: "false",
			Scope:    ScopeClient,
			Plumbing: true,
			Name:     "Recurse into source directories",
			Short:    "when a directory is given as source, descend into subdirectories",
		},
		{
			Path: "plumbing.sourceDirAllowed", Kind: KindBool, Default: "true",
			Scope:    ScopeClient,
			Plumbing: true,
			Name:     "Allow directories as source",
			Short:    "whether a directory may stand in for a source string at all",
		},
		{
			Path: "plumbing.sourceProbePaths", Kind: KindBool, Default: "true",
			Scope:    ScopeClient,
			Plumbing: true,
			Name:     "Probe for files",
			Short:    "treat a source argument that names an existing file as a file",
			Long: "Off, only the explicit @file: form reads from disk. Worth turning " +
				"off if you routinely pass one-word scripts that collide with " +
				"filenames in the working directory.",
		},
		{
			Path: "plumbing.strictUnknownKeys", Kind: KindBool, Default: "false",
			Scope:    ScopeClient,
			Plumbing: true,
			Name:     "Reject unknown config keys",
			Short:    "fail on a configuration key no setting claims",
			Long: "Off by default because the configuration file is shared with other " +
				"sections. On, a typo is an error rather than a warning.",
		},
		{
			Path: "plumbing.validatePaths", Kind: KindBool, Default: "true",
			Scope:    ScopeClient,
			Plumbing: true,
			Name:     "Check paths up front",
			Short:    "resolve and verify every referenced path before doing any work",
		},
		{
			Path: "plumbing.launcherPlaceholderRepeat", Kind: KindList, Default: "",
			Scope:    ScopeClient,
			Plumbing: true,
			Name:     "Placeholders that may repeat",
			Short:    "launcher placeholders permitted to resolve more than once",
			Long: "A placeholder used twice is normally an error, because the common " +
				"cause is a mistake. Name one here to allow it deliberately.",
		},
		{
			Path: "plumbing.indexOnQuery", Kind: KindBool, Default: "true",
			Scope:    ScopeDaemon,
			Plumbing: true,
			Name:     "Index on demand",
			Short:    "bring the log index up to date before answering a query",
		},
	}
}
