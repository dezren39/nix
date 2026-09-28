package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"strings"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/daemon"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/logging"
	"github.com/dezren39/mcpx/internal/pool"
)

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// CmdDaemon runs the daemon in the foreground.
func (a *App) CmdDaemon(ctx context.Context, args []string) error {
	fs := newFlagSet("daemon")
	cfgPath := fs.String("config", a.ConfigPath, "config file")
	port := fs.Int("port", 0, "loopback TCP port for script clients (0 = ephemeral)")
	detached := fs.Bool("detached", false, "internal: started in the background by the CLI")
	warm := fs.Bool("warm", true, "read every server's schemas in the background at startup")
	idleExit := fs.Duration("idle-exit", 0, "exit after this long with no requests and no live instances (0 = never)")
	format := fs.String("format", "", "log rendering: text, json, json-pretty, logfmt, compact, bare")
	include := fs.String("include", "", "ambient blocks on lifecycle records: host, user, process, network, version, env, all, none")
	logDir := fs.String("log-dir", "", "durable log directory (default: the state directory)")
	level := fs.String("log-level", "", "minimum level: debug, info, warn, error")
	logSource := newOptional("all")
	fs.Var(logSource, "log-source",
		"levels that record a call site: bare for all, or a level name, or false")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	pool.Version = a.Version

	// The daemon keys its socket to the config it loaded, matching what the
	// CLI computes, so a config edit yields a new daemon rather than a stale
	// one answering with the previous servers.
	paths := a.Paths
	if len(cfg.Sources) > 0 {
		paths = paths.ForConfig(daemon.FingerprintConfig(cfg.Sources))
	}

	logFormat, err := logging.ParseFormat(
		firstSet(*format, os.Getenv("MCPX_FORMAT"), cfg.Logging.Format))
	if err != nil {
		return err
	}
	minLevel, err := logging.ParseLevel(
		firstSet(*level, os.Getenv("MCPX_LOG_LEVEL"), cfg.Logging.Level))
	if err != nil {
		return err
	}
	// The daemon and scripts render through the same writer, so one --format
	// governs everything a user sees rather than each surface having its own.
	writer := logging.NewWriter(os.Stderr, logFormat, minLevel)
	// The daemon is a thing that starts and ends, so it gets a trace that
	// every record beneath it carries.
	daemonTrace := logging.NewTraceID("dmn")
	dir := firstSet(*logDir, os.Getenv("MCPX_LOG_DIR"), cfg.Logging.Dir,
		filepath.Join(paths.State, "logs"))
	sink, serr := logging.NewFileSink(logging.FileOptions{Dir: dir})
	if serr != nil {
		// A durable log is a convenience; losing it must not stop the daemon.
		fmt.Fprintf(os.Stderr, "mcpx: durable log unavailable (%v)\n", serr)
	} else {
		writer = writer.WithFile(sink, slog.LevelDebug)
		defer sink.Close()
	}
	writer = writer.WithBase(map[string]any{logging.KeyTrace: string(daemonTrace)})

	handler := logging.NewSlogHandler(writer).WithSourceLevel(
		logging.SourceLevel(firstSet(logSource.Value(), os.Getenv("MCPX_LOG_SOURCE"), cfg.Logging.Source)))
	logger := slog.NewLogLogger(handler, slog.LevelInfo)
	srv, err := daemon.NewServer(daemon.Options{
		Config:   cfg,
		Paths:    paths,
		Version:  a.Version,
		Logger:   logger,
		IdleExit: *idleExit,
	})
	if err != nil {
		return err
	}
	blocks := logging.ParseIncludes(firstSet(*include, os.Getenv("MCPX_INCLUDE"), cfg.Logging.Include))
	// The opening record carries everything about the environment, so later
	// records can carry only a trace id and still be resolvable.
	start := logging.Ambient(a.Version, blocks)
	start[logging.KeyEvent] = "daemon.start"
	start["config"] = cfg.Path
	start["servers"] = len(cfg.MCPServers)
	if sink != nil {
		start["log.file"] = sink.Path()
	}
	logStructured(writer, slog.LevelInfo, "daemon starting", start)

	// Server lifecycle is reported by the pools; give each event the daemon as
	// its parent so the tree closes. An event that already named its own
	// parent keeps it: a tool call belongs to the instance that served it, and
	// re-pointing it at the daemon would flatten the chain to two levels.
	pool.Lifecycle = func(event string, attrs map[string]any) {
		attrs[logging.KeyEvent] = event
		if p, _ := attrs[logging.KeyParent].(string); p == "" {
			attrs[logging.KeyParent] = string(daemonTrace)
		}
		logStructured(writer, slog.LevelDebug, event, attrs)
	}

	if err := srv.Listen(*port); err != nil {
		return err
	}
	defer func() {
		stop := map[string]any{logging.KeyEvent: "daemon.stop"}
		logStructured(writer, slog.LevelInfo, "daemon stopping", stop)
	}()
	if *detached {
		logger.Printf("started detached")
	}
	if *warm {
		srv.WarmAsync()
	}
	return srv.Serve(ctx)
}

// CmdConfig prints the resolved configuration.
func (a *App) CmdConfig(ctx context.Context, args []string) error {
	fs := newFlagSet("config")
	showPath := fs.Bool("path", false, "print only the nearest config file path")
	showSources := fs.Bool("sources", false, "show every file that contributed, and which defined each server")
	showDefaults := fs.Bool("defaults", false, "print the built-in default layer that underlies every config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// The base layer is data, so it can be shown. A default nobody can print
	// is a magic number with extra steps.
	if *showDefaults {
		fmt.Println(strings.TrimRight(string(defaults.BuiltinJSON()), "\n"))
		return nil
	}
	cfg, err := config.Load(a.ConfigPath)
	if err != nil {
		return err
	}
	if *showPath {
		if cfg.Path == "" {
			return fmt.Errorf("no config file found; searched:\n  %s",
				joinLines(config.SearchPath()))
		}
		fmt.Println(cfg.Path)
		return nil
	}
	if *showSources {
		return a.configSources(cfg)
	}
	servers, err := cfg.ResolveAll()
	if err != nil {
		return err
	}
	type row struct {
		Name        string `json:"name"`
		Namespace   string `json:"namespace"`
		Sharing     string `json:"sharing"`
		Scope       string `json:"scope"`
		Max         int    `json:"max"`
		Transport   string `json:"transport"`
		Command     string `json:"command,omitempty"`
		URL         string `json:"url,omitempty"`
		IdleTimeout string `json:"idleTimeout"`
		CallTimeout string `json:"callTimeout"`
	}
	out := struct {
		Path    string   `json:"path"`
		Sources []string `json:"sources,omitempty"`
		Servers []row    `json:"servers"`
	}{Path: cfg.Path, Sources: cfg.Sources}
	for _, s := range servers {
		r := row{
			Name: s.Name, Namespace: s.Namespace,
			Sharing: string(s.Sharing), Scope: string(s.Scope), Max: s.Max,
			IdleTimeout: s.IdleTimeout.String(), CallTimeout: s.CallTimeout.String(),
		}
		if s.Stdio() {
			r.Transport, r.Command = "stdio", s.Command
		} else {
			r.Transport, r.URL = s.Transport, s.URL
		}
		out.Servers = append(out.Servers, r)
	}
	return a.out(out)
}

// configSources shows the merge, because a merge nobody can inspect is worse
// than no merge: a server appearing from a parent directory is otherwise
// indistinguishable from one you forgot you wrote.
func (a *App) configSources(cfg *config.Config) error {
	type row struct {
		Server string `json:"server"`
		From   string `json:"from"`
	}
	out := struct {
		Sources []string `json:"sources"`
		Servers []row    `json:"servers"`
	}{Sources: cfg.Sources}

	names := make([]string, 0, len(cfg.MCPServers))
	for n := range cfg.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out.Servers = append(out.Servers, row{Server: n, From: cfg.Origin[n]})
	}
	if a.JSON {
		return a.out(out)
	}

	if len(out.Sources) == 0 {
		fmt.Println("No config files found. Searched:")
		for _, p := range config.SearchPath() {
			fmt.Println("  " + p)
		}
		return nil
	}
	fmt.Println("Files, nearest first:")
	for i, p := range out.Sources {
		marker := " "
		if i == 0 {
			marker = "*"
		}
		fmt.Printf(" %s %s\n", marker, p)
	}
	if len(out.Servers) == 0 {
		return nil
	}
	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SERVER\tDEFINED IN")
	for _, r := range out.Servers {
		fmt.Fprintf(tw, "%s\t%s\n", r.Server, r.From)
	}
	tw.Flush()
	return nil
}

// logStructured emits a record with attributes, bypassing slog's
// key/value pairing for a map that is already assembled.
func logStructured(w *logging.Writer, level slog.Level, msg string, attrs map[string]any) {
	w.Write(logging.Record{Time: time.Now(), Level: level, Msg: msg, Attrs: attrs})
}

// firstSet returns the first non-empty value.
func firstSet(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func joinLines(ss []string) string {
	s := ""
	for i, v := range ss {
		if i > 0 {
			s += "\n  "
		}
		s += v
	}
	return s
}

// CmdHelp prints usage.
func (a *App) CmdHelp(context.Context, []string) error {
	fmt.Print(usage)
	return nil
}

const usage = `mcpx - run TypeScript against your MCP servers from the command line

  A local daemon owns every MCP server process. Scripts import a generated,
  fully typed client and call tools as ordinary async functions, so only the
  result you print reaches the model's context.

DISCOVERY (answered from cache; starts no servers)
  mcpx ls                        list namespaces and tool counts
  mcpx search <query>            find a tool by name or description
  mcpx types <ns>[,<ns>...]      TypeScript signatures for those namespaces
  mcpx catalog [--budget N]      every namespace, signatures fitted to a budget

RUNNING
  mcpx exec '<typescript>'       run a snippet
  mcpx run <name|file.ts> [args] run a named script from .mcpx/scripts, or a file
  mcpx scripts                   list named scripts mcpx can run
  mcpx call <ns>.<tool> '<json>' one-shot call, no JavaScript runtime involved
  mcpx client -o <path>          write the typed client for a checked-in script

MANAGEMENT
  mcpx status [-v]               daemon, pools and live instances
  mcpx refresh                   re-read every server's tool schemas
  mcpx restart [<ns>]            stop instances; the next call starts fresh ones
  mcpx stop [--all]              shut the daemon down (--all: every config's)
  mcpx daemons                   list every running daemon
  mcpx daemon [--port N]         run the daemon in the foreground
  mcpx config [--path|--sources] show the resolved configuration and where it came from
  mcpx init [--global]           write a starter config

DIAGNOSTICS
  mcpx log [--since 1h] [-f]     query the durable log
  mcpx log --chain <trace>       a call and everything that led to it, as a tree
  mcpx log sql '<select ...>'    raw read-only SQL over the log index
  mcpx stats [calls|servers|errors|sessions|volume|slowest|instances]

GLOBAL FLAGS
  --config <path>                config file (default: search up from $PWD)
  --json                         machine-readable output
  --profile <name>[,<name>]      include servers in these profiles as well
  --skip-default                 with --profile, exclude the usual default set
  --all-profiles                 every configured server, ignoring profiles
  --version

CONCURRENCY
  Each server declares a mode in its config:
    shared   one process, many concurrent callers   (search, docs, databases)
    pooled   up to N processes, one per call        (stateless but expensive)
    session  up to N processes, one per script run  (browsers and other
                                                     stateful servers)
  In session mode a run holds its own process for its whole lifetime, so two
  agents driving Chrome at once get two browsers rather than corrupting one.

EXAMPLES
  mcpx ls
  mcpx types fff,codedb
  mcpx exec 'const r = await fff.search({ query: "handleCall" }); console.log(r)'
  mcpx call chrome_devtools.navigate_page '{"url":"https://example.com"}'
`
