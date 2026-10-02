package cli

import (
	"context"
	"encoding/json"
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
	"github.com/dezren39/mcpx/internal/mcpserver"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/settings"
	"github.com/dezren39/mcpx/internal/spec"
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
		var b *settings.Binding
		b, apply = a.bindFlags(fs, cmd)
		if err := a.applyPresets(fs, b, args); err != nil {
			return err
		}
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	delete(flagSetCommand, fs)
	if err := apply(); err != nil {
		return err
	}
	a.adoptSettings()
	if err := a.adoptSpec(); err != nil {
		return err
	}
	if parseFlagsHook != nil {
		return parseFlagsHook(fs)
	}
	return nil
}

// parseFlagsHook lets a test stop a command the moment its flags are folded
// in, so every command can be checked for flags that parse but never reach
// the resolved settings without running the command itself.
var parseFlagsHook func(*flag.FlagSet) error

// adoptSettings copies the settings that decide how the App itself behaves
// out of the resolved set, once the flags have been folded in.
//
// Two of them were reachable from exactly one place before this. --json was a
// global flag consumed in main before the subcommand, so `mcpx ls --json` set
// output.json in the registry and left App.JSON false: the flag parsed, the
// help described it, and the output stayed a table. The state and cache
// directories were read from MCPX_STATE_DIR and MCPX_CACHE_DIR by name, so
// the paths.state key in a config file did nothing.
//
// Here rather than in main because this is the first moment all three layers
// are known. main still resolves a starting Paths, since a command that never
// parses flags -- bare `mcpx` -- has to have somewhere to look.
func (a *App) adoptSettings() {
	if a == nil {
		return
	}
	set := a.Settings()
	if set.Bool("output.json") {
		a.JSON = true
	}
	if dir := set.String("paths.state"); dir != "" {
		a.Paths = daemon.PathsAt(dir, set.String("paths.cache"))
	} else if dir := set.String("paths.cache"); dir != "" {
		a.Paths = daemon.PathsAt(a.Paths.State, dir)
	}
}

// adoptSpec installs the MCP revision policy (spec.precedence, spec.first,
// spec.lenient) for this process. Process-wide because the two places that
// consult it, the server's notification relay and the upstream client's
// dispatch, are reached from every command that speaks MCP.
func (a *App) adoptSpec() error {
	if a == nil {
		return nil
	}
	set := a.Settings()
	p, err := spec.New(set.List("spec.precedence"), strings.TrimSpace(set.String("spec.first")), set.List("spec.lenient"))
	if err != nil {
		return err
	}
	spec.Set(p)
	return nil
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
	// The pool knobs reach Config.Pool from a file by themselves; from a
	// variable or a flag they only reach the resolved set, so they are
	// folded in here before anything resolves a server.
	config.ApplyPoolSettings(cfg, a.Settings())
	pool.Version = a.Version

	// The daemon keys its socket to the config it loaded, matching what the
	// CLI computes, so a config edit yields a new daemon rather than a stale
	// one answering with the previous servers.
	paths := a.Paths
	if len(cfg.Sources) > 0 {
		paths = paths.ForConfig(daemon.FingerprintConfig(cfg.Sources))
	}

	// Read from the resolved set rather than from a variable by name. The
	// hand-rolled chain honoured one spelling of the variable and no flag
	// beyond the one declared here, so `mcpx daemon` and `mcpx run` disagreed
	// about what MCPX_LOGGING_LEVEL meant.
	set := a.Settings()
	logFormat, err := logging.ParseFormat(
		firstSet(*format, set.String("logging.format"), cfg.Logging.Format))
	if err != nil {
		return err
	}
	minLevel, err := logging.ParseLevel(
		firstSet(*level, set.String("logging.level"), cfg.Logging.Level))
	if err != nil {
		return err
	}
	// The daemon and scripts render through the same writer, so one --format
	// governs everything a user sees rather than each surface having its own.
	writer := logging.NewWriter(os.Stderr, logFormat, minLevel)
	// The daemon is a thing that starts and ends, so it gets a trace that
	// every record beneath it carries.
	daemonTrace := logging.NewTraceID("dmn")
	dir := firstSet(*logDir, set.String("logging.dir"), cfg.Logging.Dir,
		filepath.Join(paths.State, "logs"))
	var sink *logging.FileSink
	// logging.file off means no durable log at all. The daemon still renders
	// to stderr; what stops is the JSONL the indexer reads.
	if set.Bool("logging.file") {
		var serr error
		sink, serr = logging.NewFileSink(fileOptions(set, dir))
		if serr != nil {
			// A durable log is a convenience; losing it must not stop the daemon.
			fmt.Fprintf(os.Stderr, "mcpx: durable log unavailable (%v)\n", serr)
			sink = nil
		} else {
			writer = writer.WithFile(sink, slog.LevelDebug)
			defer sink.Close()
		}
	}
	writer = writer.WithBase(map[string]any{logging.KeyTrace: string(daemonTrace)})

	handler := logging.NewSlogHandler(writer).WithSourceLevel(
		logging.SourceLevel(firstSet(logSource.Value(), set.String("logging.source"), cfg.Logging.Source)))
	logger := slog.NewLogLogger(handler, slog.LevelInfo)
	srv, err := daemon.NewServer(daemon.Options{
		Sink:     sink,
		Config:   cfg,
		Paths:    paths,
		Version:  a.Version,
		Logger:   logger,
		IdleExit: a.Settings().Duration("daemon.idleExit"),
		Settings: a.Settings(),
		Augment:  a.augmentAdapters,
	})
	if err != nil {
		return err
	}
	// mcpx's own MCP server, on the listeners the daemon already has. Built
	// here because the daemon cannot import the CLI that builds it, and
	// mounted rather than given a listener of its own: two HTTP servers with
	// overlapping /v1 prefixes was one owner too many.
	if a.Settings().Bool("proto.serveMCP") {
		mcp := &lazyMCP{app: a}
		srv.MCP = mcp
		srv.MCPPath = a.Settings().String("proto.mcpPath")
		srv.MCPTool = func(ctx context.Context, tool string, args json.RawMessage) (string, error) {
			return invokeToolText(mcp.InvokeTool(ctx, tool, args))
		}
	}
	srv.Address = a.Settings().String("daemon.address")
	srv.Origins = a.originPolicy()
	if h := srv.Address; h != "" && h != "127.0.0.1" && h != "localhost" {
		// Said once, loudly. The API is unauthenticated, so whoever can
		// route to this port can run tools as this user, and that should be
		// a sentence somebody read rather than a surprise.
		fmt.Fprintf(os.Stderr,
			"mcpx: listening on %s; the API is unauthenticated, so anything that "+
				"can reach this port can run tools as you\n", h)
	}
	blocks := logging.ParseIncludes(firstSet(*include,
		strings.Join(set.List("logging.include"), ","), cfg.Logging.Include))
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
	// So that `mcpx config` shows what a daemon would actually use.
	config.ApplyPoolSettings(cfg, a.Settings())
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
		Protocol    string `json:"protocol,omitempty"`
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
			Protocol: s.Protocol,
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
	type presetRow struct {
		Setting string `json:"setting"`
		Value   string `json:"value"`
		From    string `json:"from"`
	}
	out := struct {
		Sources []string    `json:"sources"`
		Servers []row       `json:"servers"`
		Presets []presetRow `json:"presets,omitempty"`
	}{Sources: cfg.Sources}
	// A value a preset supplied is a source too, and the one least likely to
	// be remembered: it came from a name on the command line, not a file.
	for _, v := range a.Settings().All() {
		if v.Origin.Layer == settings.LayerPreset {
			out.Presets = append(out.Presets, presetRow{Setting: v.Path, Value: v.Raw, From: v.Origin.String()})
		}
	}

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
	if len(out.Presets) > 0 {
		fmt.Println()
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "SETTING\tVALUE\tFROM PRESET")
		for _, r := range out.Presets {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Setting, r.Value, r.From)
		}
		tw.Flush()
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
		fmt.Print(Usage())
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

// usageHeader and usageFooter frame the command listing, which is generated
// from Commands() so that help cannot list a command that does not exist or
// omit one that does. The hand-written listing it replaced was missing more
// than half of them.
const usageHeader = `mcpx - run TypeScript against your MCP servers from the command line

  A local daemon owns every MCP server process. Scripts import a generated,
  fully typed client and call tools as ordinary async functions, so only the
  result you print reaches the model's context.

`

const usageFooter = `GLOBAL FLAGS
  --config <path>                config file (default: search up from $PWD)
  --json                         machine-readable output
  --profile <name>[,<name>]      include servers in these profiles as well
  --skip-default                 with --profile, exclude the usual default set
  --all-profiles                 every configured server, ignoring profiles
  --version
  --<setting> [value]            any setting every command accepts, e.g.
                                 --plumbing-strict-unknown-keys; same as after
                                 the command

CONCURRENCY
  Each server's "mcpx" block sets two independent things:
    sharing  shared     any number of callers use one process at once
             exclusive  one caller at a time; the rest queue
    scope    what decides which process you get: global (one for
             everything), repo, worktree, cwd, session, parent-session, pid
             or call -- one live process per distinct value, up to max
  A browser wants "scope": "session" and "sharing": "exclusive", so two
  agents driving Chrome at once get two browsers rather than corrupting one.
  A search index wants the defaults: one process, every caller at once.

EXAMPLES
  mcpx ls
  mcpx types fff,codedb
  mcpx exec 'const r = await fff.search({ query: "handleCall" }); console.log(r)'
  mcpx call chrome_devtools.navigate_page '{"url":"https://example.com"}'
`

// Usage is the text `mcpx help` prints.
func Usage() string {
	var b strings.Builder
	b.WriteString(usageHeader)
	groups := CommandsByGroup()
	for _, g := range commandGroups {
		fmt.Fprintf(&b, "%s\n", strings.ToUpper(commandGroupTitles[g]))
		for _, c := range groups[g] {
			left := "mcpx " + c.Name
			if c.Usage != "" && len(left)+1+len(c.Usage) <= usageColumn {
				left += " " + c.Usage
			}
			fmt.Fprintf(&b, "  %-*s %s\n", usageColumn, left, c.Summary)
		}
		b.WriteString("\n")
	}
	b.WriteString("  mcpx help <command>            usage, flags and settings for one command\n\n")
	b.WriteString(usageFooter)
	return b.String()
}

// usageColumn is where summaries start in the listing. A rendering width like
// cli.cellWidth beside it, so both are declared in the same place.
var usageColumn = defaults.UsageColumn

// originPolicy is the set of browser origins the daemon serves: loopback at
// any port, the daemon's own address when it listens somewhere else, and
// transport.allowedOrigins. The same rule MCPServer gives /mcp;
// TestV1AndMCPAgreeOnOrigins holds the two together, since both answer on
// the same listener and a page refused by one must be refused by the other.
func (a *App) originPolicy() mcpserver.OriginPolicy {
	hosts := append([]string(nil), defaults.TransportLoopbackHosts...)
	if addr := a.Settings().String("daemon.address"); addr != "" && !unspecifiedHost(addr) {
		hosts = append(hosts, addr)
	}
	return mcpserver.OriginPolicy{Hosts: hosts, Origins: a.Settings().List("transport.allowedOrigins")}
}
