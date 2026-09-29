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
	s = append(s, pathSettings()...)
	s = append(s, daemonSettings()...)
	s = append(s, outputSettings()...)
	s = append(s, plumbingSettings()...)
	return s
}

func poolSettings() []Setting {
	return []Setting{
		{
			Path: "pool.max", Kind: KindInt, Default: "4",
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Name:     "Maximum instances",
			Short:    "how many copies of one server may run at once",
			Long: "A shared server is reused by every caller, so the ceiling only " +
				"matters for exclusive ones. Raising it trades memory for parallelism.",
		},
		{
			Path: "pool.min", Kind: KindInt, Default: "0",
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Name:     "Warm instances",
			Short:    "how many copies to keep started even when idle",
			Long: "Above zero, that many instances survive the idle timeout. This is " +
				"the knob for a server whose startup is slow enough to notice.",
		},
		{
			Path: "pool.idleTimeout", Kind: KindDuration, Default: "5m",
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Name:     "Idle timeout", Short: "how long an unused server lingers before it is stopped",
		},
		{
			Path: "pool.callTimeout", Kind: KindDuration, Default: "120s",
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Name:     "Call timeout", Short: "how long one tool call may take",
		},
		{
			Path: "pool.startTimeout", Kind: KindDuration, Default: "60s",
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Name:     "Start timeout", Short: "how long a server has to become ready",
		},
		{
			Path: "pool.sharing", Kind: KindEnum, Default: "shared",
			Commands: []string{"run", "exec", "call", "daemon", "status", "restart"},
			Enum:     []string{"shared", "exclusive"},
			Name:     "Sharing", Short: "whether callers reuse one instance or each get their own",
		},
		{
			Path: "pool.scope", Kind: KindEnum, Default: "global",
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
			Enum:        []string{"text", "json", "json-pretty", "logfmt", "compact", "bare"},
			FlagAliases: []string{"format"},
			Name:        "Log format", Short: "how records are rendered",
		},
		{
			Path: "logging.level", Kind: KindEnum, Default: "info",
			Enum:        []string{"debug", "info", "warn", "error"},
			FlagAliases: []string{"log-level"},
			EnvAliases:  []string{"MCPX_LOG_LEVEL"},
			Name:        "Log level", Short: "the lowest level that is kept",
		},
		{
			Path: "logging.source", Kind: KindEnum, Default: "warn",
			Enum: []string{"none", "debug", "info", "warn", "error", "all"},
			Bare: "all", FlagAliases: []string{"log-source"},
			Name:  "Source capture",
			Short: "from which level upward to record the calling file and line",
			Long: "Capturing a stack costs roughly fifty times what emitting a record " +
				"costs, so it is worth paying only where someone will read it.",
		},
		{
			Path: "logging.dir", Kind: KindString, Default: "",
			FlagAliases: []string{"log-dir"},
			Name:        "Log directory", Short: "where the JSONL files are written",
			Long: "Empty means the state directory. The files are the durable record; " +
				"what appears on a terminal is a rendering of them.",
		},
		{
			Path: "logging.include", Kind: KindList, Default: "host,user,process,version",
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
			Name: "Rotate at size", Short: "roll the log file once it reaches this size",
		},
		{
			Path: "logging.maxLines", Kind: KindInt, Default: "0",
			Name: "Rotate at lines", Short: "roll the log file once it holds this many records",
			Long: "Zero disables the check. Size is usually the better trigger, but a " +
				"line ceiling is predictable in a way bytes are not when record " +
				"width varies wildly.",
		},
		{
			Path: "logging.maxAge", Kind: KindDuration, Default: "24h",
			Name: "Rotate at age", Short: "roll the log file once it is this old",
		},
		{
			Path: "logging.keep", Kind: KindInt, Default: "8",
			FlagAliases: []string{"keep"},
			Name:        "Retention", Short: "how many rolled files to keep",
		},
		{
			Path: "logging.file", Kind: KindBool, Default: "true",
			Name: "Write files", Short: "whether the durable JSONL log is written at all",
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
			Path: "script.runtime", Commands: runCommands, Kind: KindEnum, Default: "auto",
			Enum:        []string{"auto", "deno", "bun", "node"},
			FlagAliases: []string{"runtime"},
			Name:        "Runtime", Short: "which JavaScript runtime executes the script",
		},
		{
			Path: "script.permissions", Commands: runCommands, Kind: KindString, Default: "all",
			FlagAliases: []string{"permissions"},
			Name:        "Permissions", Short: "the sandbox profile, or raw runtime flags",
			Long: "One of all, net, read, read-net, strict, or flags passed through " +
				"verbatim. The default is wide open because the scripts are yours.",
		},
		{
			Path: "script.captureConsole", Commands: runCommands, Kind: KindBool, Default: "true",
			Name:  "Capture console",
			Short: "route console calls into the log",
			Long: "When on, console.info and friends become records. console.log still " +
				"reaches stdout, because stdout is the script's result. Raw writes " +
				"through Deno.stdout.write are never captured; a script asking for " +
				"bytes gets bytes.",
		},
		{
			Path: "script.launcher", Commands: runCommands, Kind: KindSource, Default: "",
			Name:  "Launcher",
			Short: "replace the generated launcher entirely",
			Long: "Given source or a file, that becomes the launcher, with @entry, " +
				"@globals, @before, @prefix, @onSuccess, @onError and @suffix " +
				"substituted. Given bare, there is no launcher at all and the " +
				"script is handed to the runtime untouched. " + srcLong,
			Bare:     "none",
			AllowDir: false,
		},
		{
			Path: "script.before", Commands: runCommands, Kind: KindSource, Default: "", Repeatable: true,
			Name: "Before phase", Short: "runs first, ahead of the globals being installed",
			Long: srcLong, AllowDir: false,
		},
		{
			Path: "script.prefix", Commands: runCommands, Kind: KindSource, Default: "", Repeatable: true,
			Name: "Prefix phase", Short: "runs after globals are installed, before the script",
			Long: srcLong, AllowDir: false,
		},
		{
			Path: "script.onSuccess", Commands: runCommands, Kind: KindSource, Default: "", Repeatable: true,
			Name: "On success", Short: "runs when the script returns without throwing",
			Long: srcLong, AllowDir: false,
		},
		{
			Path: "script.onError", Commands: runCommands, Kind: KindSource, Default: "", Repeatable: true,
			Name:  "On error",
			Short: "runs when the script throws; the error still propagates",
			Long: "A hook, not a handler. The error is rethrown afterwards, so the exit " +
				"status still reflects what happened. " + srcLong,
			AllowDir: false,
		},
		{
			Path: "script.suffix", Commands: runCommands, Kind: KindSource, Default: "", Repeatable: true,
			Name: "Suffix phase", Short: "runs last on both paths, like a finally",
			Long: srcLong, AllowDir: false,
		},
		{
			Path: "script.typecheck", Commands: runCommands, Kind: KindEnum, Default: "off",
			Enum:  []string{"off", "on", "strict"},
			Name:  "Type check",
			Short: "check the generated program before running it",
			Long: "Resolves and checks every import without executing anything. Costs " +
				"a second or two on a cold module cache, which is why it is off by " +
				"default rather than on.",
		},
		{
			Path: "script.env", Commands: runCommands, Kind: KindList, Default: "", Repeatable: true,
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
			Name: "Config search path", Short: "where configuration files are looked for",
			Long: spliceLong,
		},
		{
			Path: "paths.scripts", Kind: KindPathList, Default: "", Repeatable: true,
			Name: "Script search path", Short: "where named scripts are looked for",
			Long: spliceLong,
		},
		{
			Path: "paths.placeholders", Kind: KindPathList, Default: "", Repeatable: true,
			Name:  "Placeholder search path",
			Short: "directories of files declaring launcher placeholders",
			Long: "A file may declare the @name it provides, with a " +
				"// @mcpx:placeholder comment, an exported MCPX_PLACEHOLDER " +
				"constant, or by its filename. Declaring one makes it " +
				"addressable from a launcher template. " + spliceLong,
		},
		{
			Path: "paths.adapters", Kind: KindPathList, Default: "", Repeatable: true,
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
			EnvAliases: []string{"MCPX_STATE_DIR"},
			Name:       "State directory", Short: "where the daemon socket, logs and index live",
		},
		{
			Path: "paths.cache", Kind: KindString, Default: "",
			EnvAliases: []string{"MCPX_CACHE_DIR"},
			Name:       "Cache directory", Short: "where generated clients and schemas are kept",
		},
	}
}

func daemonSettings() []Setting {
	return []Setting{
		{
			Path: "daemon.reapInterval", Kind: KindDuration, Default: "30s",
			Plumbing: true,
			Name:     "Reap interval", Short: "how often idle instances are swept",
		},
		{
			Path: "daemon.saveInterval", Kind: KindDuration, Default: "5m",
			Plumbing: true,
			Name:     "Save interval", Short: "how often daemon state is written to disk",
		},
		{
			Path: "daemon.port", Kind: KindInt, Default: "0",
			Commands:    []string{"daemon", "status"},
			FlagAliases: []string{"port"},
			Name:        "Port", Short: "listen on a TCP port instead of choosing one",
		},
		{
			Path: "daemon.endpoint", Kind: KindString, Default: "",
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
			Name:  "MCP page size",
			Short: "how many items one tools/list reply carries",
			Long: "mcpx fronts every tool of every configured server, and a client " +
				"with a frame limit has no other way to read the list than to page " +
				"through it.",
			Commands: []string{"serve"},
		},
		{
			Path: "output.json", Kind: KindBool, Default: "false",
			FlagAliases: []string{"json"},
			Name:        "JSON output", Short: "emit one machine-readable document",
		},
		{
			Path: "output.color", Kind: KindEnum, Default: "auto",
			Enum: []string{"auto", "always", "never"},
			Name: "Colour", Short: "whether to colourise terminal output",
		},
		{
			Path: "catalog.budget", Kind: KindInt, Default: "2000",
			Commands:    []string{"catalog", "types", "ls", "search"},
			FlagAliases: []string{"budget"},
			Name:        "Catalog budget", Short: "token ceiling for the catalog listing",
		},
		{
			Path: "catalog.bias", Kind: KindList, Default: "", Repeatable: true,
			Commands:    []string{"catalog", "search"},
			FlagAliases: []string{"bias"},
			Name:        "Catalog bias", Short: "words that pull matching tools toward the front",
		},
		{
			Path: "catalog.instructions", Kind: KindBool, Default: "true",
			Commands: []string{"catalog", "types", "ls"},
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
			Plumbing: true,
			Name:     "Allow .ts and .js side by side",
			Short:    "permit a script directory holding both foo.ts and foo.js",
			Long: "Off, that pair is an error, because which one runs is a coin flip " +
				"nobody should have to call. On, .ts wins and the .js is ignored.",
		},
		{
			Path: "plumbing.sourceDirRecursive", Kind: KindBool, Default: "false",
			Plumbing: true,
			Name:     "Recurse into source directories",
			Short:    "when a directory is given as source, descend into subdirectories",
		},
		{
			Path: "plumbing.sourceDirAllowed", Kind: KindBool, Default: "true",
			Plumbing: true,
			Name:     "Allow directories as source",
			Short:    "whether a directory may stand in for a source string at all",
		},
		{
			Path: "plumbing.sourceProbePaths", Kind: KindBool, Default: "true",
			Plumbing: true,
			Name:     "Probe for files",
			Short:    "treat a source argument that names an existing file as a file",
			Long: "Off, only the explicit @file: form reads from disk. Worth turning " +
				"off if you routinely pass one-word scripts that collide with " +
				"filenames in the working directory.",
		},
		{
			Path: "plumbing.strictUnknownKeys", Kind: KindBool, Default: "false",
			Plumbing: true,
			Name:     "Reject unknown config keys",
			Short:    "fail on a configuration key no setting claims",
			Long: "Off by default because the configuration file is shared with other " +
				"sections. On, a typo is an error rather than a warning.",
		},
		{
			Path: "plumbing.validatePaths", Kind: KindBool, Default: "true",
			Plumbing: true,
			Name:     "Check paths up front",
			Short:    "resolve and verify every referenced path before doing any work",
		},
		{
			Path: "plumbing.launcherPlaceholderRepeat", Kind: KindList, Default: "",
			Plumbing: true,
			Name:     "Placeholders that may repeat",
			Short:    "launcher placeholders permitted to resolve more than once",
			Long: "A placeholder used twice is normally an error, because the common " +
				"cause is a mistake. Name one here to allow it deliberately.",
		},
		{
			Path: "plumbing.indexOnQuery", Kind: KindBool, Default: "true",
			Plumbing: true,
			Name:     "Index on demand",
			Short:    "bring the log index up to date before answering a query",
		},
		{
			Path: "plumbing.consoleReleaseOnExit", Kind: KindBool, Default: "true",
			Plumbing: true,
			Name:     "Restore console at exit",
			Short:    "hand the original console back before the process ends",
		},
	}
}
