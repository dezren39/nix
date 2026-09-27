package cli

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/daemon"
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
	if cfg.Path != "" {
		if body, rerr := os.ReadFile(cfg.Path); rerr == nil {
			paths = paths.ForConfig(daemon.FingerprintConfig(cfg.Path, body))
		}
	}

	logger := log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)
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
	if err := srv.Listen(*port); err != nil {
		return err
	}
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
	showPath := fs.Bool("path", false, "print only the config file path")
	if err := fs.Parse(args); err != nil {
		return err
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
	servers, err := cfg.ResolveAll()
	if err != nil {
		return err
	}
	type row struct {
		Name        string `json:"name"`
		Namespace   string `json:"namespace"`
		Mode        string `json:"mode"`
		Max         int    `json:"max"`
		Transport   string `json:"transport"`
		Command     string `json:"command,omitempty"`
		URL         string `json:"url,omitempty"`
		IdleTimeout string `json:"idleTimeout"`
		CallTimeout string `json:"callTimeout"`
	}
	out := struct {
		Path    string `json:"path"`
		Servers []row  `json:"servers"`
	}{Path: cfg.Path}
	for _, s := range servers {
		r := row{
			Name: s.Name, Namespace: s.Namespace, Mode: string(s.Mode), Max: s.Max,
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
  mcpx config [--path]           show the resolved configuration
  mcpx init [--global]           write a starter config

GLOBAL FLAGS
  --config <path>                config file (default: search up from $PWD)
  --json                         machine-readable output
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
