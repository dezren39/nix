package cli

import (
	"context"
	"errors"
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
	"github.com/dezren39/mcpx/internal/settings"
)

// newFlagSet creates a command's flag set and registers every setting the
// registry declares for it.
//
// Binding here rather than at each call site is what keeps `mcpx config
// --schema` honest: a setting listed for a command is a flag that command
// accepts. Hand-written flags are registered first by the caller... except
// they are not, because the caller declares them after this returns. So the
// registry flags are bound lazily, at parse time, once the hand-written ones
// exist and can be skipped.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	flagSetCommand[fs] = name
	return fs
}

// flagSetCommand remembers which command a flag set belongs to, so that
// parseFlags can bind the right settings without every call site repeating
// the name it already gave newFlagSet.
var flagSetCommand = map[*flag.FlagSet]string{}

// parseFlags parses, then folds what was given into the resolved settings.
//
// This replaces a bare fs.Parse so that registry-declared flags are both
// accepted and applied. Splitting bind from parse is not optional: Go panics
// on duplicate registration, and the hand-written flags are declared between
// newFlagSet and here.
func parseFlags(a *App, fs *flag.FlagSet, args []string) error {
	cmd := flagSetCommand[fs]
	apply := func() error { return nil }
	if a != nil && cmd != "" {
		apply = a.BindFlags(fs, cmd)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	delete(flagSetCommand, fs)
	return apply()
}

// CmdDaemon runs the daemon in the foreground.
func (a *App) CmdDaemon(ctx context.Context, args []string) error {
	fs := newFlagSet("daemon")
	cfgPath := fs.String("config", a.ConfigPath, "config file")
	detached := fs.Bool("detached", false, "internal: started in the background by the CLI")
	format := fs.String("format", "", "log rendering: text, json, json-pretty, logfmt, compact, bare")
	include := fs.String("include", "", "ambient blocks on lifecycle records: host, user, process, network, version, env, all, none")
	logDir := fs.String("log-dir", "", "durable log directory (default: the state directory)")
	level := fs.String("log-level", "", "minimum level: debug, info, warn, error")
	logSource := newOptional("all")
	fs.Var(logSource, "log-source",
		"levels that record a call site: bare for all, or a level name, or false")
	if err := parseFlags(a, fs, args); err != nil {
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
		Sink:     sink,
		Config:   cfg,
		Paths:    paths,
		Version:  a.Version,
		Logger:   logger,
		IdleExit: a.Settings().Duration("daemon.idleExit"),
		Settings: a.Settings(),
	})
	if err != nil {
		return err
	}
	srv.Address = a.Settings().String("daemon.address")
	if h := srv.Address; h != "" && h != "127.0.0.1" && h != "localhost" {
		// Said once, loudly. The API is unauthenticated, so whoever can
		// route to this port can run tools as this user, and that should be
		// a sentence somebody read rather than a surprise.
		fmt.Fprintf(os.Stderr,
			"mcpx: listening on %s; the API is unauthenticated, so anything that "+
				"can reach this port can run tools as you\n", h)
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
		// The same events reach live subscribers. The log is for what
		// happened; the stream is for what is happening, and a hook that
		// has to poll the log to find out has already missed the moment.
		srv.PublishLifecycle(event, attrs)
	}

	if err := srv.Listen(a.Settings().Int("daemon.port")); err != nil {
		return err
	}
	defer func() {
		stop := map[string]any{logging.KeyEvent: "daemon.stop"}
		logStructured(writer, slog.LevelInfo, "daemon stopping", stop)
	}()
	if *detached {
		logger.Printf("started detached")
	}
	if a.Settings().Bool("daemon.warm") {
		srv.WarmAsync()
	}
	return srv.Serve(ctx)
}

// CmdMan prints the manual page.
//
// Generated rather than written, from the same command table and setting
// registry the program itself uses. A hand-written man page is wrong within
// two releases; this one is wrong only if the code is.
func (a *App) CmdMan(ctx context.Context, args []string) error {
	fs := newFlagSet("man")
	install := fs.String("install", "", "write the page into this directory as man1/mcpx.1")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	page := ManPage(a.Version)
	if *install == "" {
		fmt.Print(page)
		return nil
	}
	dir := filepath.Join(*install, "man1")
	if err := os.MkdirAll(dir, defaults.PublicDirMode); err != nil {
		return err
	}
	path := filepath.Join(dir, "mcpx.1")
	if err := os.WriteFile(path, []byte(page), defaults.PublicMode); err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}

// CmdCompletion prints a shell completion script.
func (a *App) CmdCompletion(_ context.Context, args []string) error {
	shell := ""
	if len(args) > 0 {
		shell = args[0]
	}
	if shell == "" {
		return errors.New("usage: mcpx completion <bash|zsh|fish>")
	}
	text, err := Completion(shell)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

// CmdConfig prints the resolved configuration.
func (a *App) CmdConfig(ctx context.Context, args []string) error {
	fs := newFlagSet("config")
	showPath := fs.Bool("path", false, "print only the nearest config file path")
	showSources := fs.Bool("sources", false, "show every file that contributed, and which defined each server")
	showDefaults := fs.Bool("defaults", false, "print the built-in default layer that underlies every config")
	showSchema := fs.Bool("schema", false, "print every setting, with its flag and variable")
	withPlumbing := fs.Bool("plumbing", false, "include internal settings in --schema")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	// The base layer is data, so it can be shown. A default nobody can print
	// is a magic number with extra steps.
	if *showDefaults {
		fmt.Println(strings.TrimRight(string(defaults.BuiltinJSON()), "\n"))
		return nil
	}
	if *showSchema {
		sch, serr := settings.New(settings.Registry())
		if serr != nil {
			return serr
		}
		if a.JSON {
			fmt.Println(sch.JSON(*withPlumbing))
			return nil
		}
		fmt.Print(sch.Describe(*withPlumbing))
		if !*withPlumbing {
			fmt.Println("\n(--plumbing also lists internal settings)")
		}
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
func (a *App) CmdHelp(_ context.Context, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Print(usage)
		return nil
	}
	return a.helpFor(args[0])
}

// helpFor prints one command in detail, plus every setting that applies to
// it. Both come from the same declarations the program runs on, so help
// cannot describe a flag that does not exist or omit one that does.
func (a *App) helpFor(name string) error {
	var found *Command
	for _, c := range Commands() {
		if c.Name == name {
			found = &c
			break
		}
		for _, alias := range c.Aliases {
			if alias == name {
				found = &c
				break
			}
		}
	}
	if found == nil {
		return fmt.Errorf("no command %q; run `mcpx help` for the list", name)
	}
	usageLine := found.Usage
	if usageLine != "" {
		usageLine = " " + usageLine
	}
	fmt.Printf("mcpx %s%s\n\n  %s\n", found.Name, usageLine, found.Summary)
	if len(found.Aliases) > 0 {
		fmt.Printf("  also: %s\n", strings.Join(found.Aliases, ", "))
	}
	if found.Detail != "" {
		fmt.Printf("\n%s\n", wrapAt(found.Detail, 76, "  "))
	}
	if len(found.Examples) > 0 {
		fmt.Println("\nEXAMPLES")
		for _, ex := range found.Examples {
			fmt.Printf("  %s\n", ex)
		}
	}
	sch, err := settings.New(settings.Registry())
	if err != nil {
		return err
	}
	applicable := sch.ForCommand(found.Name)
	var lines []string
	for _, set := range applicable {
		if set.Plumbing {
			continue
		}
		lines = append(lines, fmt.Sprintf("  --%-26s %s", set.FlagName(), set.Short))
	}
	if len(lines) > 0 {
		fmt.Println("\nSETTINGS (also readable from config and the environment)")
		fmt.Println(strings.Join(lines, "\n"))
		fmt.Println("\n  mcpx config --schema   shows every setting with its variable")
	}
	return nil
}

// wrapAt is a plain greedy wrap. Help that runs off the side of a terminal is
// help nobody finishes reading.
func wrapAt(text string, width int, indent string) string {
	words := strings.Fields(text)
	var lines []string
	line := indent
	for _, w := range words {
		if len(line)+len(w)+1 > width && strings.TrimSpace(line) != "" {
			lines = append(lines, line)
			line = indent
		}
		if strings.TrimSpace(line) != "" {
			line += " "
		}
		line += w
	}
	if strings.TrimSpace(line) != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
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
