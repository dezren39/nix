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
}

// Commands is every subcommand mcpx has.
func Commands() []Command {
	return []Command{
		{
			Name: "run", Group: "running",
			Usage:   "[flags] <script|file> [args...]",
			Summary: "run a named script or a file",
			Detail: "Resolves a bare name along the script search path, or takes a " +
				"path directly. The script is wrapped in a launcher that installs " +
				"the tool bindings, captures the console and runs the configured " +
				"phases; --no-launcher skips all of that.",
			Examples: []string{
				"mcpx run report",
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
			Summary: "list every script on the search path",
			Detail: "Shows the name, where it was found, and its first comment line. " +
				"A script shadowed by a nearer one of the same name is listed too, " +
				"marked, because otherwise the shadowing is invisible.",
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
			Usage: "<words>", Summary: "find tools by name and description",
		},
		{
			Name: "call", Group: "discovery",
			Usage:   "<namespace>.<tool> [json]",
			Summary: "call one tool directly, without writing a script",
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
			Summary: "list every mcpx daemon on this machine",
		},
		{
			Name: "refresh", Group: "daemon",
			Summary: "re-read tool schemas from every server",
		},
		{
			Name: "restart", Group: "daemon",
			Usage: "[server...]", Summary: "restart servers, or all of them",
		},
		{
			Name: "stop", Group: "daemon",
			Usage: "[--all]", Summary: "stop the daemon for this configuration, or all of them",
		},
		{
			Name: "daemon", Group: "daemon",
			Usage:   "[--detached]",
			Summary: "run the daemon in the foreground",
			Detail: "Normally the daemon is started on demand. Running it in the " +
				"foreground is for watching what it does, or for supervising it " +
				"with something else.",
		},
		{
			Name: "config", Group: "configuration",
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
			Summary: "write a starter configuration file",
		},
		{
			Name: "schema", Group: "discovery",
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
				"twenty minutes later. A call that is waiting exits 75, which is " +
				"EX_TEMPFAIL, and prints the question and the command that answers " +
				"it.",
			Examples: []string{
				"mcpx elicit list",
				"mcpx elicit answer elc-9f2c1a84 repo=me/thing",
				"mcpx elicit decline elc-9f2c1a84",
			},
		},
		{
			Name: "doctor", Group: "inspection",
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
			Summary: "browse namespaces, tools and the log interactively",
			Detail: "Everything it shows is available from other commands; it exists " +
				"because discovery is a loop, and running four commands with " +
				"different flags to go round it once is enough friction that " +
				"people guess instead. Every screen prints the command that " +
				"produced it, so it teaches its own scriptable form.",
		},
		{
			Name: "log", Group: "inspection",
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
			Usage: "[command]", Summary: "show this help, or help for one command",
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

	groups := []string{"running", "discovery", "daemon", "configuration", "inspection"}
	titles := map[string]string{
		"running":       "RUNNING CODE",
		"discovery":     "FINDING TOOLS",
		"daemon":        "THE DAEMON",
		"configuration": "CONFIGURATION",
		"inspection":    "INSPECTION",
	}
	for _, g := range groups {
		fmt.Fprintf(&b, ".SH %s\n", titles[g])
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
				fmt.Fprintf(&b, ".br\nOne of: %s\n", roffEscape(strings.Join(set.Enum, ", ")))
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
