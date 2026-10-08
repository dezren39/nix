package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/settings"
)

// Command is one subcommand, described once so that help, the man page and
// the command table cannot disagree about what exists.
//
// The previous arrangement listed commands in three places -- the dispatch
// map, the help text, and nothing else, because there was no man page -- and
// the help text was already missing two of them.
type Command struct {
	Name    string
	Aliases []string
	Summary string
	// Usage is the argument shape, without the leading "mcpx".
	Usage string
	// Detail is the paragraph in the man page.
	Detail string
	// Examples are shown under the command in the man page.
	Examples []string
	// Group orders the listing.
	Group string
	// Local says why a command reaches no /v1 operation. Every command
	// either reaches one or says why it cannot, so "the CLI can do it and
	// nothing else can" is a decision written down rather than a gap nobody
	// noticed; docs/parity.md prints it.
	Local string
}

// Commands is every subcommand mcpx has: the hand-written ones, then one per
// family of /v1 operations the command line reaches through a generated
// command.
func Commands() []Command {
	return append(handCommands(), opCommands()...)
}

// opCommands describes the generated commands from the same table that
// builds them, so help, the man page and completion cover an operation the
// moment it is declared.
func opCommands() []Command {
	var out []Command
	for _, g := range opGroups() {
		c := Command{Name: g.Name, Aliases: g.Aliases, Group: "operations"}
		var usage, detail []string
		if g.Bare != nil {
			c.Summary = lowerFirst(g.Bare.Summary)
			usage = append(usage, OpUsage(*g.Bare))
			detail = append(detail, strings.TrimSpace(g.Bare.Description+" "+g.Bare.Method+" "+g.Bare.Path+"."))
		}
		for _, v := range g.Verbs {
			usage = append(usage, v.Word+" "+OpUsage(v.Op))
			detail = append(detail, fmt.Sprintf("%s: %s. %s %s.", v.Word, v.Op.Summary, v.Op.Method, v.Op.Path))
		}
		if c.Summary == "" {
			// The family's root resource describes it best -- /v1/tasks
			// rather than /v1/tasks/{id}/cancel -- and a read of it better
			// than a write, since that is the answer to "what are these".
			lead := g.Verbs[0].Op
			for _, v := range g.Verbs[1:] {
				if len(v.Op.Path) < len(lead.Path) ||
					(len(v.Op.Path) == len(lead.Path) && v.Op.Method == "GET") {
					lead = v.Op
				}
			}
			c.Summary = lowerFirst(lead.Summary) + " (" + strings.Join(g.verbWords(), ", ") + ")"
		}
		c.Usage = strings.Join(usage, " | ")
		c.Detail = strings.Join(detail, " ")
		out = append(out, c)
	}
	return out
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// handCommands are the commands with an implementation of their own.
func handCommands() []Command {
	return []Command{
		{
			Name: "run", Group: "running",
			Local:   "Resolves a script name against the caller's search path, which only the caller has; the source itself runs anywhere through `exec`.",
			Usage:   "[flags] <script|file> [args...]",
			Summary: "run a named script or a file",
			Detail: "Resolves a bare name along the script search path, or takes a " +
				"path directly. The script is wrapped in a launcher that installs " +
				"the tool bindings, captures the console and runs the configured " +
				"phases; --no-launcher skips all of that. --runtime takes deno, bun, " +
				"node, a script.runtimes name or a path; --permissions composes " +
				"profiles in order (read,strict) or passes raw:<flags>; see " +
				"docs/runtimes.md. --preset applies named flag bundles from the " +
				"config's presets, below anything given explicitly.",
			Examples: []string{
				"mcpx run report",
				"mcpx run --runtime node --permissions readnet report",
				"mcpx run ./one-off.ts --format json",
				"mcpx run --no-launcher bare.ts",
			},
		},
		{
			Name: "exec", Group: "running",
			Usage:   "[flags] <source...>",
			Summary: "run a snippet given on the command line",
			Detail: "The snippet is generated into a module, so a --prefix shares its " +
				"scope and can declare bindings the snippet uses. That is the only " +
				"difference from run, which imports a file and therefore cannot.",
			Examples: []string{
				`mcpx exec 'const r = await tools.fs.read({path:"/etc/hosts"}); emit(r)'`,
				`mcpx exec --format json 'log.info("hello", {a:1})'`,
			},
		},
		{
			Name: "scripts", Group: "running",
			Local:   "Lists the caller's script search path.",
			Summary: "list every script on the search path",
			Detail: "Shows the name, where it was found, and its first comment line. " +
				"A script shadowed by a nearer one of the same name is listed too, " +
				"marked, because otherwise the shadowing is invisible.",
		},
		{
			Name: "recipes", Group: "running",
			Usage:   "[list|show|match|save|run] [<name>] [key=value...]",
			Summary: "saved scripts that declare their own holes",
			Detail: "A recipe is a script on the search path with a header declaring " +
				"its parameters: `// @param repo:string which repository`. " +
				"References to @repo in the body are replaced by the value, encoded " +
				"as JSON, so a recipe cannot be an injection site. Values not " +
				"supplied and without a default are asked for as one form, with a " +
				"deadline. Matching free text to a recipe is a deterministic score " +
				"over the name, the description, the parameter names and the tools " +
				"it calls -- no model, no cost, same answer every time.",
			Examples: []string{
				"mcpx recipes",
				"mcpx recipes run close-stale repo=me/thing days=14",
				"mcpx recipes match 'close old issues'",
			},
		},
		{
			Name: "prompt", Group: "running",
			Usage:   `[--run] [--set key=value] "<what you want done>"`,
			Summary: "a request in words, a script back",
			Detail: "Tries the recipes first, deterministically and for nothing. Only " +
				"when none matches does it ask for a script to be written, and " +
				"mcpx has no model to ask: the request goes out as MCP sampling " +
				"through the same broker that carries every other question, so " +
				"whatever drives mcpx answers it. It is given only the slice of " +
				"the catalog the search ranked for this request. Without an " +
				"answerer, it prints the ranked recipes and says plainly that no " +
				"model is available. The script is returned rather than run unless " +
				"--run is given, because generated code runs with your credentials. " +
				"The daemon's autonomy.max bounds --run: above it the request is " +
				"lowered, and mcpx says \"requested run, clamped to propose by " +
				"autonomy.max\" on stderr.",
			Examples: []string{
				`mcpx prompt "take a screenshot of the checkout page"`,
				`mcpx prompt --run "list my open issues"`,
			},
		},
		{
			Name: "diagnose", Group: "inspection",
			Usage:   "<script|file|-|'<source>'>",
			Summary: "explain what is wrong with a script, without running it",
			Detail: "Compares every tool call in the source against the schemas the " +
				"servers publish now, and against what those schemas used to be. " +
				"The answer names the tool, the argument at fault, what changed " +
				"and when, the line as written, and the minimum that works. No " +
				"model is involved: these are facts mcpx already holds, and an " +
				"error being specific is worth more than an error being reworded.",
			Examples: []string{
				"mcpx diagnose report",
				"mcpx diagnose - < ./draft.ts",
			},
		},
		{
			Name: "ls", Aliases: []string{"list", "namespaces"}, Group: "discovery",
			Summary: "list configured servers and their namespaces",
		},
		{
			Name: "catalog", Group: "discovery",
			Usage:   "[--budget N] [--bias words]",
			Summary: "list tools within a token budget",
			Detail: "Round-robins across servers so a large server cannot crowd out a " +
				"small one, and lists every namespace even when the budget runs " +
				"out mid-way, because knowing a server exists is worth more than " +
				"knowing three more of another server's tools.",
		},
		{
			Name: "types", Group: "discovery",
			Usage:   "<namespace>[.<tool>]",
			Summary: "print the TypeScript signature of a namespace or tool",
		},
		{
			Name: "search", Group: "discovery",
			Usage: "[--semantic] <words>", Summary: "find tools by name and description (keyword or semantic)",
		},
		{
			Name: "call", Group: "discovery",
			Usage:   "<namespace>.<tool> [json]",
			Summary: "call one tool directly, without writing a script",
		},
		{
			Name: "batch", Group: "discovery",
			Usage:   "[-c '<json>' | --file <path> | <file.json>]",
			Summary: "execute multiple tool calls in a single turn",
		},
		{
			Name: "client", Group: "discovery",
			Summary: "print the generated TypeScript client",
		},
		{
			Name: "status", Group: "daemon",
			Summary: "show the daemon, its servers and their instances",
		},
		{
			Name: "daemons", Group: "daemon",
			Local:   "Reads every daemon's info file on this machine; one daemon cannot see the others.",
			Summary: "list every mcpx daemon on this machine",
		},
		{
			Name: "refresh", Group: "daemon",
			Summary: "re-read tool schemas from every server",
		},
		{
			Name: "restart", Group: "daemon",
			Usage: "[--lazy] [--daemon] [server]", Summary: "restart servers, or the daemon itself",
			Detail: "Stops every running instance and starts a replacement under the " +
				"same scope key, waiting until it has initialized, so a server with a " +
				"slow start comes back warm and a broken configuration fails now, " +
				"with the server's stderr, and a non-zero exit. With nothing running " +
				"a global-scope server starts one instance; a scoped server " +
				"(session, pid, cwd, ...) with nothing running has no caller to start " +
				"one for and starts nothing. A pid-scoped instance whose owner has " +
				"exited, and a per-call instance, are reported and not replaced. " +
				"--lazy only stops: the next call starts a fresh instance. " +
				"--daemon restarts the mcpx daemon process itself.",
		},
		{
			Name: "stop", Group: "daemon",
			Usage: "[--all]", Summary: "stop the daemon for this configuration, or all of them",
		},
		{
			Name: "daemon", Group: "daemon",
			Local:   "Starts a daemon, which a daemon's own API cannot do.",
			Usage:   "[--detached]",
			Summary: "run the daemon in the foreground",
			Detail: "Normally the daemon is started on demand. Running it in the " +
				"foreground is for watching what it does, or for supervising it " +
				"with something else.",
		},
		{
			Name: "config", Group: "configuration",
			Local:   "Reads the configuration files on the caller's search path; `settings` is the running daemon's view.",
			Usage:   "[--path|--sources|--defaults|--schema]",
			Summary: "show the resolved configuration and where it came from",
			Detail: "--sources lists every file that contributed and which server each " +
				"one defined. --defaults prints the built-in layer underneath " +
				"everything. --schema prints every setting with its flag and its " +
				"environment variable; --plumbing adds the internal ones.",
			Examples: []string{
				"mcpx config --sources",
				"mcpx config --schema --plumbing",
			},
		},
		{
			Name: "settings", Group: "configuration",
			Usage:   "list|get|set|unset [<path>] [<value>] [--persist runtime|project|user]",
			Summary: "read and change any setting, from anywhere",
			Detail: "`mcpx config --schema` prints what exists; this prints what is " +
				"in force and where each value came from, which is the question " +
				"people actually have. set applies to the running daemon at once " +
				"when the setting allows it, and --persist writes the project or " +
				"user configuration file. Each setting names the process that reads " +
				"it -- daemon, client, one call, or the plugin -- so a value that " +
				"cannot take effect says so instead of being silently ignored.",
			Examples: []string{
				"mcpx settings list --changed",
				"mcpx settings get pool.idleTimeout",
				"mcpx settings set logging.level debug",
				"mcpx settings set pool.max 8 --persist project",
			},
		},
		{
			Name: "servers", Group: "configuration",
			Usage:   "list|add|remove [<name>] [-- <command> ...]",
			Summary: "add or remove an MCP server without restarting anything",
			Detail: "The entry is written to a configuration file and the daemon " +
				"reloads, so a server added here is callable immediately and is " +
				"still there tomorrow. Servers whose definition did not change keep " +
				"their running process, so adding one does not restart the rest. " +
				"`mcpx registry add` is the same operation for a server a public " +
				"registry already describes.",
			Examples: []string{
				"mcpx servers list",
				"mcpx servers add fs -- npx -y @modelcontextprotocol/server-filesystem /tmp",
				"mcpx servers add remote --url https://example.com/mcp --header AUTHORIZATION",
				"mcpx servers remove fs",
			},
		},
		{
			Name: "init", Group: "configuration",
			Local:   "Writes a file into the current directory.",
			Summary: "write a starter configuration file",
		},
		{
			Name: "schema", Group: "discovery",
			Local:   "Reshapes what `tools` and `types` return into JSON Schema, OpenAPI or MCP form; the data is those operations', the conversion is local.",
			Usage:   "[--format json-schema|openapi|typescript|mcp] [--ns ...]",
			Summary: "publish tool types in whichever form a consumer reads",
			Detail: "The same information, reshaped. TypeScript is what a script " +
				"imports, JSON Schema is what a validator reads, OpenAPI is what a " +
				"client generator reads, and the MCP form is what an MCP host " +
				"reads. A caller should not have to convert one into another.",
			Examples: []string{
				"mcpx schema --format openapi -o mcpx-openapi.json",
				"mcpx schema --format json-schema --ns fff",
			},
		},
		{
			Name: "elicit", Group: "inspection",
			Usage:   "[list|show|answer|decline|cancel|watch|result] [<id>] [<json>|key=value...]",
			Summary: "answer a question a server asked",
			Detail: "A server may stop mid-call and ask something -- which repository, " +
				"are you sure, log in here. mcpx stores the question with a " +
				"deadline rather than blocking on it, so whoever answers need not " +
				"be whoever asked: a call from CI can be answered from a laptop " +
				"twenty minutes later. `mcpx call`, `exec` and `run` stop at a " +
				"question with exit status 75 (EX_TEMPFAIL), printing what is " +
				"asked, the `mcpx elicit answer` command, and the task id whose " +
				"result `mcpx task result` collects once it is answered.",
			Examples: []string{
				"mcpx elicit list",
				"mcpx elicit answer elc-9f2c1a84 repo=me/thing",
				"mcpx elicit decline elc-9f2c1a84",
			},
		},
		{
			Name: "doctor", Group: "inspection",
			Local: "Checks this machine: binaries on PATH, permissions, configuration files.",
			Usage: "[-v]", Summary: "diagnose the installation",
			Detail: "Checks the runtime, git, the configuration chain, whether every " +
				"server's command is actually installed, the directories, the " +
				"daemon, the log index and the optional integrations. Each check " +
				"says what to do about it, because one that only reports a problem " +
				"leaves the reader where they started.",
		},
		{
			Name: "prompts", Group: "discovery",
			Usage: "[<ns>.<name> key=value...]", Summary: "list or render a server's prompts",
			Detail: "Prompts are the half of MCP that is not tools: a server saying " +
				"\"here is the wording that works for this\". A server publishing a " +
				"good one has encoded expertise that would otherwise be " +
				"rediscovered by whoever writes the request.",
		},
		{
			Name: "resources", Group: "discovery",
			Usage: "[<ns>/<uri>]", Summary: "list or read a server's resources",
		},
		{
			Name: "tui", Group: "inspection",
			Local:   "An interactive interface over the reads above; it consumes the API rather than extending it.",
			Summary: "full-screen browser for namespaces, tools and the log",
			Detail: "Three panes -- namespaces, their tools, one signature -- so " +
				"comparing two tools is a keystroke rather than two commands and " +
				"a scrollback hunt. Also what bare `mcpx` opens when there is a " +
				"terminal to draw on. Use explore instead when you want the " +
				"result in scrollback, or the plain commands for scripting.",
			Examples: []string{"mcpx", "mcpx tui", "mcpx --tui"},
		},
		{
			Name: "explore", Group: "inspection",
			Local:   "An interactive interface over the reads above; it consumes the API rather than extending it.",
			Summary: "browse namespaces, tools and the log interactively",
			Detail: "Everything it shows is available from other commands; it exists " +
				"because discovery is a loop, and running four commands with " +
				"different flags to go round it once is enough friction that " +
				"people guess instead. Every screen prints the command that " +
				"produced it, so it teaches its own scriptable form.",
		},
		{
			Name: "dashboard", Group: "inspection",
			Usage:   "[--open]",
			Summary: "open or show the live web dashboard",
			Detail: "Serves an interactive web dashboard for monitoring server pools, " +
				"live instances, tool semantic search, and token economics telemetry.",
		},
		{
			Name: "proxy", Group: "running",
			Usage:   "[--port <port>] [--upstream <url>]",
			Summary: "run or print the OpenAI-compatible LLM reverse proxy",
			Detail: "Exposes a transparent OpenAI-compatible /v1/chat/completions endpoint " +
				"that extracts user prompts, semantically discovers tools from upstream MCP " +
				"servers, and injects them dynamically while recording token savings.",
		},
		{
			Name: "embeddings", Group: "running",
			Usage:   "test <text> | export [--output <path>] | import <file.json>",
			Summary: "test, export, or import vector embeddings",
			Detail: "Interacts with the daemon vector embeddings engine and index. " +
				"Exports or imports pre-computed vector JSON representations across environments, " +
				"or tests embedding representations across local, WASM, or remote backends.",
		},
		{
			Name: "feedback", Group: "running",
			Usage:   "<trace-id> [--score <0.0-1.0>] [--notes <text>] [--scores <json>]",
			Summary: "submit feedback on past tool executions or model completions",
			Detail: "Attaches quality scores and reasoning to a specific interaction trace ID, " +
				"dynamically tuning future semantic routing and tool recommendation rankings.",
		},
		{
			Name: "interactions", Group: "inspection",
			Usage:   "[--q <query>] [--source <source>] [--feedback yes|no] [--show <trace-id>]",
			Summary: "search, filter, and inspect interaction history and feedback",
			Detail: "Searches past inputs, model outputs, tools used, and recorded feedback scores. " +
				"Enables discovering unrated queries or inspecting full input/output logs.",
		},
		{
			Name: "log", Aliases: []string{"logs"}, Group: "inspection",
			Usage:   "[--since d] [--chain id] [--follow] | sql '<query>' | record '<json>'",
			Summary: "query the structured log, or add to it",
			Detail: "--chain walks a record back through its parents, which is how a " +
				"tool call is traced to the daemon that started the server that " +
				"served it. `log sql` runs read-only SQL for anything the flags " +
				"do not cover. `log record` appends, so mcpx's log can hold what " +
				"the harness knows and one `mcpx stats` covers both.",
			Examples: []string{
				"mcpx log --since 1h --level warn",
				"mcpx log --chain cal-d13e4c64102e9113",
				`mcpx log record '{"event":"deploy","version":"1.2.0"}'`,
			},
		},
		{
			Name: "stats", Group: "inspection",
			Usage:   "[calls|servers|errors|sessions|volume|slowest]",
			Summary: "aggregate the log into numbers",
		},
		{
			Name: "help", Group: "configuration",
			Local: "Text about the binary, produced by the binary.",
			Usage: "[command]", Summary: "show this help, or help for one command",
		},
		{
			Name: "man", Group: "configuration",
			Local:   "Text about the binary, produced by the binary.",
			Usage:   "[--install dir]",
			Summary: "print the manual page, or install it",
			Detail: "Generated from the command table and the setting registry the " +
				"program runs on, so it cannot describe a flag that does not exist.",
		},
		{
			Name: "completion", Group: "configuration",
			Local:   "Text about the binary, produced by the binary.",
			Usage:   "bash|zsh|fish",
			Summary: "print a shell completion script",
			Examples: []string{
				"source <(mcpx completion bash)",
				"mcpx completion fish > ~/.config/fish/completions/mcpx.fish",
			},
		},
		{
			Name: "serve", Group: "daemon",
			Local:   "It is the MCP surface: a stdio shim over the daemon, which serves the same tools at `/mcp` itself.",
			Usage:   "[--mode code-mode|reactive|full] [--tools]",
			Summary: "speak MCP over stdio, for a host that spawns its servers",
			Detail: "A thin shim over the daemon: every tool it lists is answered by " +
				"the daemon, which is started if it is not running. HTTP is served " +
				"by the daemon itself at /mcp; this command exists because an MCP " +
				"host starts servers by spawning a process. --tools prints the " +
				"tool list and exits.",
			Examples: []string{
				`{"mcpServers":{"mcpx":{"command":"mcpx","args":["serve"]}}}`,
				"mcpx serve --tools",
			},
		},
		{
			Name: "openapi", Group: "discovery",
			Usage:   "[-o file]",
			Summary: "print an OpenAPI document for mcpx",
		},
		{
			Name: "adapter", Group: "configuration",
			Local:   "Adapter declarations are files the CLI reads; the daemon serves each adapter as a server of its own, so its tools reach every tool operation (call, types, search, catalog) like any upstream's.",
			Usage:   "[list|check|call|tools|serve] [<name>.<tool> '<json>' | <name> [file...]]",
			Summary: "command-line programs declared as MCP tools",
			Detail: "An adapter file, named by paths.adapters, declares a program and " +
				"the tools it offers. list shows them, check says whether each " +
				"program is installed, call runs one tool, and tools prints the " +
				"tool definitions an MCP host would see. Every adapter is also a " +
				"namespace: the daemon runs it as `mcpx adapter serve <name>`, " +
				"so it is in mcpx ls, types, search, catalog and the generated " +
				"client, and a script calls it as <name>.<tool>({...}).",
			Examples: []string{
				"mcpx adapter check",
				`mcpx adapter call jq.run '{"filter":"."}'`,
			},
		},
		{
			Name: "registry", Group: "configuration",
			Usage:   "[search|show|add] <query|name> [--limit N] [--write]",
			Summary: "find servers in a public registry and add them",
			Examples: []string{
				"mcpx registry search github",
				"mcpx registry add io.github.example/server --write",
			},
		},
		{
			Name: "api", Group: "configuration",
			Local:   "OpenAPI declarations are files the CLI reads; the tools they produce are served over MCP, and are not yet a tool source the daemon owns (#83).",
			Usage:   "[list|tools|call] [--spec <path-or-url>] [<tool> '<json>']",
			Summary: "HTTP services described by OpenAPI, as tools",
			Detail: "Declarations named by paths.apis, or one given with --spec, are " +
				"turned into a tool per operation: read-only methods by default, " +
				"every method with --methods all.",
			Examples: []string{
				"mcpx api tools --spec https://example.com/openapi.json",
			},
		},
	}
}

// ManPage renders a roff man page.
//
// Generated from the same command table and setting registry the program
// uses, so it cannot describe a flag that does not exist or miss one that
// does. A hand-written man page is wrong within two releases; this one is
// wrong only if the code is.
func ManPage(version string) string {
	var b strings.Builder
	date := time.Now().UTC().Format("2006-01-02")

	fmt.Fprintf(&b, ".TH MCPX 1 %q %q \"mcpx manual\"\n", date, "mcpx "+version)
	b.WriteString(".SH NAME\nmcpx \\- expose MCP servers to the shell\n")

	b.WriteString(".SH SYNOPSIS\n.B mcpx\n[\\fIglobal-flags\\fR]\n\\fIcommand\\fR\n[\\fIargs\\fR]\n.br\n")
	b.WriteString(".B mcpx\n\\fIscript.ts\\fR\n[\\fIargs\\fR]\n.br\n")
	b.WriteString(".B mcpx\n\\fB'\\fIsource\\fB'\\fR\n")

	b.WriteString(".SH DESCRIPTION\n")
	b.WriteString("mcpx runs MCP servers as a pool behind a daemon and generates a\n" +
		"TypeScript client for them, so a script can call a tool as an ordinary\n" +
		"function. The point is that tool schemas never enter a model's context:\n" +
		"an agent writes a script against the generated types and reads back only\n" +
		"what the script chose to emit.\n.PP\n")
	b.WriteString("A first argument that is a file, or that obviously contains source,\n" +
		"is run without needing \\fBrun\\fR or \\fBexec\\fR.\n")

	for _, g := range commandGroups {
		fmt.Fprintf(&b, ".SH %s\n", strings.ToUpper(commandGroupTitles[g]))
		for _, c := range Commands() {
			if c.Group != g {
				continue
			}
			usage := c.Usage
			if usage != "" {
				usage = " " + usage
			}
			fmt.Fprintf(&b, ".TP\n.B mcpx %s%s\n%s\n", c.Name, roffEscape(usage), roffEscape(c.Summary))
			if len(c.Aliases) > 0 {
				fmt.Fprintf(&b, ".br\nAlso: %s\n", strings.Join(c.Aliases, ", "))
			}
			if c.Detail != "" {
				fmt.Fprintf(&b, ".RS\n.PP\n%s\n.RE\n", roffEscape(c.Detail))
			}
			for _, ex := range c.Examples {
				fmt.Fprintf(&b, ".RS\n.PP\n.EX\n%s\n.EE\n.RE\n", roffEscape(ex))
			}
		}
	}

	sch, err := settings.New(settings.Registry())
	if err == nil {
		b.WriteString(".SH SETTINGS\n")
		b.WriteString("Every setting below is readable from a configuration file at its\n" +
			"dotted path, from the environment variable shown, and from the flag\n" +
			"shown. Precedence runs defaults, then configuration files with the\n" +
			"nearest winning, then the environment, then the command line.\n")
		section := ""
		for _, set := range sch.All() {
			if set.Plumbing {
				continue
			}
			if head := strings.SplitN(set.Path, ".", 2)[0]; head != section {
				section = head
				fmt.Fprintf(&b, ".SS %s\n", strings.ToUpper(section))
			}
			fmt.Fprintf(&b, ".TP\n.B %s\n%s\n", roffEscape(set.Path), roffEscape(set.Short))
			fmt.Fprintf(&b, ".br\n\\fB--%s\\fR, \\fB%s\\fR, default \\fB%s\\fR\n",
				roffEscape(set.FlagName()), roffEscape(set.EnvName()),
				roffEscape(defaultOrEmpty(set.Default)))
			if len(set.Enum) > 0 {
				fmt.Fprintf(&b, ".br\nOne of: %s\n", roffEscape(set.EnumWords()))
			}
			if set.Long != "" {
				fmt.Fprintf(&b, ".RS\n.PP\n%s\n.RE\n", roffEscape(set.Long))
			}
		}

		b.WriteString(".SH PLUMBING\n")
		b.WriteString("These control internals. They work, and they are listed so that\n" +
			"nobody has to patch the binary to get past a decision that was never\n" +
			"meant to be final, but there is no ordinary reason to change one.\n")
		for _, set := range sch.All() {
			if !set.Plumbing {
				continue
			}
			fmt.Fprintf(&b, ".TP\n.B %s\n%s\n.br\n\\fB--%s\\fR, \\fB%s\\fR, default \\fB%s\\fR\n",
				roffEscape(set.Path), roffEscape(set.Short),
				roffEscape(set.FlagName()), roffEscape(set.EnvName()),
				roffEscape(defaultOrEmpty(set.Default)))
		}
	}

	b.WriteString(".SH LAUNCHER\n")
	b.WriteString("A script runs inside a generated launcher. \\fB--launcher\\fR replaces\n" +
		"it with source or a file, and \\fB--no-launcher\\fR removes it entirely.\n" +
		"A replacement may use these placeholders:\n")
	for _, p := range []struct{ name, what string }{
		{"@header", "the client import, and the script and result objects"},
		{"@globals", "install log, emit, tools and the namespaces"},
		{"@console", "patch the console, subject to script.captureConsole"},
		{"@import", "the dynamic import alone"},
		{"@entry", "the import and the call to the entry point"},
		{"@before", "the configured before phase"},
		{"@prefix", "the configured prefix phase"},
		{"@onSuccess", "the configured success hook"},
		{"@onError", "the configured error hook; the error still propagates"},
		{"@suffix", "the configured suffix phase"},
	} {
		fmt.Fprintf(&b, ".TP\n.B %s\n%s\n", p.name, roffEscape(p.what))
	}
	b.WriteString(".PP\nFills may reference each other, so a suffix ending in \\fB@prefix\\fR\n" +
		"runs the prefix again. A reference loop is refused with the path that\n" +
		"closed it. A placeholder resolving twice is refused unless named in\n" +
		"\\fBplumbing.launcherPlaceholderRepeat\\fR.\n")

	b.WriteString(".SH FILES\n")
	for _, f := range []struct{ path, what string }{
		{".mcpx.json", "configuration, nearest wins, merged up the tree"},
		{".config/mcpx/config.json", "the same, spelled more explicitly"},
		{".config/mcpx/scripts/", "named scripts"},
		{"$MCPX_STATE_DIR/logs/", "rotated JSONL logs"},
	} {
		fmt.Fprintf(&b, ".TP\n.B %s\n%s\n", roffEscape(f.path), roffEscape(f.what))
	}

	b.WriteString(".SH NOTES\n")
	b.WriteString("Writes made through \\fBDeno.stdout.write\\fR and \\fBDeno.stderr.write\\fR\n" +
		"are not captured. A script reaching for raw bytes has asked for raw\n" +
		"bytes, and wrapping them in records would be the wrong answer; use\n" +
		"\\fBconsole\\fR or \\fBlog\\fR for anything meant to be recorded.\n")

	return b.String()
}

func defaultOrEmpty(v string) string {
	if v == "" {
		return "(empty)"
	}
	return v
}

// roffEscape protects the few characters roff treats specially. A backslash
// in a default value would otherwise silently eat the next character.
func roffEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\e`)
	s = strings.ReplaceAll(s, "-", `\-`)
	// A line starting with a dot or a quote is a roff request.
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, ".") || strings.HasPrefix(l, "'") {
			lines[i] = `\&` + l
		}
	}
	return strings.Join(lines, "\n")
}

// commandGroups orders the listing in help and the man page. A group
// missing here would drop its commands from both, which is why a test checks
// every command's group is one of these.
var commandGroups = []string{"running", "discovery", "daemon", "configuration", "inspection", "operations"}

var commandGroupTitles = map[string]string{
	"running":       "running code",
	"discovery":     "finding tools",
	"daemon":        "the daemon",
	"configuration": "configuration",
	"inspection":    "inspection",
	"operations":    "daemon operations",
}

// CommandsByGroup is the ordered listing used by help.
func CommandsByGroup() map[string][]Command {
	out := map[string][]Command{}
	for _, c := range Commands() {
		out[c.Group] = append(out[c.Group], c)
	}
	for g := range out {
		sort.SliceStable(out[g], func(i, j int) bool { return out[g][i].Name < out[g][j].Name })
	}
	return out
}
