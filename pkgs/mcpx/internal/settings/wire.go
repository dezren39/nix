package settings

import "github.com/dezren39/mcpx/internal/defaults"

// wireSettings are the numbers that used to be written where they were
// needed: a body limit beside one decoder, a ping interval beside one
// stream, an argument list beside the one call that spawns a daemon.
//
// None of them is a knob anybody asks for by name, and that is exactly why
// they are here. A constant that nobody can change is fine right up to the
// moment somebody needs it changed, and then it is a patched binary or a
// fork. Declaring them costs a line each and removes that outcome entirely;
// the ones that would be strange to touch are marked plumbing so they stay
// out of ordinary help.
//
// Defaults are read from internal/defaults/defaults.json rather than restated
// here. The registry and the embedded layer disagreeing about what a default
// is would be a bug with no symptom other than a number being wrong.
func wireSettings() []Setting {
	return append(append(append(
		httpSettings(),
		autostartSettings()...),
		limitSettings()...),
		serviceSettings()...)
}

func httpSettings() []Setting {
	return []Setting{
		{
			Path: "http.bodyLimit", Kind: KindBytes, Default: defaults.Bytes(defaults.HTTPBodyLimit),
			Scope: ScopeDaemon, Hot: true, Plumbing: true,
			Name: "Request body limit", Short: "the ceiling on an ordinary /v1 request body",
			Long: "Applies to every route that is not a tool call. A body over the " +
				"limit is refused rather than buffered, because an unbounded " +
				"decode is how one bad client takes the daemon down with it.",
		},
		{
			Path: "http.callBodyLimit", Kind: KindBytes, Default: defaults.Bytes(defaults.HTTPCallBodyLimit),
			Scope: ScopeDaemon, Hot: true,
			Name: "Tool call body limit", Short: "the ceiling on a POST /v1/call body",
			Long: "Far higher than the ordinary limit because tool arguments " +
				"legitimately carry documents. Raise it if a tool takes a large " +
				"file as an argument rather than as a path.",
		},
		{
			Path: "http.controlBodyLimit", Kind: KindBytes, Default: defaults.Bytes(defaults.HTTPControlBodyLimit),
			Scope: ScopeDaemon, Hot: true, Plumbing: true,
			Name: "Control body limit", Short: "the ceiling on a body that should hold one field",
		},
		{
			Path: "http.readHeaderTimeout", Kind: KindDuration, Default: defaults.Str(defaults.HTTPReadHeaderTimeout),
			Scope: ScopeDaemon, Plumbing: true,
			Name: "Header read timeout", Short: "how long a client has to finish sending its headers",
		},
		{
			Path: "http.shutdownGrace", Kind: KindDuration, Default: defaults.Str(defaults.HTTPShutdownGrace),
			Scope: ScopeDaemon, Hot: true,
			Name: "Shutdown grace", Short: "how long in-flight requests have when the daemon stops",
		},
		{
			Path: "http.ssePing", Kind: KindDuration, Default: defaults.Str(defaults.HTTPSSEPing),
			Scope: ScopeDaemon, Hot: true, Plumbing: true,
			Name: "Event stream keepalive", Short: "how often a comment is sent on an idle event stream",
			Long: "An idle proxy decides a quiet connection is dead. The default " +
				"sits under the usual sixty-second idle timeout; a proxy with a " +
				"shorter one needs this shorter still.",
		},
		{
			Path: "http.sseRetry", Kind: KindDuration, Default: defaults.Str(defaults.HTTPSSERetry),
			Scope: ScopeDaemon, Hot: true, Plumbing: true,
			Name: "Event stream retry hint", Short: "how long a dropped subscriber is told to wait",
		},
		{
			Path: "http.streamBufferInit", Kind: KindBytes, Default: defaults.Bytes(defaults.HTTPStreamBufferInit),
			Scope: ScopeClient, Plumbing: true,
			Name: "Stream buffer", Short: "the initial line buffer when reading an event stream",
		},
		{
			Path: "http.streamBufferMax", Kind: KindBytes, Default: defaults.Bytes(defaults.HTTPStreamBufferMax),
			Scope: ScopeClient, Plumbing: true,
			Name: "Stream line ceiling", Short: "the largest single event line that will be read",
			Long: "A line over this ends the stream. It is generous because an " +
				"event carrying a tool result can be large, and truncating one " +
				"silently would be worse than failing.",
		},
		{
			Path: "http.idleConns", Kind: KindInt, Default: defaults.Num(defaults.HTTPIdleConns),
			Scope: ScopeClient, Plumbing: true,
			Name: "Pooled connections", Short: "idle connections kept to a local daemon",
		},
		{
			Path: "http.remoteIdleConns", Kind: KindInt, Default: defaults.Num(defaults.HTTPRemoteIdleConns),
			Scope: ScopeClient, Plumbing: true,
			Name: "Pooled remote connections", Short: "idle connections kept to a daemon over the network",
			Long: "Higher than the local figure because a handshake over a VPN is " +
				"most of the latency of a call, and reusing a connection removes it.",
		},
		{
			Path: "http.requestTimeout", Kind: KindDuration, Default: defaults.Str(defaults.HTTPRequestTimeout),
			Scope: ScopeClient,
			Name:  "Client request timeout", Short: "how long a CLI request to the daemon may take",
			Long: "Long, because a tool call goes through it and a tool may " +
				"legitimately run for minutes. It bounds the client, not the call: " +
				"pool.callTimeout is what stops a runaway tool.",
		},
		{
			Path: "http.idleConnTimeout", Kind: KindDuration, Default: defaults.Str(defaults.HTTPIdleTimeout),
			Scope: ScopeClient, Plumbing: true,
			Name: "Idle connection timeout", Short: "how long an unused connection is kept",
		},
	}
}

func autostartSettings() []Setting {
	return []Setting{
		{
			Path: "autostart.bin", Kind: KindString, Default: defaults.AutostartBin,
			Scope: ScopeClient,
			Name:  "Daemon binary", Short: "which executable is started as the daemon",
			Long: "Empty means this binary, which is right almost always. It is a " +
				"setting because the running executable is not always the one that " +
				"should become a long-lived daemon: a wrapper script, a binary " +
				"about to be replaced by an upgrade, or a sandbox that can exec " +
				"one path and not another.",
		},
		{
			Path: "autostart.args", Kind: KindList, Default: defaults.CSV(defaults.AutostartArgs),
			Scope: ScopeClient, Plumbing: true, Repeatable: true,
			Name: "Daemon arguments", Short: "the arguments an auto-started daemon is given",
			Long: "The idle timeout and --config are appended to these, so a list " +
				"here replaces the subcommand and the detach flag rather than the " +
				"whole command line.",
		},
		{
			Path: "autostart.idleExit", Kind: KindDuration, Default: defaults.Str(defaults.AutostartIdleExit),
			Scope: ScopeClient,
			Name:  "Auto-started idle timeout",
			Short: "how long an auto-started daemon survives with nothing to do",
			Long: "A daemon nobody asked for should not outlive the reason it " +
				"started, or visiting many projects leaves a process behind in each. " +
				"Zero means never, which is what a daemon started deliberately gets.",
		},
		{
			Path: "autostart.connectTimeout", Kind: KindDuration, Default: defaults.Str(defaults.AutostartConnectTimeout),
			Scope: ScopeClient,
			Name:  "Startup wait", Short: "how long to wait for a started daemon to answer",
		},
		{
			Path: "autostart.pollInterval", Kind: KindDuration, Default: defaults.Str(defaults.AutostartPollInterval),
			Scope: ScopeClient, Plumbing: true,
			Name: "Startup poll", Short: "how often a starting daemon is probed",
		},
		{
			Path: "autostart.pingTimeout", Kind: KindDuration, Default: defaults.Str(defaults.AutostartPingTimeout),
			Scope: ScopeClient, Plumbing: true,
			Name: "Liveness timeout", Short: "how long a health check waits before calling it dead",
		},
		{
			Path: "autostart.logTail", Kind: KindBytes, Default: defaults.Bytes(defaults.AutostartLogTail),
			Scope: ScopeClient, Plumbing: true,
			Name: "Failure excerpt", Short: "how much of the daemon log is shown when it will not start",
		},
	}
}

func limitSettings() []Setting {
	return []Setting{
		{
			Path: "search.limit", Kind: KindInt, Default: defaults.Num(defaults.SearchLimit),
			Scope: ScopeCall, Hot: true,
			Commands:    []string{"search", "serve"},
			FlagAliases: []string{"limit", "n"},
			Name:        "Search results", Short: "how many tools a search returns",
			Long: "Call-scoped: the CLI sends its effective value with the request, " +
				"so --limit on one command reaches a daemon that was started " +
				"without it.",
		},
		{
			Path: "search.semantic", Kind: KindBool, Default: "false",
			Scope: ScopeCall, Hot: true,
			Commands: []string{"search"},
			Name:     "Semantic search", Short: "rank search results using local dense vector embeddings",
		},
		{
			Path: "completion.maxValues", Kind: KindInt, Default: defaults.Num(defaults.CompletionValues),
			Scope: ScopeDaemon, Hot: true, Plumbing: true,
			Name: "Completion values", Short: "how many completions one reply carries",
			Long: "The MCP specification caps a completion reply at one hundred. " +
				"Lower is legal; higher is not, and a server that sends more is " +
				"sending something a conforming client will reject.",
		},
		{
			Path: "elicit.pendingLimit", Kind: KindInt, Default: defaults.Num(defaults.ElicitPending),
			Scope: ScopeDaemon, Hot: true, Plumbing: true,
			Name: "Pending questions", Short: "how many unanswered questions one listing returns",
		},
		{
			Path: "registry.limit", Kind: KindInt, Default: defaults.Num(defaults.RegistryLimit),
			Scope: ScopeCall, Hot: true, Commands: []string{"registry"},
			Name: "Registry results", Short: "how many registry results are returned",
		},
		{
			Path: "registry.pageSize", Kind: KindInt, Default: defaults.Num(defaults.RegistryPageSize),
			Scope: ScopeClient, Plumbing: true,
			Name: "Registry page size", Short: "how many entries are fetched per registry request",
			Long: "The size of one request, not of the answer. registry.limit is how " +
				"many results a search returns; the search asks for pages of this " +
				"size, following the registry's cursor, until it has them.",
		},
		{
			Path: "registry.maxPages", Kind: KindInt, Default: defaults.Num(defaults.RegistryMaxPages),
			Scope: ScopeClient, Plumbing: true,
			Name: "Registry pages", Short: "how many requests one registry search may make",
			Long: "A bound, not a page size. A registry that always answers with " +
				"another cursor would otherwise be followed until registry.timeout; " +
				"a search that stops here says there was more.",
		},
		{
			Path: "registry.timeout", Kind: KindDuration, Default: defaults.Str(defaults.RegistryTimeout),
			Scope: ScopeClient,
			Name:  "Registry timeout", Short: "how long a registry request may take",
			Long: "A search is one request however many pages it takes, so this " +
				"bounds the whole walk rather than each page -- otherwise a slow " +
				"registry could hold a command for registry.maxPages times this.",
		},
		{
			Path: "logstore.queryLimit", Kind: KindInt, Default: defaults.Num(defaults.LogQueryLimit),
			Scope: ScopeCall, Hot: true,
			Name: "Log records", Short: "how many records a log query returns by default",
		},
		{
			Path: "logstore.followBacklog", Kind: KindInt, Default: defaults.Num(defaults.FollowBacklog),
			Scope: ScopeClient, Plumbing: true,
			Name: "Follow backlog", Short: "how many records one poll of `mcpx log --follow` may emit",
			Long: "A ceiling on a burst, not a page size. Without one, a daemon " +
				"that logged heavily while nobody was watching floods the terminal " +
				"the moment somebody starts.",
		},
		{
			Path: "stats.top", Kind: KindInt, Default: defaults.Num(defaults.StatsTop),
			Scope: ScopeCall, Hot: true,
			Name: "Ranking rows", Short: "how many rows a ranked statistic shows",
		},
		{
			Path: "events.history", Kind: KindInt, Default: defaults.Num(defaults.EventHistory),
			Scope: ScopeDaemon,
			Name:  "Event history", Short: "how many past events a late subscriber can replay",
			Long: "A subscriber that reconnects asks for everything after the last " +
				"sequence it saw. Beyond this many events the answer is a gap " +
				"marker instead, which is honest rather than a stream with a hole " +
				"in it.",
		},
		{
			Path: "events.reconnect", Kind: KindDuration, Default: defaults.Str(defaults.StreamReconnect),
			Scope: ScopeClient,
			Name:  "Reconnect delay", Short: "how long a client waits before resuming a dropped stream",
		},
		{
			Path: "tasks.ttl", Kind: KindDuration, Default: defaults.Str(defaults.TaskTTL),
			Scope: ScopeDaemon, Hot: true,
			Name: "Task retention", Short: "how long a finished task's result is kept",
			Long: "A result nobody collects is memory nobody frees, so it expires. " +
				"Raise it if the thing collecting results runs on its own schedule.",
		},
		{
			Path: "tasks.resultWait", Kind: KindDuration, Default: defaults.Str(defaults.TaskResultWait),
			Scope: ScopeCall, Hot: true,
			Name: "Task result wait", Short: "how long collecting a task result blocks before giving up",
		},
	}
}

func serviceSettings() []Setting {
	return []Setting{
		{
			Path: "daemon.idleExit", Kind: KindDuration, Default: "0s",
			Scope: ScopeDaemon, Commands: []string{"daemon"},
			FlagAliases: []string{"idle-exit"},
			Name:        "Idle exit", Short: "stop the daemon after this long with nothing to do",
			Long: "Zero means never, which is right for a daemon under a service " +
				"manager. A daemon started on demand sets autostart.idleExit " +
				"instead, so the two cases cannot be confused for each other.",
		},
		{
			Path: "serve.mode", Kind: KindString, Default: "code-mode",
			Scope: ScopeDaemon, Commands: []string{"serve"},
			FlagAliases: []string{"mode"},
			Name:        "Serve mode", Short: "server mode: code-mode, reactive, or full",
		},
		{
			Path: "catalog.pinnedTools", Kind: KindList, Default: "",
			Scope: ScopeDaemon,
			Name:  "Pinned tools", Short: "tools always exposed in reactive mode",
		},
		{
			Path: "retention.maxTools", Kind: KindInt, Default: "24",
			Scope: ScopeDaemon,
			Name:  "Max tools retention", Short: "maximum active tools in reactive working set",
		},
		{
			Path: "retention.stickyWindow", Kind: KindDuration, Default: "2h",
			Scope: ScopeDaemon,
			Name:  "Sticky retention window", Short: "duration explicitly requested tools remain retained",
		},
		{
			Path: "embeddings.backend", Kind: KindString, Default: "auto",
			Scope: ScopeDaemon, Hot: true,
			Enum: []string{"auto", "local", "remote", "wasm"},
			Name: "Embeddings backend", Short: "which embedding engine to use (auto, local, remote, wasm)",
		},
		{
			Path: "embeddings.url", Kind: KindString, Default: "",
			Scope: ScopeDaemon, Hot: true,
			Name: "Remote embeddings URL", Short: "OpenAI-compatible embeddings endpoint URL",
		},
		{
			Path: "embeddings.apiKey", Kind: KindString, Default: "",
			Scope: ScopeDaemon, Hot: true,
			Name: "Remote embeddings API key", Short: "API key for remote embeddings endpoint",
		},
		{
			Path: "embeddings.model", Kind: KindString, Default: "text-embedding-3-small",
			Scope: ScopeDaemon, Hot: true,
			Name: "Remote embeddings model", Short: "model name for remote embeddings endpoint",
		},
		{
			Path: "embeddings.wasmPath", Kind: KindString, Default: "",
			Scope: ScopeDaemon, Hot: true,
			Name: "WASM model path", Short: "path to compiled WebAssembly neural embedding model",
		},
		{
			Path: "daemon.warm", Kind: KindBool, Default: "true",
			Scope: ScopeDaemon, Commands: []string{"daemon"},
			FlagAliases: []string{"warm"},
			Name:        "Warm at startup", Short: "read every server's schemas in the background at startup",
		},
		{
			Path: "daemon.watchConfig", Kind: KindBool, Default: "true",
			Scope: ScopeDaemon, Hot: true,
			Name:  "Watch the configuration",
			Short: "re-read the configuration files when they change on disk",
			Long: "Checked on the same tick that reaps idle instances, so an edit " +
				"takes effect within daemon.reapInterval without anything being " +
				"restarted. Off, a file edited by hand does nothing until the " +
				"daemon is restarted -- which was the old behaviour, and was not " +
				"guessable from anything mcpx printed.",
		},
		{
			Path: "daemon.warmTimeout", Kind: KindDuration, Default: defaults.Str(defaults.WarmTimeout),
			Scope: ScopeDaemon, Plumbing: true,
			Name: "Warm timeout", Short: "how long the background schema fetch may take",
		},
		{
			Path: "daemon.refreshTimeout", Kind: KindDuration, Default: defaults.Str(defaults.RefreshTimeout),
			Scope: ScopeDaemon, Hot: true,
			Name: "Refresh timeout", Short: "how long POST /v1/refresh may take",
		},
		{
			Path: "daemon.leaseTTL", Kind: KindDuration, Default: defaults.Str(defaults.LeaseTTL),
			Scope: ScopeDaemon, Hot: true, Plumbing: true,
			Name: "Lease retention", Short: "how long a silent caller's instances are remembered",
			Long: "A caller that never says it has finished would otherwise hold its " +
				"instances forever. After this long with no request the lease is " +
				"forgotten and the reaper may stop them.",
		},
		{
			Path: "daemon.socketProbeTimeout", Kind: KindDuration, Default: defaults.Str(defaults.SocketProbeTimeout),
			Scope: ScopeDaemon, Plumbing: true,
			Name: "Stale socket probe", Short: "how long a socket left by a crashed daemon is given to answer",
		},
		{
			Path: "daemon.takeoverTimeout", Kind: KindDuration, Default: defaults.Str(defaults.TakeoverTimeout),
			Scope: ScopeDaemon, Plumbing: true,
			Name: "Takeover timeout", Short: "how long a handoff may take, and how long the old daemon waits for its successor to hang up",
		},
		{
			Path: "daemon.inlineStartTimeout", Kind: KindDuration, Default: defaults.Str(defaults.InlineStartTimeout),
			Scope: ScopeClient, Plumbing: true,
			Name: "Inline start wait", Short: "how long an in-process daemon has to become reachable",
		},
		{
			Path: "daemon.inlineStartPoll", Kind: KindDuration, Default: defaults.Str(defaults.InlineStartPoll),
			Scope: ScopeClient, Plumbing: true,
			Name: "Inline start poll", Short: "how often a starting in-process daemon is probed",
		},
		{
			Path: "daemon.probeTimeout", Kind: KindDuration, Default: defaults.Str(defaults.InlineProbeTimeout),
			Scope: ScopeClient, Plumbing: true,
			Name: "Daemon probe timeout", Short: "how long another daemon's socket is given to answer",
			Long: "Used when listing every daemon on this machine. Short, because " +
				"the listing probes each one in turn and a dead socket should not " +
				"hold up the rest.",
		},
		{
			Path: "doctor.timeout", Kind: KindDuration, Default: defaults.Str(defaults.DoctorTimeout),
			Scope: ScopeClient, Commands: []string{"doctor"},
			Name: "Diagnosis timeout", Short: "how long `mcpx doctor` gives the daemon to answer",
		},
		{
			Path: "session.releaseTimeout", Kind: KindDuration, Default: defaults.Str(defaults.ReleaseTimeout),
			Scope: ScopeClient, Plumbing: true,
			Name: "Release timeout", Short: "how long releasing a finished session may take",
			Long: "Bounded and short: this runs as a command exits, and a hang here " +
				"would be a shell that will not return.",
		},
	}
}
