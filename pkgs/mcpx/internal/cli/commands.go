package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/daemon"
	"github.com/dezren39/mcpx/internal/runner"
)

// App carries state shared by every subcommand.
type App struct {
	Version    string
	ConfigPath string
	JSON       bool
	Paths      daemon.Paths
	client     *Client
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
		a.client = NewClient(paths, cfgPath)
	}
	return a.client
}

func (a *App) ensure(ctx context.Context) (*Client, error) {
	c := a.Client()
	if err := c.EnsureDaemon(ctx); err != nil {
		return nil, err
	}
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	nss, err := c.Namespaces(ctx)
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
	fmt.Fprintln(tw, "NAMESPACE\tTOOLS\tMODE\tLIVE\tSTATE\tDESCRIPTION")
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
		fmt.Fprintf(tw, "%s\t%d\t%s\t%d\t%s\t%s\n", n.Namespace, n.Tools, n.Mode, n.Live, state, desc)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	ns := splitAll(fs.Args())
	if len(ns) == 0 {
		return errors.New("usage: mcpx types <namespace>[,<namespace>...]\n" +
			"       (listing every namespace at once defeats the purpose; run `mcpx ls` first)")
	}
	if err := a.ensureSchemas(ctx, c, ns); err != nil {
		return err
	}
	text, err := c.Types(ctx, ns)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

// ensureSchemas triggers a fetch for namespaces that have never been read.
func (a *App) ensureSchemas(ctx context.Context, c *Client, ns []string) error {
	known, err := c.Namespaces(ctx)
	if err != nil {
		return err
	}
	byName := map[string]daemon.NamespaceInfo{}
	for _, n := range known {
		byName[n.Namespace] = n
		byName[n.Server] = n
	}
	needRefresh := false
	for _, want := range ns {
		n, ok := byName[want]
		if !ok {
			names := make([]string, 0, len(known))
			for _, k := range known {
				names = append(names, k.Namespace)
			}
			sort.Strings(names)
			return fmt.Errorf("unknown namespace %q; available: %s", want, strings.Join(names, ", "))
		}
		if !n.Cached {
			needRefresh = true
		}
	}
	if !needRefresh {
		return nil
	}
	fmt.Fprintln(os.Stderr, "mcpx: reading tool schemas for the first time...")
	_, err = c.Refresh(ctx)
	return err
}

// ---- search ----

// CmdSearch ranks tools across every namespace.
func (a *App) CmdSearch(ctx context.Context, args []string) error {
	fs := newFlagSet("search")
	limit := fs.Int("n", 20, "max results")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("usage: mcpx search <query>")
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	hits, err := c.Search(ctx, strings.Join(fs.Args(), " "), *limit)
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
	if err := fs.Parse(args); err != nil {
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
	res, err := c.Call(ctx, ns, tool, *session, argsJSON)
	if err != nil {
		return err
	}
	if *raw || a.JSON {
		return a.out(json.RawMessage(res.Result))
	}
	fmt.Println(renderResult(res.Result))
	return nil
}

// renderResult unwraps a CallToolResult the same way the script client does,
// so CLI and script output agree.
func renderResult(raw json.RawMessage) string {
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return string(raw)
	}
	if len(r.StructuredContent) > 0 {
		var buf strings.Builder
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if enc.Encode(json.RawMessage(r.StructuredContent)) == nil {
			return strings.TrimRight(buf.String(), "\n")
		}
	}
	var texts []string
	for _, c := range r.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n")
	}
	return string(raw)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		if inline {
			return errors.New("usage: mcpx exec '<typescript>'")
		}
		return errors.New("usage: mcpx run <script.ts> [args...]")
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

	sessionKey := *session
	if sessionKey == "" {
		sessionKey = newSessionKey()
	}
	// Free any pinned instances (browsers) as soon as the script ends, rather
	// than leaving them parked until the idle timer fires.
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.ReleaseSession(rctx, sessionKey)
	}()

	// The session is deliberately not baked into the generated module: several
	// concurrent runs share one client file, and each must keep its own
	// session so session-mode pools hand out separate processes.
	clientSrc, err := c.ClientModule(ctx, ns, "")
	if err != nil {
		return err
	}
	endpoint, err := c.Endpoint(ctx)
	if err != nil {
		return err
	}
	prelude, err := a.buildPrelude(ctx, c, ns)
	if err != nil {
		return err
	}

	cfg, _ := config.Load(a.ConfigPath)
	runtimePref := *rt
	if runtimePref == "" && cfg != nil {
		runtimePref = cfg.Runtime
	}

	opts := runner.Options{
		ClientSource: clientSrc,
		Runtime:      runtimePref,
		Timeout:      *timeout,
		Prelude:      prelude,
		Env: map[string]string{
			"MCPX_SESSION":  sessionKey,
			"MCPX_ENDPOINT": endpoint,
			// Path facts travel through the environment so that a module three
			// imports deep sees the same values as the entry script, without
			// anything being threaded through call signatures.
			"MCPX_CWD":         mustGetwd(),
			"MCPX_SCRIPT_DIRS": strings.Join(scriptSearchDirs(), ":"),
			"MCPX_CONFIG_PATH": configPathOf(cfg),
		},
	}
	if inline {
		opts.Source = strings.Join(fs.Args(), " ")
	} else {
		// A bare name resolves through .mcpx/scripts; anything path-shaped is
		// used verbatim.
		file, rerr := resolveScript(fs.Arg(0))
		if rerr != nil {
			return rerr
		}
		opts.File = file
		opts.Args = fs.Args()[1:]
	}
	if *keep {
		dir, err := os.MkdirTemp("", "mcpx-keep-")
		if err != nil {
			return err
		}
		opts.WorkDir = dir
		fmt.Fprintf(os.Stderr, "mcpx: workdir %s\n", dir)
	}

	res, err := runner.Run(ctx, opts)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		os.Exit(res.ExitCode)
	}
	return nil
}

// ensureAnySchemas fetches schemas once if nothing has ever been cached.
func (a *App) ensureAnySchemas(ctx context.Context, c *Client) error {
	known, err := c.Namespaces(ctx)
	if err != nil {
		return err
	}
	for _, n := range known {
		if n.Cached {
			return nil
		}
	}
	if len(known) == 0 {
		return nil
	}
	fmt.Fprintln(os.Stderr, "mcpx: reading tool schemas for the first time...")
	_, err = c.Refresh(ctx)
	return err
}

// buildPrelude imports the client and binds each namespace as a bare
// identifier, so an agent can write either `fff.search(...)` or
// `tools.fff.search(...)`.
func (a *App) buildPrelude(ctx context.Context, c *Client, ns []string) (string, error) {
	known, err := c.Namespaces(ctx)
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

	var b strings.Builder
	b.WriteString("// --- mcpx prelude (generated) ---\n")
	fmt.Fprintf(&b, "import tools, { call, readResource, ToolError } from %q;\n",
		"./"+runner.ClientFileName)
	if len(names) > 0 {
		fmt.Fprintf(&b, "const { %s } = tools;\n", strings.Join(names, ", "))
	}
	b.WriteString("void [tools, call, readResource, ToolError")
	for _, n := range names {
		b.WriteString(", " + n)
	}
	b.WriteString("];\n// --- end prelude ---\n\n")
	return b.String(), nil
}

// ---- client (write the module for hand-written scripts) ----

// CmdClient writes the generated client module to a path so an editor and a
// checked-in script can both see it.
func (a *App) CmdClient(ctx context.Context, args []string) error {
	fs := newFlagSet("client")
	outPath := fs.String("o", "", "write to this path (default: stdout)")
	nsFlag := fs.String("ns", "", "restrict to these namespaces")
	if err := fs.Parse(args); err != nil {
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
	src, err := c.ClientModule(ctx, ns, "")
	if err != nil {
		return err
	}
	if *outPath == "" {
		fmt.Print(src)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(*outPath, []byte(src), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", *outPath)
	return nil
}

// ---- status / admin ----

// CmdStatus prints daemon and pool state.
func (a *App) CmdStatus(ctx context.Context, args []string) error {
	fs := newFlagSet("status")
	if err := fs.Parse(args); err != nil {
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
		Mode      string `json:"mode"`
		Max       int    `json:"max"`
		Live      int    `json:"live"`
		Tools     int    `json:"tools"`
		SchemaAge string `json:"schemaAge"`
		LastError string `json:"lastError"`
		Instances []struct {
			ID        string `json:"id"`
			PID       int    `json:"pid"`
			Busy      bool   `json:"busy"`
			Session   string `json:"session"`
			Calls     int64  `json:"calls"`
			UptimeSec int    `json:"uptimeSec"`
			IdleSec   int    `json:"idleSec"`
		} `json:"instances"`
	}
	_ = json.Unmarshal(b, &servers)

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tMODE\tLIVE/MAX\tTOOLS\tSCHEMA\tERROR")
	for _, s := range servers {
		fmt.Fprintf(tw, "%s\t%s\t%d/%d\t%d\t%s\t%s\n",
			s.Namespace, s.Mode, s.Live, s.Max, s.Tools, s.SchemaAge, truncate(oneLine(s.LastError), 50))
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
		fmt.Fprintln(tw, "INSTANCE\tPID\tBUSY\tCALLS\tUPTIME\tIDLE\tSESSION")
		for _, s := range servers {
			for _, in := range s.Instances {
				fmt.Fprintf(tw, "%s\t%d\t%v\t%d\t%ds\t%ds\t%s\n",
					in.ID, in.PID, in.Busy, in.Calls, in.UptimeSec, in.IdleSec, truncate(in.Session, 18))
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
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
		time.Sleep(100 * time.Millisecond)
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
	if err := fs.Parse(args); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	if err := os.WriteFile(path, []byte(starterConfig), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	fmt.Println("Edit it, then run `mcpx ls`.")
	return nil
}

const starterConfig = `{
  // mcpx reads the same "mcpServers" object other MCP hosts use, so an
  // existing config can be pasted in unchanged. The optional "mcpx" block on
  // each server controls how many processes are kept and how they are shared.
  "mcpServers": {
    "example-stateless": {
      "command": "some-mcp-server",
      "args": [],
      "mcpx": {
        // One process, unlimited concurrent callers. Right for search, docs
        // and database servers, which hold no per-caller state.
        "mode": "shared",
        "description": "what this server is for, shown by mcpx ls"
      }
    },
    "chrome-devtools": {
      "command": "chrome-devtools-mcp",
      "args": ["--headless", "--isolated"],
      "mcpx": {
        // One browser per script run, up to 4 at once. Concurrent agents get
        // separate browsers instead of fighting over one.
        "mode": "session",
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
	if err := fs.Parse(args); err != nil {
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
