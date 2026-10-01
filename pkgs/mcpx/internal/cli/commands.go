package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/settings"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dezren39/mcpx/internal/artifacts"
	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/daemon"
	"github.com/dezren39/mcpx/internal/execsvc"
	"github.com/dezren39/mcpx/internal/logging"
	"github.com/dezren39/mcpx/internal/preflight"
	"github.com/dezren39/mcpx/internal/runner"
	"github.com/dezren39/mcpx/internal/source"
)

// App carries state shared by every subcommand.
type App struct {
	Version    string
	ConfigPath string
	JSON       bool
	Profile    Profile
	Paths      daemon.Paths
	client     *Client
	// machineOutput suppresses progress notices when the caller has asked
	// for a shape something else will parse.
	machineOutput bool
	// inlineStop shuts down an in-process daemon when the command ends.
	inlineStop func()
	// connected records that the ladder has been walked for this command.
	connected bool
	// ladder is how the connection was made, for doctor and --json output.
	ladder *Ladder
	// stdoutOverride collects a script's output instead of printing it.
	// Used by the MCP server, which must return what a script produced
	// rather than write it to a stream the host is parsing as protocol.
	stdoutOverride interface{ Write([]byte) (int, error) }
	settingsState
}

// Client returns the lazily-built daemon client, keyed to the config this
// invocation resolves to.
func (a *App) Client() *Client {
	if a.client == nil {
		paths, cfg := a.resolvePathsForConfig()
		cfgPath := a.ConfigPath
		if cfgPath == "" && cfg != nil {
			cfgPath = cfg.Path
		}
		// A configured endpoint points at a daemon this process did not
		// start and must not try to. That is the whole point: one daemon on
		// a network, many machines using it.
		a.client = NewClientAt(paths, cfgPath, a.Settings().String("daemon.endpoint"))
	}
	return a.client
}

// ensure returns a client connected to a daemon, walking the connection
// ladder: an existing socket, a named endpoint, a spawned daemon, and --
// only when explicitly allowed -- an in-process one.
//
// The result is cached for the life of the command, so a command that makes
// ten requests walks the ladder once.
func (a *App) ensure(ctx context.Context) (*Client, error) {
	if a.client != nil && a.connected {
		return a.client, nil
	}
	c, ladder, err := a.Connect(ctx, ConnectOptions{
		Endpoint:    a.Settings().String("daemon.endpoint"),
		AllowSpawn:  a.Settings().Bool("daemon.autostart"),
		AllowInline: a.Settings().Bool("daemon.inline"),
	})
	if err != nil {
		return nil, err
	}
	a.client, a.connected, a.ladder = c, true, ladder
	return c, nil
}

func (a *App) out(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ---- ls ----

// CmdLs prints the configured namespaces.
//
// This is the command an agent runs first. It is answered from the schema
// cache, so it costs a socket round trip and nothing else: no MCP server is
// started and no tools/list is issued.
func (a *App) CmdLs(ctx context.Context, args []string) error {
	fs := newFlagSet("ls")
	verbose := fs.Bool("v", false, "include per-instance detail")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	nss, err := c.Namespaces(ctx, a.Profile)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(nss)
	}
	if len(nss) == 0 {
		fmt.Println("No MCP servers configured. Run `mcpx init` to create a config.")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tTOOLS\tSHARING\tSCOPE\tLIVE\tSTATE\tDESCRIPTION")
	broken := 0
	for _, n := range nss {
		state := "ready"
		switch {
		case n.Error != "":
			state = "error"
			broken++
		case !n.Cached:
			state = "unread"
		case n.Live > 0:
			state = "running"
		}
		desc := n.Description
		if n.Error != "" {
			desc = truncate(oneLine(n.Error), 60)
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%d\t%s\t%s\n",
			n.Namespace, n.Tools, n.Sharing, n.Scope, n.Live, state, desc)
	}
	tw.Flush()

	if *verbose {
		fmt.Println()
		return a.CmdStatus(ctx, nil)
	}
	if broken > 0 {
		fmt.Fprintf(os.Stderr, "\n%d server(s) failed to start; `mcpx status` has the full error.\n", broken)
	}
	fmt.Println("\nNext: `mcpx types <namespace>` for signatures, `mcpx search <query>` to find a tool.")
	return nil
}

// ---- types ----

// CmdTypes prints TypeScript declarations for selected namespaces.
func (a *App) CmdTypes(ctx context.Context, args []string) error {
	fs := newFlagSet("types")
	noInstr := fs.Bool("no-instructions", false,
		"omit the server's own guidance, which can be long")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	ns := splitAll(fs.Args())
	if len(ns) == 0 {
		return errors.New("usage: mcpx types <namespace>[.<tool>][,...]\n" +
			"       one namespace, or one tool within it; run `mcpx ls` first")
	}
	if err := a.ensureSchemas(ctx, c, ns); err != nil {
		return err
	}
	// --no-instructions is the short spelling of the same choice; the
	// setting carries it from a config file, MCPX_CATALOG_INSTRUCTIONS or
	// --catalog-instructions, none of which reached this call before.
	wantInstr := a.Settings().Bool("catalog.instructions") && !*noInstr
	text, err := c.Types(ctx, ns, wantInstr, a.Profile)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

// ensureSchemas triggers a fetch for namespaces that have never been read.
//
// Selectors may name a tool ("chrome_devtools.click"); only the namespace part
// is validated here, because whether a tool exists is a question for the
// daemon, which holds the schemas.
func (a *App) ensureSchemas(ctx context.Context, c *Client, selectors []string) error {
	known, err := c.Namespaces(ctx, a.Profile)
	if err != nil {
		return err
	}
	byName := map[string]daemon.NamespaceInfo{}
	for _, n := range known {
		byName[n.Namespace] = n
		byName[n.Server] = n
	}
	needRefresh := false
	for _, sel := range selectors {
		ns := sel
		if i := strings.Index(sel, "."); i > 0 {
			ns = sel[:i]
		}
		n, ok := byName[ns]
		if !ok {
			names := make([]string, 0, len(known))
			for _, k := range known {
				names = append(names, k.Namespace)
			}
			sort.Strings(names)
			return fmt.Errorf("unknown namespace %q; available: %s", ns, strings.Join(names, ", "))
		}
		if !n.Cached {
			needRefresh = true
		}
	}
	if !needRefresh {
		return nil
	}
	a.notice("reading tool schemas for the first time...")
	_, err = c.Refresh(ctx)
	return err
}

// resolveLauncher turns the flag's value into launcher source.
//
// The value is whatever the user gave: a path, inline source, or the bare
// word that means no launcher at all. Resolution goes through the same
// probe every other source-shaped setting uses, so a launcher kept in a file
// needs no special syntax.
func (a *App) resolveLauncher(v string) (text, name string, err error) {
	if strings.TrimSpace(v) == runner.LauncherNone {
		return runner.LauncherNone, "none", nil
	}
	dir := mustGetwd()
	r, rerr := source.Resolve(v, plumbingSourceOptions(dir, nil))
	if rerr != nil {
		return "", "", rerr
	}
	switch r.Kind {
	case source.KindNone:
		return "", "", nil
	case source.KindFile:
		return r.Text, r.Files[0], nil
	default:
		return r.Text, "--launcher", nil
	}
}

// notice reports progress on stderr, unless the caller asked for JSON. A
// machine-readable run should produce one document and nothing else, even on
// the stream a human would have read.
// notice reports progress on stderr.
//
// Silent whenever the caller has chosen a machine-readable shape. --json, and
// every log format other than the default text one, mean somebody is parsing
// this; a friendly line about warming a cache is noise at best and a parse
// error at worst. It was both, in a test that read bare output and got a
// sentence.
func (a *App) notice(msg string) {
	if a.JSON || a.machineOutput {
		return
	}
	fmt.Fprintln(os.Stderr, "mcpx: "+msg)
}

// setOutputFormat records the rendering the caller asked for, so that
// progress notices can stay out of the way of anything parsing the output.
func (a *App) setOutputFormat(f logging.Format) {
	a.machineOutput = f != logging.FormatText
}

// ---- search ----

// CmdSearch ranks tools across every namespace.
func (a *App) CmdSearch(ctx context.Context, args []string) error {
	fs := newFlagSet("search")
	// -n, --limit and --search-limit are one setting, declared once. The
	// value is sent only when somebody actually chose it: a client's
	// inherited default must not override what the daemon is configured
	// with, or every command would quietly impose its own.
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	limit := 0
	if v, ok := a.Settings().Value("search.limit"); ok && v.Origin.Layer != settings.LayerDefault {
		limit = a.Settings().Int("search.limit")
	}
	if fs.NArg() == 0 {
		return errors.New("usage: mcpx search <query>")
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	// Search reads the schema cache, so it has to make sure there is one.
	// Every other discovery command did this; search did not, and on a cold
	// cache it answered "no matching tools" -- which is indistinguishable
	// from the tool genuinely not existing, and sends the reader looking in
	// the wrong place.
	if err := a.ensureAnySchemas(ctx, c); err != nil {
		return err
	}
	hits, err := c.Search(ctx, strings.Join(fs.Args(), " "), limit)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(hits)
	}
	if len(hits) == 0 {
		fmt.Println("No matching tools. Try `mcpx ls` to see which namespaces exist,")
		fmt.Println("or `mcpx refresh` if schemas have never been read.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FUNCTION\tDESCRIPTION")
	for _, h := range hits {
		fmt.Fprintf(tw, "%s\t%s\n", h.Function, truncate(oneLine(h.Description), 90))
	}
	tw.Flush()
	return nil
}

// ---- call ----

// CmdCall invokes a single tool without starting a JavaScript runtime. This is
// the cheap path for one-shot calls where a script would be overkill.
func (a *App) CmdCall(ctx context.Context, args []string) error {
	fs := newFlagSet("call")
	session := fs.String("session", "", "session key for stateful servers")
	raw := fs.Bool("raw", false, "print the full MCP envelope")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: mcpx call <namespace>.<tool> ['<json args>']")
	}
	target := fs.Arg(0)
	dot := strings.LastIndex(target, ".")
	if dot < 1 {
		return fmt.Errorf("expected <namespace>.<tool>, got %q", target)
	}
	ns, tool := target[:dot], target[dot+1:]

	argsJSON := json.RawMessage(`{}`)
	if fs.NArg() > 1 {
		body := strings.Join(fs.Args()[1:], " ")
		if !json.Valid([]byte(body)) {
			return fmt.Errorf("arguments must be JSON, got: %s", body)
		}
		argsJSON = json.RawMessage(body)
	}

	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	res, err := c.Call(ctx, ns, tool, a.callContext(*session, *session), argsJSON)
	if err != nil {
		if a.JSON {
			a.outCallFailure(err)
		}
		return err
	}
	text, failed := renderResult(res.Result)
	if *raw || a.JSON {
		if err := a.out(json.RawMessage(res.Result)); err != nil {
			return err
		}
	} else {
		fmt.Println(text)
	}
	// The result is printed either way -- it is the explanation -- but a
	// script must not read a failed tool as a success.
	if failed {
		return fmt.Errorf("%s.%s reported an error (isError)", ns, tool)
	}
	return nil
}

// outCallFailure writes a refused call to stdout as the daemon's error
// document, so --json carries the diagnostics as data and not only as the
// text on stderr. The error is still returned, because the exit status is
// what a script checks first.
func (a *App) outCallFailure(err error) {
	var he *HTTPError
	if !errors.As(err, &he) {
		return
	}
	var body daemon.CallErrorBody
	if json.Unmarshal(he.Body, &body) != nil || body.Error == "" {
		return
	}
	_ = a.out(body)
}

// renderResult unwraps a CallToolResult the same way the script client does,
// so CLI and script output agree. failed is the result's isError, which every
// caller has to carry to its own surface: dropping it tells a model that a
// failed tool succeeded.
func renderResult(raw json.RawMessage) (text string, failed bool) {
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return string(raw), false
	}
	if len(r.StructuredContent) > 0 {
		var buf strings.Builder
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if enc.Encode(json.RawMessage(r.StructuredContent)) == nil {
			return strings.TrimRight(buf.String(), "\n"), r.IsError
		}
	}
	var texts []string
	for _, c := range r.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n"), r.IsError
	}
	return string(raw), r.IsError
}

// ---- run / exec ----

// CmdRun executes a TypeScript file against the generated client.
func (a *App) CmdRun(ctx context.Context, args []string) error {
	return a.runScript(ctx, args, false)
}

// CmdExec executes an inline TypeScript snippet.
func (a *App) CmdExec(ctx context.Context, args []string) error {
	return a.runScript(ctx, args, true)
}

func (a *App) runScript(ctx context.Context, args []string, inline bool) error {
	name := "run"
	if inline {
		name = "exec"
	}
	fs := newFlagSet(name)
	nsFlag := fs.String("ns", "", "restrict the client to these namespaces (comma separated)")
	rt := fs.String("runtime", "", "javascript runtime: auto, deno, bun, node")
	timeout := fs.Duration("timeout", 0, "kill the script after this long (0 = no limit)")
	keep := fs.Bool("keep", false, "keep the generated client and script for inspection")
	session := fs.String("session", "", "session key (default: a fresh one per run)")
	format := fs.String("format", "", "log rendering: text, json, json-pretty, logfmt, compact, bare")
	level := fs.String("log-level", "", "minimum level: debug, info, warn, error")
	logSource := newOptional("all")
	fs.Var(logSource, "log-source",
		"levels that record a call site: bare for all, or a level name, or false")
	export := fs.String("export", "", "call this export instead of the default one")
	prefix := newRepeatable()
	fs.Var(prefix, "prefix", "line to emit before an exec snippet; repeatable, '-' inherits")
	suffix := newRepeatable()
	fs.Var(suffix, "suffix", "line to emit after an exec snippet; repeatable, '-' inherits")
	before := newRepeatable()
	fs.Var(before, "before", "line to run before anything is installed; repeatable")
	onSuccess := newRepeatable()
	fs.Var(onSuccess, "on-success", "line to run when the entry point returns; repeatable")
	onError := newRepeatable()
	fs.Var(onError, "on-error", "line to run when it throws; repeatable")
	// --launcher always takes a value. An optional-value flag would be
	// tidier to type, but Go's parser only makes that work by treating the
	// flag as boolean, and then `--launcher mine.ts` silently reads mine.ts
	// as the script and runs the wrong file with no launcher at all. A
	// separate --no-launcher costs one flag and removes the trap.
	launcherFlag := fs.String("launcher", "",
		"replace the generated launcher with this source or file")
	noLauncher := fs.Bool("no-launcher", false,
		"run the script with no launcher: no globals, no capture, no wrapper")
	typecheck := fs.String("typecheck", "",
		"check the program before running it: off, on, strict")

	allowRepeat := newRepeatable()
	fs.Var(allowRepeat, "allow-repeat",
		"launcher placeholder permitted to resolve more than once; repeatable")
	envVars := newRepeatable()
	fs.Var(envVars, "env", "set an environment variable for the script, KEY=VALUE; repeatable")
	noConsole := fs.Bool("no-capture-console", false,
		"leave console.* alone instead of mirroring it into the record stream")
	perms := fs.String("permissions", "",
		"deno sandbox: all (default), net, read, read-net, strict, or explicit flags")
	// --remote and --local are the readable spellings of exec.where. The
	// setting is an enum because there is a third value -- auto -- and an
	// enum with three values is not two booleans.
	remote := fs.Bool("remote", false, "run the script on the daemon rather than here")
	local := fs.Bool("local", false, "run the script in this process (the default when the daemon is local)")
	// parseFlags binds everything the registry declares for this command that
	// the hand-written flags above have not already claimed, then folds what
	// was given into the resolved settings.
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		if inline {
			return errors.New("usage: mcpx exec '<typescript>'")
		}
		return errors.New("usage: mcpx run <script.ts> [args...]")
	}

	// Resolved before anything can print. A progress notice is suppressed
	// for a machine-readable format, and the first thing that emits one is
	// the schema fetch below -- so deciding the format afterwards meant the
	// suppression never applied on a cold cache. The failure appeared only
	// when the cache happened to be cold, which is the worst kind of
	// ordering bug: correct on every run that had already been run.
	if f, ferr := logging.ParseFormat(firstNonEmpty(*format,
		a.Settings().String("logging.format"))); ferr == nil {
		a.setOutputFormat(f)
	}

	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}

	ns := splitAll(strings.Split(*nsFlag, ","))
	if len(ns) > 0 {
		if err := a.ensureSchemas(ctx, c, ns); err != nil {
			return err
		}
	} else if err := a.ensureAnySchemas(ctx, c); err != nil {
		return err
	}

	eo := a.resolveExecOptions()
	if *remote && *local {
		return errors.New("--remote and --local contradict each other")
	}
	if *remote {
		eo.Where = "remote"
	} else if *local {
		eo.Where = "local"
	}
	elsewhere, werr := a.runsRemotely(c, eo.Where)
	if werr != nil {
		return werr
	}
	// One identifier for this execution, used by the artifact index to group
	// what the script produced and by a stream consumer to name the run.
	runID := execsvc.NewRunID()

	sessionKey := *session
	hostSession := os.Getenv("MCPX_SESSION_ID")
	// Ephemeral unless the caller named a session or the host assigned one.
	ephemeral := sessionKey == "" && hostSession == ""
	if sessionKey == "" {
		sessionKey = hostSession
	}
	if sessionKey == "" {
		sessionKey = newSessionKey()
	}
	// Free any pinned instances (browsers) as soon as the script ends, rather
	// than leaving them parked until the idle timer fires.
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(),
			a.Settings().Duration("session.releaseTimeout"))
		defer cancel()
		_ = c.ReleaseCaller(rctx, sessionKey)
	}()

	if elsewhere {
		return a.execOnDaemon(ctx, c, fs, inline, ns, sessionKey, eo,
			execRemoteFlags{timeout: *timeout, runtime: *rt, permissions: *perms,
				export: *export, session: sessionKey})
	}

	// The session is deliberately not baked into the generated module: several
	// concurrent runs share one client file, and each must keep its own
	// session so session-mode pools hand out separate processes.
	clientSrc, err := c.ClientModule(ctx, ns, "", a.Profile)
	if err != nil {
		return err
	}
	endpoint, err := c.Endpoint(ctx)
	if err != nil {
		return err
	}
	prelude, err := a.buildPrelude(ctx, c, ns, !*noConsole)
	if err != nil {
		return err
	}
	globalsSrc, gerr := c.Globals(ctx, ns, a.Profile)
	if gerr != nil {
		// Editor convenience only; never fail a run for it.
		globalsSrc = ""
	}

	cfg, _ := config.Load(a.ConfigPath)
	runtimePref := *rt
	if runtimePref == "" && cfg != nil {
		runtimePref = cfg.Runtime
	}

	// Read from the resolved settings, which already folded the flag, the
	// environment and every config file in the right order. The chain that
	// used to be spelled out here did the same thing for three of the values
	// and a slightly different thing for the fourth, which is the drift the
	// registry exists to remove.
	//
	// The short flag spellings still win where they were given, because
	// parseFlags folded them into the same set.
	var cfgLog config.LoggingConfig
	if cfg != nil {
		cfgLog = cfg.Logging
	}
	set := a.Settings()
	logFormat, ferr := logging.ParseFormat(firstNonEmpty(*format, set.String("logging.format"), cfgLog.Format))
	if ferr != nil {
		return ferr
	}
	a.setOutputFormat(logFormat)
	minLevel, lerr := logging.ParseLevel(firstNonEmpty(*level, set.String("logging.level"), cfgLog.Level))
	if lerr != nil {
		return lerr
	}
	sourceLevel := logging.SourceLevel(
		firstNonEmpty(logSource.Value(), set.String("logging.source"), cfgLog.Source))
	// With --json the envelope carries the records, so nothing is rendered to
	// stderr; a reader wants one parseable document, not two streams.
	logSink := io.Writer(os.Stderr)
	var collected []logging.Record
	var collect func(logging.Record)
	if a.JSON {
		logSink = io.Discard
		collect = func(r logging.Record) { collected = append(collected, r) }
	}
	writer := logging.NewWriter(logSink, logFormat, minLevel)
	// Script records go to the same durable log the daemon writes, so a run's
	// output is recoverable afterwards even when the terminal showed little.
	if dir := firstNonEmpty(set.String("logging.dir"), cfgLog.Dir,
		filepath.Join(a.Paths.State, "logs")); dir != "" && set.Bool("logging.file") {
		if sink, serr := logging.NewFileSink(fileOptions(set, dir)); serr == nil {
			writer = writer.WithFile(sink, slog.LevelDebug)
			defer sink.Close()
		}
	}

	// Streamed values are the script's answers, so they belong on stdout as
	// they arrive. Under --json they are collected into the envelope instead,
	// preserving order.
	var streamed []logging.Streamed
	onResult := func(v logging.Streamed) { streamed = append(streamed, v) }
	// Only the text shape writes streamed values straight to stdout. The
	// other two carry them as fields -- in the document, or in an emit frame
	// -- and printing them here as well would put a bare JSON line in the
	// middle of a document a caller is parsing.
	if !a.JSON && eo.Output == execsvc.OutputText {
		stdout := io.Writer(os.Stdout)
		onResult = func(v logging.Streamed) {
			fmt.Fprintln(stdout, string(v.Value))
		}
	}

	// script.permissions, from a file, MCPX_SCRIPT_PERMISSIONS, MCPX_PERMISSIONS
	// or --script-permissions. Only this path read MCPX_PERMISSIONS, by name,
	// so the other three were accepted here and ignored. Only when given: the
	// default "all" would otherwise hide the config file's top-level
	// "permissions" key.
	setPerms := ""
	if a.Settings().Given("script.permissions") {
		setPerms = a.Settings().String("script.permissions")
	}
	opts := runner.Options{
		// Set when something other than a terminal is collecting the output:
		// the MCP server, which has to return it rather than print it.
		Stdout:         a.stdoutOverride,
		ClientSource:   clientSrc,
		GlobalsSource:  globalsSrc,
		CaptureConsole: !*noConsole,
		Runtime:        runtimePref,
		Timeout:        *timeout,
		Prelude:        prelude,
		Export:         *export,
		Permissions:    firstNonEmpty(*perms, setPerms, cfgPerms(cfg)),
		Log:            writer,
		CollectLogs:    collect,
		OnResult:       onResult,
		// Harness-supplied identifiers ride along on every record, so a log
		// can be filtered by session or worktree without the script having
		// been told any of it.
		Enrich: enrichWith(map[string]any{
			"session": sessionKey,
			"cwd":     mustGetwd(),
		}, logging.HarnessIDs()),
		Env: map[string]string{
			"MCPX_SESSION": sessionKey,
			"MCPX_RUN":     runID,
			// Whether artifact({path}) may hand the daemon a path instead of
			// bytes. A socket is the evidence that the daemon shares this
			// filesystem; a remote endpoint has none.
			"MCPX_ARTIFACTS_LOCAL": boolFlag(c.Socket() != ""),
			"MCPX_ENDPOINT":        endpoint,
			// The socket as well as the port, so a script can use whichever
			// is faster. Empty when the daemon is remote: a socket on another
			// machine is not reachable from here.
			"MCPX_SOCKET":     c.Socket(),
			"MCPX_LOG_SOURCE": logging.SourceSpec(sourceLevel),
			"MCPX_LOG_LEVEL":  logging.LevelName(minLevel),
			// Path facts travel through the environment so that a module three
			// imports deep sees the same values as the entry script, without
			// anything being threaded through call signatures.
			"MCPX_CWD": mustGetwd(),
			"MCPX_PID": strconv.Itoa(os.Getpid()),
			// The session a script's calls belong to. Scope resolution on the
			// daemon side keys on this.
			"MCPX_SESSION_ID":        os.Getenv("MCPX_SESSION_ID"),
			"MCPX_PARENT_SESSION_ID": os.Getenv("MCPX_PARENT_SESSION_ID"),
			"MCPX_EPHEMERAL":         boolFlag(ephemeral),
			"MCPX_SCRIPT_DIRS":       strings.Join(scriptSearchDirs(), ":"),
			"MCPX_CONFIG_PATH":       configPathOf(cfg),
		},
	}
	// Everything knowable is checked before any server starts. A bad --env
	// pair or an unreadable hook is cheap to find now and expensive to find
	// as a syntax error in generated code.
	var envPairs []string
	for _, e := range envVars.Values() {
		if str, ok := e.(string); ok {
			envPairs = append(envPairs, str)
		}
	}
	// script.env is the same list from a config file, MCPX_SCRIPT_ENV or
	// --script-env. --env is a hand-written alias that Bind skips, so before
	// this the other three spellings were accepted and had no effect.
	envPairs = append(envPairs, a.Settings().List("script.env")...)
	pre := preflight.Merge(preflight.CheckEnvPairs(envPairs, "--env"))
	// plumbing.validatePaths is the switch this block exists behind. It read
	// MCPX_LOGGING_DIR by name, so the same directory given in a config file
	// or as --log-dir went unchecked; the resolved value is the one that will
	// actually be written to.
	if a.Settings().Bool("plumbing.validatePaths") {
		if logDir := a.Settings().String("logging.dir"); logDir != "" {
			pre = preflight.Merge(pre, preflight.CheckPaths([]preflight.PathCheck{
				{Path: logDir, Where: "logging.dir", WantDir: true, Writable: true},
			}))
		}
	}
	if err := pre.Err(); err != nil {
		return err
	}
	for _, w := range pre.Warnings() {
		fmt.Fprintln(os.Stderr, "mcpx:", w.String())
	}

	// Each phase takes its command-line values and then whatever the
	// registry resolved from a variable or the generated --script-<phase>
	// flag. Only those two layers: the configuration file reaches these
	// through cfg.ScriptPhase below, and counting it twice would run the
	// line twice. Before this, --script-prefix and MCPX_SCRIPT_PREFIX were
	// accepted, recorded and dropped.
	prefixLines := cfgScriptLines(cfg, a.phaseValues("script.prefix", prefix), true)
	suffixLines := cfgScriptLines(cfg, a.phaseValues("script.suffix", suffix), false)

	// Both paths, not only the file one. This lived in the `else` branch, so
	// `mcpx exec --typecheck=on` accepted the flag and ignored it -- on the
	// command an agent reaches for most.
	//
	// Read from the resolved set, so the value is whichever layer won: the
	// short --typecheck spelling, the generated --script-typecheck, a
	// variable, or the config file. Reading the flag variable directly would
	// silently ignore the other three.
	opts.TypeCheck = firstNonEmpty(*typecheck, a.Settings().String("script.typecheck"))
	// Both paths. These were set only for a file, so `mcpx exec --launcher`,
	// --no-launcher, --export and --allow-repeat were accepted and dropped:
	// the flags existed, the help described them, and nothing read them.
	//
	// Phases stay where they are, because there the difference is real: a
	// snippet splices them into its own scope so they can declare bindings
	// it uses, while a file gets them wrapped around its import.
	if *noLauncher && *launcherFlag != "" {
		return errors.New("--launcher and --no-launcher contradict each other; " +
			"--no-launcher means there is nothing to replace")
	}
	// --launcher is the short spelling; script.launcher carries the same
	// choice from a config file, MCPX_SCRIPT_LAUNCHER or --script-launcher,
	// all three of which were previously parsed and ignored. The bare form of
	// the generated flag resolves to "none", which resolveLauncher
	// understands, so --no-launcher and --script-launcher mean the same thing.
	launcherWanted := firstNonEmpty(*launcherFlag, a.Settings().String("script.launcher"))
	if *noLauncher {
		opts.Launcher, opts.LauncherName = runner.LauncherNone, "none"
	} else if launcherWanted != "" {
		text, name, lerr := a.resolveLauncher(launcherWanted)
		if lerr != nil {
			return lerr
		}
		opts.Launcher, opts.LauncherName = text, name
	}
	opts.PlaceholderFiles = PlaceholderFiles()
	for _, r := range allowRepeat.Values() {
		if str, ok := r.(string); ok {
			opts.AllowRepeat = append(opts.AllowRepeat, str)
		}
	}
	// The plumbing setting is the durable form of --allow-repeat: a launcher
	// template kept in a config file wants its repeats declared beside it,
	// not retyped on every command line.
	opts.AllowRepeat = append(opts.AllowRepeat,
		a.Settings().List("plumbing.launcherPlaceholderRepeat")...)
	if inline {
		// A snippet is generated wholesale, so prefix lines share its scope
		// and can declare bindings the snippet uses.
		var body strings.Builder
		for _, line := range prefixLines {
			body.WriteString(line)
			body.WriteString("\n")
		}
		body.WriteString(strings.Join(fs.Args(), " "))
		for _, line := range suffixLines {
			body.WriteString("\n")
			body.WriteString(line)
		}
		opts.Source = body.String()
		// The three phases that are not spliced into the snippet run in the
		// launcher around it, as they do around a file. They were set only
		// for a file, so `mcpx exec --before/--on-success/--on-error` and
		// script.before/onSuccess/onError were accepted and dropped.
		opts.Phases = runner.Phases{
			Before:    cfg.ScriptPhase("before", a.phaseValues("script.before", before)),
			OnSuccess: cfg.ScriptPhase("onSuccess", a.phaseValues("script.onSuccess", onSuccess)),
			OnError:   cfg.ScriptPhase("onError", a.phaseValues("script.onError", onError)),
		}
	} else {
		// A file keeps its own module scope, so its prefix and suffix run in
		// the launcher around it: before the import and after the entry point.
		// They can act -- set globals, log, time, clean up -- but cannot
		// declare bindings the script will see.
		opts.Phases = runner.Phases{
			Before:    cfg.ScriptPhase("before", a.phaseValues("script.before", before)),
			Prefix:    prefixLines,
			OnSuccess: cfg.ScriptPhase("onSuccess", a.phaseValues("script.onSuccess", onSuccess)),
			OnError:   cfg.ScriptPhase("onError", a.phaseValues("script.onError", onError)),
			Suffix:    suffixLines,
		}
		file, rerr := resolveScript(fs.Arg(0))
		if rerr != nil {
			return rerr
		}
		opts.File = file
		opts.Args = fs.Args()[1:]
	}

	// Deterministic diagnostics, before anything starts. What this catches is
	// a tool whose schema moved under a script that used to work; left to the
	// runtime it surfaces as an error from the server, halfway through, after
	// the side effects of every call before it.
	diagSource := opts.Source
	if diagSource == "" {
		diagSource = sourceOfScript(opts.File)
	}
	if derr := a.diagnoseBeforeRun(ctx, c, diagSource, sessionKey); derr != nil {
		return derr
	}
	// envPairs is the same list the preflight above checked: --env, plus
	// whatever script.env resolved to from a file, MCPX_SCRIPT_ENV or
	// --script-env. Iterating envVars here instead was how the three other
	// spellings were validated and then dropped.
	for _, kv := range envPairs {
		k, v, found := strings.Cut(kv, "=")
		if !found {
			return fmt.Errorf("--env expects KEY=VALUE, got %q", kv)
		}
		opts.Env[k] = v
	}

	if *keep {
		dir, err := os.MkdirTemp("", "mcpx-keep-")
		if err != nil {
			return err
		}
		opts.WorkDir = dir
		fmt.Fprintf(os.Stderr, "mcpx: workdir %s\n", dir)
	} else {
		// One directory per distinct catalogue, named for the hash of the
		// generated client. A single shared directory -- which is what this
		// was, and what the daemon used -- let two projects overwrite each
		// other's client: 7 of 12 concurrent cross-project runs failed,
		// each type-checked against the other's catalogue.
		opts.WorkRoot = filepath.Join(a.Paths.Cache, "exec")
	}

	// The artifact store is the daemon's, opened from this side. When the
	// daemon is on this machine that is the same SQLite index and the same
	// blobs, so a file the script registered can be hardlinked into an
	// output directory rather than fetched back over a socket.
	var store *artifacts.Store
	if st, serr := a.localStore(); serr == nil {
		store = st
		defer store.Close()
	} else if eo.ArtifactsDir != "" {
		// Only worth failing for when the caller actually asked for files.
		return serr
	}
	svc := a.localService(store, c.Socket())
	wire := execsvc.Options{
		Session:   sessionKey,
		Output:    eo.Output,
		Artifacts: eo.artifactOptions(mustGetwd()),
	}
	if wire.Artifacts != nil {
		wire.Capabilities = []string{execsvc.CapabilityArtifacts}
	}

	if a.JSON {
		// Everything the run produced belongs in the document, including the
		// script's own stderr. Letting it through would mean the caller has to
		// separate a JSON envelope from a stack trace on one stream.
		var out, errOut strings.Builder
		opts.Stdout = &out
		opts.Stderr = &errOut
		res, err := svc.RunWith(ctx, opts, wire, nil)
		if err != nil {
			return err
		}
		env := runEnvelope(res, out.String(), collected, streamed)
		if s := errOut.String(); s != "" {
			env["stderr"] = s
		}
		if len(res.Artifacts) > 0 {
			env["artifacts"] = res.Artifacts
		}
		if err := a.out(env); err != nil {
			return err
		}
		if res.ExitCode != 0 {
			os.Exit(res.ExitCode)
		}
		return nil
	}

	// A caller asking for frames wants them on stdout as they happen, and
	// nothing else on stdout: the script's own output is carried inside a
	// stdout frame rather than written beside them.
	var sink execsvc.Sink
	switch eo.Output {
	case execsvc.OutputStream:
		var raw strings.Builder
		opts.Stdout = &raw
		enc := json.NewEncoder(os.Stdout)
		sink = func(f execsvc.Frame) error { return enc.Encode(f) }
	case execsvc.OutputStructured:
		var raw strings.Builder
		opts.Stdout = &raw
	default:
		// Text: the script's stdout is the answer and goes straight to the
		// terminal as it is produced. The service still collects a copy, but
		// it must not become the only destination -- a long run that printed
		// as it went would print nothing until it ended.
		if opts.Stdout == nil {
			opts.Stdout = os.Stdout
		}
	}

	res, err := svc.RunWith(ctx, opts, wire, sink)
	if err != nil {
		return err
	}
	if eo.Output == execsvc.OutputStructured {
		if err := a.out(res); err != nil {
			return err
		}
	} else if eo.Output != execsvc.OutputStream {
		a.reportArtifacts(res)
	}
	if res.ExitCode != 0 {
		os.Exit(res.ExitCode)
	}
	return nil
}

// runEnvelope is the machine-readable form of a script run: one document
// carrying everything a caller would otherwise have to scrape from two
// streams and an exit status.
func runEnvelope(res *execsvc.Result, stdout string, logs []logging.Record, streamed []logging.Streamed) map[string]any {
	env := map[string]any{
		"ok":         res.ExitCode == 0 && !res.TimedOut,
		"exitCode":   res.ExitCode,
		"durationMs": res.DurationMs,
		"runtime":    res.Runtime,
		"stdout":     stdout,
	}
	if res.TimedOut {
		env["timedOut"] = true
	}
	// A script whose stdout is JSON almost always means it as its result, so
	// offer it parsed as well as raw rather than making every caller re-parse.
	if trimmed := strings.TrimSpace(stdout); trimmed != "" {
		var parsed any
		if json.Unmarshal([]byte(trimmed), &parsed) == nil {
			env["result"] = parsed
		}
	}
	if len(streamed) > 0 {
		values := make([]json.RawMessage, 0, len(streamed))
		for _, v := range streamed {
			values = append(values, v.Value)
		}
		env["results"] = values
	}
	if len(logs) > 0 {
		items := make([]map[string]any, 0, len(logs))
		for _, r := range logs {
			item := map[string]any{
				"ts":    r.Time.Format(time.RFC3339Nano),
				"level": logging.LevelName(r.Level),
				"msg":   r.Msg,
			}
			if r.Template != "" {
				item["template"] = r.Template
			}
			for k, v := range r.Attrs {
				if _, taken := item[k]; !taken {
					item[k] = v
				}
			}
			items = append(items, item)
		}
		env["logs"] = items
	}
	return env
}

// enrichWith folds harness identifiers in without letting them overwrite what
// mcpx itself established. A caller can add context; it cannot rewrite which
// session a call was actually leased for.
func enrichWith(base, extra map[string]any) map[string]any {
	for k, v := range extra {
		if _, taken := base[k]; taken {
			continue
		}
		base[k] = v
	}
	return base
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// ensureAnySchemas fetches schemas when a namespace has none yet.
//
// It used to stop at the first namespace that was cached, so a server whose
// first fetch was still in flight -- an adapter, which is mcpx spawning
// itself, is reliably slower than a small upstream -- was silently missing
// from the generated client while its neighbours were present. A namespace
// whose fetch failed has an error to show instead and is not retried here,
// or every command would pay for one broken server.
func (a *App) ensureAnySchemas(ctx context.Context, c *Client) error {
	known, err := c.Namespaces(ctx, a.Profile)
	if err != nil {
		return err
	}
	missing := false
	for _, n := range known {
		if !n.Cached && n.Error == "" {
			missing = true
		}
	}
	if !missing {
		return nil
	}
	a.notice("reading tool schemas for the first time...")
	_, err = c.Refresh(ctx)
	return err
}

// buildPrelude imports the client and binds each namespace as a bare
// identifier, so an agent can write either `fff.search(...)` or
// `tools.fff.search(...)`.
func (a *App) buildPrelude(ctx context.Context, c *Client, ns []string, captureConsole bool) (string, error) {
	known, err := c.Namespaces(ctx, a.Profile)
	if err != nil {
		return "", err
	}
	want := map[string]bool{}
	for _, n := range ns {
		want[n] = true
	}
	var names []string
	for _, n := range known {
		if len(want) > 0 && !want[n.Namespace] && !want[n.Server] {
			continue
		}
		names = append(names, n.Namespace)
	}
	sort.Strings(names)
	// Built by the shared service, not here. Two preludes would mean a
	// snippet that compiles when a person runs it and not when the daemon
	// does, which is the exact class of difference this whole change removes.
	return execsvc.Prelude(names, captureConsole), nil
}

// ---- client (write the module for hand-written scripts) ----

// CmdClient writes the generated client module to a path so an editor and a
// checked-in script can both see it.
func (a *App) CmdClient(ctx context.Context, args []string) error {
	fs := newFlagSet("client")
	outPath := fs.String("o", "", "write to this path (default: stdout)")
	nsFlag := fs.String("ns", "", "restrict to these namespaces")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	ns := splitAll(strings.Split(*nsFlag, ","))
	if err := a.ensureAnySchemas(ctx, c); err != nil {
		return err
	}
	src, err := c.ClientModule(ctx, ns, "", a.Profile)
	if err != nil {
		return err
	}
	if *outPath == "" {
		fmt.Print(src)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), defaults.PublicDirMode); err != nil {
		return err
	}
	if err := os.WriteFile(*outPath, []byte(src), defaults.PublicMode); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", *outPath)
	return nil
}

// ---- status / admin ----

// CmdStatus prints daemon and pool state.
func (a *App) CmdStatus(ctx context.Context, args []string) error {
	fs := newFlagSet("status")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	c := a.Client()
	if !c.Ping(ctx) {
		if a.JSON {
			return a.out(map[string]any{"running": false})
		}
		fmt.Println("daemon: not running")
		return nil
	}
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if a.JSON {
		// Present both ways. It used to appear only as false, so a reader
		// testing `running` concluded a live daemon was down -- the opencode
		// plugin did exactly that.
		st["running"] = true
		return a.out(st)
	}
	fmt.Printf("daemon:   running (pid %v, up %v)\n", st["pid"], st["uptime"])
	fmt.Printf("endpoint: %v\n", st["endpoint"])
	fmt.Printf("socket:   %v\n", st["socket"])
	fmt.Printf("config:   %v\n\n", st["config"])

	b, _ := json.Marshal(st["servers"])
	var servers []struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Sharing   string `json:"sharing"`
		Scope     string `json:"scope"`
		Max       int    `json:"max"`
		Live      int    `json:"live"`
		Tools     int    `json:"tools"`
		SchemaAge string `json:"schemaAge"`
		LastError string `json:"lastError"`
		Instances []struct {
			ID        string `json:"id"`
			PID       int    `json:"pid"`
			Holders   int    `json:"holders"`
			Key       string `json:"key"`
			Calls     int64  `json:"calls"`
			UptimeSec int    `json:"uptimeSec"`
			IdleSec   int    `json:"idleSec"`
		} `json:"instances"`
	}
	_ = json.Unmarshal(b, &servers)

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tSHARING\tSCOPE\tLIVE/MAX\tTOOLS\tSCHEMA\tERROR")
	for _, s := range servers {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d/%d\t%d\t%s\t%s\n",
			s.Namespace, s.Sharing, s.Scope, s.Live, s.Max, s.Tools, s.SchemaAge,
			truncate(oneLine(s.LastError), 50))
	}
	tw.Flush()

	any := false
	for _, s := range servers {
		if len(s.Instances) > 0 {
			any = true
		}
	}
	if any {
		fmt.Println()
		tw = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "INSTANCE\tPID\tHOLDERS\tCALLS\tUPTIME\tIDLE\tKEY")
		for _, s := range servers {
			for _, in := range s.Instances {
				fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%ds\t%ds\t%s\n",
					in.ID, in.PID, in.Holders, in.Calls, in.UptimeSec, in.IdleSec, truncate(in.Key, 28))
			}
		}
		tw.Flush()
	}
	return nil
}

// CmdRefresh re-reads every server's schemas.
func (a *App) CmdRefresh(ctx context.Context, args []string) error {
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	start := time.Now()
	res, err := c.Refresh(ctx)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(res)
	}
	fmt.Printf("refreshed in %s\n", time.Since(start).Truncate(time.Millisecond))
	if errs, ok := res["errors"].(map[string]any); ok && len(errs) > 0 {
		keys := make([]string, 0, len(errs))
		for k := range errs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", k, errs[k])
		}
	}
	return a.CmdLs(ctx, nil)
}

// CmdRestart stops running instances so the next call starts fresh ones.
func (a *App) CmdRestart(ctx context.Context, args []string) error {
	fs := newFlagSet("restart")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	target := ""
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}
	n, err := c.Restart(ctx, target)
	if err != nil {
		return err
	}
	label := "all servers"
	if target != "" {
		label = target
	}
	fmt.Printf("stopped %d instance(s) for %s\n", n, label)
	return nil
}

// CmdStop shuts the daemon down.
func (a *App) CmdStop(ctx context.Context, args []string) error {
	fs := newFlagSet("stop")
	all := fs.Bool("all", false, "stop every mcpx daemon, not just this config's")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if *all {
		return a.stopAll(ctx)
	}
	c := a.Client()
	if !c.Ping(ctx) {
		fmt.Println("daemon: not running")
		return nil
	}
	if err := c.Shutdown(ctx); err != nil {
		return err
	}
	if err := waitUntilStopped(ctx, c); err != nil {
		return err
	}
	fmt.Println("daemon stopped")
	return nil
}

// stopAll shuts down every daemon recorded in the state directory. Each
// config gets its own daemon, so this is the way to clear them all after
// working across many repos.
func (a *App) stopAll(ctx context.Context) error {
	infos := a.Paths.ListDaemons()
	if len(infos) == 0 {
		fmt.Println("no daemons running")
		return nil
	}
	stopped := 0
	for _, info := range infos {
		p := a.Paths
		p.Socket = info.Socket
		c := NewClient(p, info.ConfigPath)
		if !c.Ping(ctx) {
			continue
		}
		if err := c.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", info.ConfigPath, err)
			continue
		}
		if err := waitUntilStopped(ctx, c); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", info.ConfigPath, err)
			continue
		}
		stopped++
		fmt.Printf("stopped daemon for %s (pid %d)\n", info.ConfigPath, info.PID)
	}
	if stopped == 0 {
		fmt.Println("no running daemons found")
	}
	return nil
}

// waitUntilStopped polls until the daemon stops answering. Shutdown is
// asynchronous on the server side, so returning before it has actually gone
// would make `stop` followed by `status` report a live daemon.
func waitUntilStopped(ctx context.Context, c *Client) error {
	for i := 0; i < 50; i++ {
		if !c.Ping(ctx) {
			return nil
		}
		time.Sleep(defaults.DaemonRestartSettle)
	}
	return errors.New("daemon did not stop")
}

// CmdDaemons lists every daemon this user has running.
func (a *App) CmdDaemons(ctx context.Context, args []string) error {
	infos := a.Paths.ListDaemons()
	type row struct {
		PID     int    `json:"pid"`
		Config  string `json:"config"`
		Running bool   `json:"running"`
		Started string `json:"startedAt"`
	}
	var rows []row
	for _, info := range infos {
		p := a.Paths
		p.Socket = info.Socket
		c := NewClient(p, info.ConfigPath)
		rows = append(rows, row{
			PID: info.PID, Config: info.ConfigPath,
			Running: c.Ping(ctx), Started: info.StartedAt,
		})
	}
	if a.JSON {
		return a.out(rows)
	}
	if len(rows) == 0 {
		fmt.Println("no daemons")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PID\tRUNNING\tSTARTED\tCONFIG")
	for _, r := range rows {
		fmt.Fprintf(tw, "%d\t%v\t%s\t%s\n", r.PID, r.Running, r.Started, r.Config)
	}
	tw.Flush()
	return nil
}

// ---- init ----

// CmdInit writes a starter config.
func (a *App) CmdInit(ctx context.Context, args []string) error {
	fs := newFlagSet("init")
	force := fs.Bool("force", false, "overwrite an existing config")
	global := fs.Bool("global", false, "write to the user config instead of ./.mcpx.json")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	path := ".mcpx.json"
	if *global {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".config", "mcpx", "config.json")
	}
	if _, err := os.Stat(path); err == nil && !*force {
		return fmt.Errorf("%s already exists (use --force)", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), defaults.PublicDirMode); err != nil && filepath.Dir(path) != "." {
		return err
	}
	if err := os.WriteFile(path, []byte(starterConfig), defaults.PublicMode); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	fmt.Println("Edit it, then run `mcpx ls`.")
	return nil
}

// starterConfig is what `mcpx init` writes. Every key in it must be one the
// loader reads: TestStarterConfigMeansWhatItSays loads it strictly and checks
// each server resolves to what its comment promises.
const starterConfig = `{
  // mcpx reads the same "mcpServers" object other MCP hosts use, so an
  // existing config can be pasted in unchanged. The optional "mcpx" block on
  // each server controls how many processes are kept and how they are shared.
  "mcpServers": {
    "example-stateless": {
      "command": "some-mcp-server",
      "args": [],
      "mcpx": {
        // One process for everything, any number of callers at once. Right
        // for search, docs and database servers, which hold no per-caller
        // state. These two are the defaults, written out to show the knobs.
        "sharing": "shared",
        "scope": "global",
        "description": "what this server is for, shown by mcpx ls"
      }
    },
    "chrome-devtools": {
      "command": "chrome-devtools-mcp",
      "args": ["--headless", "--isolated"],
      "mcpx": {
        // One browser per session, one caller at a time, up to 4 at once.
        // A session is the host's MCPX_SESSION_ID, or a single script run
        // when there is none. Concurrent agents get separate browsers
        // instead of fighting over one.
        "sharing": "exclusive",
        "scope": "session",
        "max": 4,
        "idleTimeout": "5m",
        "description": "drive a headless Chrome"
      }
    }
  }
}
`

// ---- helpers ----

func newSessionKey() string {
	return fmt.Sprintf("s%d-%d", os.Getpid(), time.Now().UnixNano()%1_000_000_000)
}

func splitAll(in []string) []string {
	var out []string
	for _, s := range in {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "\u2026"
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}

// CmdScripts lists the named scripts mcpx can run.
func (a *App) CmdScripts(ctx context.Context, args []string) error {
	fs := newFlagSet("scripts")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	entries, err := discoverScripts()
	if err != nil {
		fmt.Println("No scripts found. mcpx looks for <name>.ts in:")
		for _, d := range scriptSearchDirs() {
			fmt.Println("  " + d)
		}
		fmt.Printf("\nCreate one:\n  mkdir -p %s && $EDITOR %s/hello.ts\n  mcpx run hello\n",
			ScriptsDirName, ScriptsDirName)
		return nil
	}
	if a.JSON {
		return a.out(entries)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tDESCRIPTION\tPATH")
	for _, e := range entries {
		name := e.Name
		if e.Shadowed {
			name += " (shadowed)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", name, truncate(oneLine(e.Summary), 54), e.Path)
	}
	tw.Flush()
	fmt.Println("\nRun one with: mcpx run <name>")
	return nil
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func configPathOf(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.Path
}

// callContext assembles what mcpx knows about this invocation. Session and
// parent ids cannot be discovered -- a subagent and its parent share a cwd and
// differ only by an identifier their host assigns -- so they come from flags
// or from the environment the host set up.
func (a *App) callContext(sessionID, callID string) config.CallContext {
	// A session id the caller invented for itself has exactly one user, so it
	// can be torn down on exit; one the host assigned may be shared with a
	// sibling run and must not be.
	ephemeral := sessionID == "" && os.Getenv("MCPX_SESSION_ID") == ""
	if sessionID == "" {
		sessionID = os.Getenv("MCPX_SESSION_ID")
	}
	if callID == "" {
		callID = newSessionKey()
	}
	return config.CallContext{
		Ephemeral:       ephemeral,
		Cwd:             mustGetwd(),
		SessionID:       sessionID,
		ParentSessionID: os.Getenv("MCPX_PARENT_SESSION_ID"),
		PID:             os.Getpid(),
		CallID:          callID,
	}
}

func boolFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// CmdCatalog prints every namespace with as many signatures as fit a budget.
func (a *App) CmdCatalog(ctx context.Context, args []string) error {
	fs := newFlagSet("catalog")
	budget := fs.Int("budget", 0, fmt.Sprintf("approximate token ceiling (default %d)", defaults.CatalogBudget))
	bias := fs.String("bias", "", "promote tools matching these words")
	nsFlag := fs.String("ns", "", "restrict to these namespaces")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	if err := a.ensureAnySchemas(ctx, c); err != nil {
		return err
	}
	ns := splitAll(strings.Split(*nsFlag, ","))
	if *bias == "" && fs.NArg() > 0 {
		*bias = strings.Join(fs.Args(), " ")
	}
	// Fall back to the resolved settings, so catalog.budget and catalog.bias
	// work from a config file and the environment and not only from the two
	// short flags.
	if *budget == 0 {
		*budget = a.Settings().Int("catalog.budget")
	}
	if *bias == "" {
		*bias = strings.Join(a.Settings().List("catalog.bias"), " ")
	}
	text, err := c.Catalog(ctx, ns, *budget, *bias, a.Profile)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

func cfgPerms(c *config.Config) string {
	if c == nil {
		return ""
	}
	return c.Permissions
}

// phaseValues is a phase's command-line values plus whatever the registry
// resolved for it above the configuration file.
//
// The split is deliberate and narrow: cfg.ScriptPhase folds the file layers
// itself, with an inheritance marker the registry does not model, so the
// file's lines would run twice if they were taken from both. What the file
// reader cannot see -- MCPX_SCRIPT_PREFIX, --script-prefix, a runtime
// override -- is what this adds.
func (a *App) phaseValues(path string, flagged *repeatable) []any {
	out := flagged.Values()
	for _, line := range a.Settings().ListAboveFile(path) {
		out = append(out, line)
	}
	return out
}

// cfgScriptLines resolves a layered prefix or suffix, with command-line values
// as the nearest layer.
func cfgScriptLines(cfg *config.Config, flags []any, isPrefix bool) []string {
	if cfg == nil {
		cfg = &config.Config{}
	}
	if isPrefix {
		return resolvePhase(cfg.ScriptPrefix(flags))
	}
	return resolvePhase(cfg.ScriptSuffix(flags))
}

// resolvePhase turns each configured line into source.
//
// A phase line that names a file is read; anything else is used as written.
// The alternative -- a separate --prefix-file flag beside every --prefix --
// doubles the surface to say the same thing, and forces a choice at the point
// where the snippet is one line long and the answer is not yet obvious.
func resolvePhase(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	dir := mustGetwd()
	opt := plumbingSourceOptions(dir, []string{".ts", ".js", ".mts", ".mjs"})
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		r, err := source.Resolve(line, opt)
		if err != nil || r.Kind == source.KindText || r.Kind == source.KindNone {
			// A phase that looks like a path but is not readable stays a
			// line. It is more likely to be code than a typo'd filename, and
			// the runtime's own error will be clearer than a guess here.
			out = append(out, line)
			continue
		}
		out = append(out, strings.Split(strings.TrimRight(r.Text, "\n"), "\n")...)
	}
	return out
}
