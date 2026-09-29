package settings

// execSettings are the knobs for running a script and for the artifacts it
// produces.
//
// Two groups in one file because they are one feature: an artifact only
// exists because a script produced it, and every question about delivery is a
// question about how the run reports back.
func execSettings() []Setting {
	return append(execRunSettings(), artifactSettings()...)
}

func execRunSettings() []Setting {
	return []Setting{
		{
			Path: "exec.timeout", Scope: ScopeCall, Hot: true, Commands: runCommands, Kind: KindDuration, Default: "120s",
			Name:  "Exec timeout",
			Short: "kill a script after this long",
			Long: "Applies wherever the script runs. A run over /v1/exec is bounded " +
				"by the same value, because a daemon holding a runaway script is " +
				"worse than a terminal holding one: nobody is watching it.",
		},
		{
			Path: "exec.output", Scope: ScopeCall, Hot: true, Commands: runCommands, Kind: KindEnum, Default: "text",
			Enum:        []string{"text", "structured", "stream"},
			FlagAliases: []string{"output"},
			Name:        "Output shape",
			Short:       "text for a terminal, structured for one document, stream for frames",
			Long: "Text is the default at the command line and nothing else is: " +
				"stdout is the script's answer and a person reading it wants it " +
				"unadorned. /v1/exec and the MCP tool default to structured, " +
				"because their callers want the emits, the logs and the artifact " +
				"list as fields rather than as text they have to scrape. Stream " +
				"delivers the same information as frames as they happen, which is " +
				"what lets a consumer read the result and stop before downloading " +
				"artifacts it does not want.",
		},
		{
			Path: "exec.where", Scope: ScopeCall, Hot: true, Commands: runCommands, Kind: KindEnum, Default: "auto",
			Enum:  []string{"auto", "local", "remote"},
			Name:  "Where a script runs",
			Short: "in this process, or on the daemon",
			Long: "Auto runs locally when the daemon is on this machine and on the " +
				"daemon when it is not. Local is what a person at a terminal wants: " +
				"the script owns the terminal, reads the real standard input and " +
				"resolves relative paths against the working directory. Remote is " +
				"what makes a daemon on another machine useful, and what lets a " +
				"caller with no mcpx binary -- the opencode plugin -- run a script " +
				"at all.",
		},
	}
}

func artifactSettings() []Setting {
	return []Setting{
		{
			Path: "artifacts.enabled", Scope: ScopeDaemon, Kind: KindBool, Default: "true",
			Name:  "Artifacts",
			Short: "whether the daemon keeps files scripts produce",
			Long: "Off, artifact() fails and nothing is written. The store is a " +
				"directory of other people's bytes with a quota and a TTL, and " +
				"somewhere it should not exist at all.",
		},
		{
			Path: "artifacts.dir", Scope: ScopeCall, Hot: true, Commands: runCommands, Kind: KindString, Default: "",
			Name:  "Artifacts directory",
			Short: "write this run's artifacts here",
			Long: "A directory the caller can read. When it is on the same machine " +
				"as the daemon the files are hardlinked into it rather than copied, " +
				"so handing back a gigabyte costs an inode. Names are sanitised and " +
				"collisions get a numeric suffix; nothing is ever overwritten.",
		},
		{
			Path: "artifacts.delivery", Scope: ScopeCall, Hot: true, Commands: runCommands, Kind: KindEnum, Default: "reference",
			Enum:  []string{"reference", "inline", "stream"},
			Name:  "Delivery",
			Short: "how artifact bodies reach the caller",
			Long: "Reference hands back an id and a URI and nothing else, which is " +
				"the only mode whose cost does not scale with what the script " +
				"happened to produce. Inline base64s the body into the result, " +
				"bounded by artifacts.inlineMaxBytes. Stream sends bodies as frames " +
				"after the end frame, so a consumer can read the answer and " +
				"disconnect before paying for them.",
		},
		{
			Path: "artifacts.ttl", Scope: ScopeDaemon, Kind: KindDuration, Default: "24h",
			Name:  "Artifact retention",
			Short: "how long an artifact is kept before it is collected",
		},
		{
			Path: "artifacts.maxBytes", Scope: ScopeDaemon, Kind: KindBytes, Default: "64MiB",
			Name:  "Per-artifact limit",
			Short: "the largest single artifact that may be stored",
			Long: "Exceeding it is an error rather than a truncation. A truncated " +
				"screenshot is worse than a refused one, because it looks like it " +
				"worked.",
		},
		{
			Path: "artifacts.quota", Scope: ScopeDaemon, Kind: KindBytes, Default: "1GiB",
			Name:  "Store quota",
			Short: "the total the artifact store may hold",
			Long: "Counted over distinct content: the store is addressed by sha256, " +
				"so two registrations of the same bytes cost one copy.",
		},
		{
			Path: "artifacts.inlineMaxBytes", Scope: ScopeCall, Hot: true, Kind: KindBytes, Default: "1MiB",
			Name:  "Inline limit",
			Short: "the largest artifact that may be base64'd into a result",
			Long: "Well under the per-artifact limit on purpose. Inline delivery " +
				"puts bytes into the caller's context, which is the cost this " +
				"whole feature exists to avoid.",
		},
		{
			Path: "artifacts.chunkBytes", Scope: ScopeDaemon, Kind: KindBytes, Default: "256KiB",
			Plumbing: true,
			Name:     "Stream chunk size",
			Short:    "how much of a body one streamed frame carries",
		},
		{
			Path: "artifacts.gcInterval", Scope: ScopeDaemon, Kind: KindDuration, Default: "10m",
			Plumbing: true,
			Name:     "Collection interval",
			Short:    "how often expired artifacts are swept",
		},
		{
			Path: "artifacts.interceptImages", Scope: ScopeCall, Hot: true, Kind: KindBool, Default: "true",
			Name:  "Intercept inline media",
			Short: "turn image and audio content in a result into artifact references",
			Long: "An upstream tool that returns a screenshot returns it as base64 " +
				"image content. Left alone that lands in an agent's context, where " +
				"one screenshot costs more than the rest of the task. With this on, " +
				"and only when the caller declared it can receive artifacts, the " +
				"block is stored and replaced by a resource_link. The script still " +
				"sees the original bytes: a script that cannot inspect the image it " +
				"just took is a worse script.",
		},
	}
}
