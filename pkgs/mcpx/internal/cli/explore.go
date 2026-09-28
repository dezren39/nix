package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/dezren39/mcpx/internal/logstore"
)

// CmdExplore is an interactive browser for a local human.
//
// Everything it shows is available from other commands. The reason it exists
// is that discovery is a loop -- list namespaces, look at one, read a tool's
// signature, try it, look at what the log said -- and running four commands
// with different flags to go round that loop once is enough friction that
// people stop doing it and guess instead.
//
// Deliberately not a full-screen application. A pane-and-cursor interface
// would need a terminal library, raw mode, resize handling and a redraw loop,
// and would lose the two things a plain prompt gives for free: the output
// stays in scrollback where it can be copied, and every screen corresponds to
// a command that can be run directly and put in a script. The footer prints
// that command, so the tool teaches its own non-interactive form.
type explorer struct {
	app *App
	ctx context.Context
	in  *bufio.Scanner
}

func (a *App) CmdExplore(ctx context.Context, args []string) error {
	fs := newFlagSet("explore")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if !isTerminal(os.Stdin) {
		return fmt.Errorf("explore needs a terminal; for scripting use ls, types, catalog or log")
	}
	e := &explorer{app: a, ctx: ctx, in: bufio.NewScanner(os.Stdin)}
	return e.run(fs.Args())
}

func (e *explorer) run(args []string) error {
	fmt.Println("mcpx explore - ? for help, q to quit")
	if len(args) > 0 {
		e.dispatch(strings.Join(args, " "))
	} else {
		e.namespaces()
	}
	for {
		fmt.Print("\nmcpx> ")
		if !e.in.Scan() {
			fmt.Println()
			return nil
		}
		line := strings.TrimSpace(e.in.Text())
		switch line {
		case "":
			continue
		case "q", "quit", "exit":
			return nil
		}
		e.dispatch(line)
	}
}

func (e *explorer) dispatch(line string) {
	word, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)
	switch word {
	case "?", "h", "help":
		e.help()
	case "ns", "namespaces", "ls":
		e.namespaces()
	case "t", "types":
		e.types(rest)
	case "s", "search":
		e.search(rest)
	case "c", "catalog":
		e.catalog(rest)
	case "l", "log":
		e.log(rest)
	case "chain":
		e.chain(rest)
	case "stats":
		e.stats(rest)
	case "status":
		e.shell("status")
	case "run", "x", "exec":
		e.exec(rest)
	default:
		// A bare word that names a namespace is the thing somebody means
		// nine times in ten, and making them type "types" first is friction
		// for no gain.
		e.types(line)
	}
}

func (e *explorer) help() {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	for _, row := range [][2]string{
		{"ns", "list namespaces and tool counts"},
		{"<namespace>", "signatures for that namespace"},
		{"t <ns>[.<tool>]", "the same, explicitly"},
		{"s <words>", "search tools by name and description"},
		{"c [budget]", "catalog, fitted to a token budget"},
		{"x <typescript>", "run a snippet against the servers"},
		{"l [n]", "the last n log records"},
		{"chain <trace>", "a call and everything that led to it"},
		{"stats [dim]", "aggregate the log"},
		{"status", "daemon, pools and live instances"},
		{"q", "quit"},
	} {
		fmt.Fprintf(w, "  %s\t%s\n", row[0], row[1])
	}
	_ = w.Flush()
	fmt.Println("\n  Every screen prints the command that produced it.")
}

// echo prints the non-interactive equivalent, so the tool teaches its own
// scriptable form rather than being a place where knowledge stops.
func (e *explorer) echo(args ...string) {
	fmt.Printf("\n\033[2m$ mcpx %s\033[0m\n", strings.Join(args, " "))
}

func (e *explorer) shell(args ...string) {
	e.echo(args...)
	cmd := exec.CommandContext(e.ctx, os.Args[0], args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "mcpx:", err)
	}
}

func (e *explorer) namespaces() { e.shell("ls") }

func (e *explorer) types(arg string) {
	if arg == "" {
		e.namespaces()
		return
	}
	e.shell("types", arg)
}

func (e *explorer) search(arg string) {
	if arg == "" {
		fmt.Println("  s <words>")
		return
	}
	e.shell(append([]string{"search"}, strings.Fields(arg)...)...)
}

func (e *explorer) catalog(arg string) {
	if n, err := strconv.Atoi(strings.TrimSpace(arg)); err == nil {
		e.shell("catalog", "--budget", strconv.Itoa(n))
		return
	}
	if arg != "" {
		e.shell(append([]string{"catalog", "--bias"}, arg)...)
		return
	}
	e.shell("catalog")
}

func (e *explorer) exec(arg string) {
	if arg == "" {
		fmt.Println("  x <typescript>")
		return
	}
	e.shell("exec", arg)
}

func (e *explorer) log(arg string) {
	n := "20"
	if v := strings.TrimSpace(arg); v != "" {
		if _, err := strconv.Atoi(v); err == nil {
			n = v
		} else {
			e.shell("log", "--grep", v, "--limit", "40")
			return
		}
	}
	e.shell("log", "--limit", n)
}

func (e *explorer) chain(arg string) {
	if arg == "" {
		// Offer the most recent calls rather than demanding an id nobody has
		// memorised. Needing to run another command to find the argument for
		// this one is exactly the friction this is here to remove.
		e.recentTraces()
		return
	}
	e.shell("log", "--chain", arg)
}

func (e *explorer) recentTraces() {
	st, err := e.app.openStore("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcpx:", err)
		return
	}
	defer st.Close()
	recs, err := st.Records(logstore.Query{Event: "mcp.call", Limit: 10})
	if err != nil || len(recs) == 0 {
		fmt.Println("  no calls recorded yet")
		return
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Time.After(recs[j].Time) })
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  TRACE\tWHEN\tSERVER.TOOL\tMS")
	for _, r := range recs {
		fmt.Fprintf(w, "  %s\t%s\t%s.%s\t%s\n",
			attrString(r.Attrs, "trace"), r.Time.Format("15:04:05"),
			attrString(r.Attrs, "server"), attrString(r.Attrs, "tool"),
			attrString(r.Attrs, "durationMs"))
	}
	_ = w.Flush()
	fmt.Println("\n  chain <trace>")
}

// attrString reads one attribute without caring how the store typed it.
// Numbers arrive as float64 through JSON and as int64 from SQLite, and a
// type switch at every call site would be noise.
func attrString(attrs map[string]any, key string) string {
	v, ok := attrs[key]
	if !ok || v == nil {
		return "-"
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', 1, 64)
	}
	return fmt.Sprint(v)
}

func (e *explorer) stats(arg string) {
	if arg == "" {
		arg = "calls"
	}
	e.shell(append([]string{"stats"}, strings.Fields(arg)...)...)
}
