package settings

// The environment a script is started with.
//
// Settings are what a person tells mcpx. This is the other direction: what
// mcpx, and the opencode plugin, tell a program they start. A script reads
// its session from here, the generated client finds the daemon from here,
// and an `mcpx` run inside a script or a shell command learns which session
// it belongs to from here. It is an interface with as many consumers as the
// settings have, and until #172 it was declared nowhere -- the only list was
// the code that happened to set each name, spread over five packages and a
// TypeScript file, and the documentation had already drifted from it once
// (MCPX_SESSION and MCPX_SESSION_ID are two variables, not one renamed).
//
// This table is that declaration. docs/environment.md is generated from it,
// and a test holds it to the code in both directions: every name a setter
// writes is here with that setter listed, and every setter listed here
// writes the name.

// Setter is a part of mcpx that writes variables into a child's environment.
type Setter string

const (
	// SetByCLI is `mcpx exec` and `mcpx run` on this machine.
	SetByCLI Setter = "cli"
	// SetByExec is the exec service behind /v1/exec and the mcpx_exec tool.
	// The CLI's local runs go through it too, and it fills in whatever the
	// CLI did not already set.
	SetByExec Setter = "exec"
	// SetByConsumer is the daemon running an event consumer's script.
	SetByConsumer Setter = "consumer"
	// SetByRunner is the runner, which starts every script whoever asked.
	SetByRunner Setter = "runner"
	// SetByPlugin is the opencode plugin, into the environment of every
	// shell command the editor runs.
	SetByPlugin Setter = "plugin"
)

// Describe is the setter as a reader of the documentation knows it.
func (s Setter) Describe() string {
	switch s {
	case SetByCLI:
		return "`mcpx exec` / `mcpx run`"
	case SetByExec:
		return "the exec service (`/v1/exec`, `mcpx_exec`)"
	case SetByConsumer:
		return "an event consumer"
	case SetByRunner:
		return "the runner, for every script"
	case SetByPlugin:
		return "the opencode plugin"
	}
	return string(s)
}

// EnvVar is one variable in the contract.
type EnvVar struct {
	Name string
	// SetBy is every part of mcpx that writes it.
	SetBy []Setter
	// When qualifies SetBy. Empty means always.
	When string
	// Meaning is what the value is.
	Meaning string
	// ReadBy is who acts on it.
	ReadBy string
	// Setting is the registry path that reads the same name, for the few
	// that are also configuration: a variable mcpx writes for a child that
	// the child, when it is mcpx, reads as a setting.
	Setting string
}

// ScriptEnv is the contract, in the order the documentation presents it.
func ScriptEnv() []EnvVar {
	const (
		client = "the generated client"
		nested = "`mcpx` run inside the script or shell"
	)
	return []EnvVar{
		// ---- the session ----
		{
			Name:  "MCPX_SESSION",
			SetBy: []Setter{SetByCLI, SetByExec, SetByConsumer},
			Meaning: "the session key this run's tool calls are leased under: " +
				"--session, else MCPX_SESSION_ID, else one minted for the run",
			ReadBy: client + ", which sends it with every call",
		},
		{
			Name:  "MCPX_SESSION_ID",
			SetBy: []Setter{SetByCLI, SetByExec, SetByConsumer, SetByPlugin},
			When: "the CLI passes through the value it was given, which may be " +
				"empty; the plugin sets it at every plugin.env level",
			Meaning: "the session the host assigned. Unlike MCPX_SESSION it " +
				"survives from one command to the next, so it is what " +
				"session-scoped servers key on",
			ReadBy: nested + ", as its session when --session is not given",
		},
		{
			Name:    "MCPX_PARENT_SESSION_ID",
			SetBy:   []Setter{SetByCLI, SetByPlugin},
			When:    "the CLI passes through what it was given; the plugin at plugin.env=full, when there is a parent",
			Meaning: "the session that spawned this one, so a parent agent and its subagents can share a server",
			ReadBy:  nested + ", and " + client + " for parent-session scoping",
		},
		{
			Name:    "MCPX_EPHEMERAL",
			SetBy:   []Setter{SetByCLI},
			Meaning: "1 when the session key was minted for this run alone, so its instances may be stopped as soon as it ends",
			ReadBy:  client,
		},
		{
			Name:    "MCPX_RUN",
			SetBy:   []Setter{SetByCLI, SetByExec},
			Meaning: "this execution's run id, which groups the artifacts it produces",
			ReadBy:  client + ", when storing an artifact",
		},
		{
			Name:  "MCPX_INPUT",
			SetBy: []Setter{SetByCLI},
			When:  "mcpx exec and mcpx run, locally and with --remote; not when mcpx serve collects the output",
			Meaning: "report: nothing in this run can answer a server's question mid-call, so a call that " +
				"asks one exits 75 with the question instead of waiting for it to expire",
			ReadBy: client + ", sent as X-Mcpx-Input",
		},
		// ---- reaching the daemon ----
		{
			Name:    "MCPX_ENDPOINT",
			SetBy:   []Setter{SetByCLI, SetByExec, SetByConsumer},
			Meaning: "the daemon's HTTP endpoint",
			ReadBy:  client + "; the plugin as a fallback for MCPX_DAEMON_ENDPOINT",
		},
		{
			Name:  "MCPX_SOCKET",
			SetBy: []Setter{SetByCLI, SetByExec},
			When:  "empty when the daemon is remote, since its socket is not reachable from here",
			Meaning: "the daemon's unix socket, which is faster than the endpoint. " +
				"Also where a daemon binds and a client dials, for anyone who sets it by hand",
			ReadBy: client + ", " + nested + ", the plugin",
		},
		{
			Name:    "MCPX_ARTIFACTS_LOCAL",
			SetBy:   []Setter{SetByCLI, SetByExec},
			Meaning: "1 when the daemon shares this filesystem, so artifact({path}) may hand over a path instead of the bytes",
			ReadBy:  client,
		},
		// ---- where things are ----
		{
			Name:    "MCPX_CWD",
			SetBy:   []Setter{SetByCLI, SetByExec},
			When:    "the exec service only when the caller sent its working directory",
			Meaning: "the directory the caller ran mcpx from, which a remote run cannot discover",
			ReadBy:  client + " (paths.cwd, and cwd scoping)",
		},
		{
			Name:    "MCPX_PID",
			SetBy:   []Setter{SetByCLI, SetByExec},
			Meaning: "the pid of the mcpx process that started the script",
			ReadBy:  client + ", for pid scoping",
		},
		{
			Name:  "MCPX_ENTRY",
			SetBy: []Setter{SetByRunner},
			Meaning: "the file the runtime was started on: mcpx's generated launcher, " +
				"or the script itself under --no-launcher. For exec the script is a generated file",
			ReadBy: client + " (paths.entry)",
		},
		{
			Name:    "MCPX_CLIENT",
			SetBy:   []Setter{SetByRunner},
			Meaning: "the generated client module",
			ReadBy:  client + " (paths.client)",
		},
		{
			Name:    "MCPX_CONFIG_PATH",
			SetBy:   []Setter{SetByCLI},
			Meaning: "the nearest configuration file in effect, empty when there is none",
			ReadBy:  client + " (paths.config)",
		},
		{
			Name:    "MCPX_SCRIPT_DIRS",
			SetBy:   []Setter{SetByCLI},
			Meaning: "the directories searched for named scripts, nearest first, colon-separated",
			ReadBy:  client + " (paths.scriptDirs)",
		},
		// ---- how the script runs ----
		{
			Name:    "MCPX_RUNTIME",
			SetBy:   []Setter{SetByRunner},
			Meaning: "deno, bun or node",
			ReadBy:  client,
		},
		{
			Name:    "MCPX_ALLOW_READ",
			SetBy:   []Setter{SetByRunner},
			When:    "only when the permissions allow reading files",
			Meaning: "1: the client's file helpers may read. Without it they refuse, naming the permission, rather than leaving it to the sandbox",
			ReadBy:  client,
		},
		{
			Name:    "MCPX_LOG_LEVEL",
			SetBy:   []Setter{SetByCLI},
			Meaning: "the lowest level the script's log keeps",
			ReadBy:  client + "; " + nested + " reads it as logging.level",
			Setting: "logging.level",
		},
		{
			Name:    "MCPX_LOG_SOURCE",
			SetBy:   []Setter{SetByCLI},
			Meaning: "which levels record a call site: a level name for that level and above, 0 for none",
			ReadBy:  client,
		},
		// ---- what the editor knows ----
		{
			Name:  "MCPX_DAEMON_ENDPOINT",
			SetBy: []Setter{SetByPlugin},
			When:  "plugin.env=standard or above, once the plugin has chosen a daemon",
			Meaning: "the daemon this editor session settled on, so a shell command " +
				"that runs mcpx talks to the same one instead of looking for its own",
			ReadBy:  nested + ", as daemon.endpoint; the plugin, as the daemon to use",
			Setting: "daemon.endpoint",
		},
		{
			Name:  "MCPX_TRACE_IDS",
			SetBy: []Setter{SetByPlugin},
			When:  "plugin.env=standard or above, when there is anything to say",
			Meaning: "JSON pairs such as [[\"session_id\",\"abc\"],[\"worktree\",\"/p\"]], " +
				"extended at full with the parent session and the whole ancestry",
			ReadBy: nested + ", which puts every pair on every log record",
		},
		{Name: "MCPX_CALL_ID", SetBy: []Setter{SetByPlugin}, When: "plugin.env=standard or above",
			Meaning: "the editor's id for this one tool call", ReadBy: "the command"},
		{Name: "MCPX_OPENCODE_CWD", SetBy: []Setter{SetByPlugin}, When: "plugin.env=standard or above",
			Meaning: "the directory the command runs in, which is not always the project root", ReadBy: "the command"},
		{Name: "MCPX_OPENCODE_DIRECTORY", SetBy: []Setter{SetByPlugin}, When: "plugin.env=standard or above",
			Meaning: "the project directory the editor opened", ReadBy: "the command"},
		{Name: "MCPX_OPENCODE_WORKTREE", SetBy: []Setter{SetByPlugin}, When: "plugin.env=standard or above",
			Meaning: "the project's worktree", ReadBy: "the command"},
		{Name: "MCPX_WORKTREE_NAME", SetBy: []Setter{SetByPlugin}, When: "plugin.env=standard or above, when there is a worktree",
			Meaning: "the worktree's last path element", ReadBy: "the command"},
		{Name: "MCPX_PROJECT_ID", SetBy: []Setter{SetByPlugin}, When: "plugin.env=standard or above",
			Meaning: "the editor's project id", ReadBy: "the command"},
		{Name: "MCPX_HARNESS", SetBy: []Setter{SetByPlugin}, When: "plugin.env=standard or above",
			Meaning: "opencode", ReadBy: "the command"},
		{Name: "MCPX_HARNESS_VERSION", SetBy: []Setter{SetByPlugin}, When: "plugin.env=standard or above",
			Meaning: "the editor's version", ReadBy: "the command"},
		{Name: "MCPX_HARNESS_PID", SetBy: []Setter{SetByPlugin}, When: "plugin.env=standard or above",
			Meaning: "the editor's pid", ReadBy: "the command"},
		{Name: "MCPX_HARNESS_STARTED", SetBy: []Setter{SetByPlugin}, When: "plugin.env=standard or above",
			Meaning: "when the plugin started", ReadBy: "the command"},
		{Name: "MCPX_SHELL_SEQ", SetBy: []Setter{SetByPlugin}, When: "plugin.env=standard or above",
			Meaning: "a counter of shell commands in this editor process", ReadBy: "the command"},
		{Name: "MCPX_SESSION_TITLE", SetBy: []Setter{SetByPlugin}, When: "plugin.env=full",
			Meaning: "the session's title", ReadBy: "the command"},
		{Name: "MCPX_SESSION_DIRECTORY", SetBy: []Setter{SetByPlugin}, When: "plugin.env=full",
			Meaning: "the session's directory", ReadBy: "the command"},
		{Name: "MCPX_SESSION_VERSION", SetBy: []Setter{SetByPlugin}, When: "plugin.env=full",
			Meaning: "the editor version that created the session", ReadBy: "the command"},
		{Name: "MCPX_SESSION_DEPTH", SetBy: []Setter{SetByPlugin}, When: "plugin.env=full",
			Meaning: "how many parents the session has", ReadBy: "the command"},
		{Name: "MCPX_SESSION_CREATED", SetBy: []Setter{SetByPlugin}, When: "plugin.env=full, when known",
			Meaning: "when the session was created, ISO 8601", ReadBy: "the command"},
		{Name: "MCPX_SESSION_AGE_MS", SetBy: []Setter{SetByPlugin}, When: "plugin.env=full, when known",
			Meaning: "the session's age when the command started, in milliseconds", ReadBy: "the command"},
	}
}
