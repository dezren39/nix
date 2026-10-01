package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dezren39/mcpx/internal/adapter"
	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// AdapterFile is a document declaring command-line programs as servers.
type AdapterFile struct {
	Adapters []adapter.Spec `json:"adapters"`
}

// loadAdapters reads declarations from the configured paths.
func (a *App) loadAdapters() ([]adapter.Spec, error) {
	return loadAdapterFiles(splitPathList(a.Settings().String("paths.adapters")))
}

// loadAdapterFiles reads and validates declarations from these files.
func loadAdapterFiles(paths []string) ([]adapter.Spec, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	var out []adapter.Spec
	for _, p := range paths {
		if p == "" {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			// A named file that is not there is an error, unlike a search
			// directory: somebody wrote this path down on purpose.
			return nil, fmt.Errorf("reading adapters from %s: %w", p, err)
		}
		var doc AdapterFile
		if err := json.Unmarshal(config.StripJSONC(b), &doc); err != nil {
			// A bare array is the obvious other thing somebody writes.
			var bare []adapter.Spec
			if json.Unmarshal(config.StripJSONC(b), &bare) != nil {
				return nil, fmt.Errorf("%s is not a valid adapter file: %w", p, err)
			}
			doc.Adapters = bare
		}
		out = append(out, doc.Adapters...)
	}
	seen := map[string]string{}
	for _, s := range out {
		if err := s.Validate(); err != nil {
			return nil, err
		}
		if prev, dup := seen[s.Name]; dup {
			return nil, fmt.Errorf("two adapters named %q (%s and here)", s.Name, prev)
		}
		seen[s.Name] = s.Name
	}
	return out, nil
}

// CmdAdapter inspects and runs adapted programs.
//
// A subcommand rather than a flag because the three things somebody wants --
// what is declared, does it work, run one -- are different questions and a
// flag would have to guess which.
func (a *App) CmdAdapter(ctx context.Context, args []string) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := newFlagSet("adapter")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	specs, err := a.loadAdapters()
	if err != nil {
		return err
	}

	switch sub {
	case "", "list":
		if a.JSON {
			return a.out(specs)
		}
		if len(specs) == 0 {
			fmt.Println("No adapters declared. Point paths.adapters at a file:\n" +
				`  { "adapters": [ { "name": "jq", "command": "jq",` + "\n" +
				`      "tools": [ { "name": "run", "params": [{"name":"filter"}] } ] } ] }`)
			return nil
		}
		for _, s := range specs {
			state := "ok"
			if err := s.Probe(); err != nil {
				state = "missing"
			}
			fmt.Printf("%-16s %-10s %s (%d tools)\n", s.Name, state, s.Command, len(s.Tools))
			for _, t := range s.Tools {
				fmt.Printf("    %-14s %s\n", t.Name, firstLine(t.Description))
			}
		}
		return nil

	case "check":
		var problems []string
		for _, s := range specs {
			if err := s.Probe(); err != nil {
				problems = append(problems, err.Error())
			}
		}
		if len(problems) > 0 {
			sort.Strings(problems)
			return fmt.Errorf("adapters are not runnable:\n  %s", strings.Join(problems, "\n  "))
		}
		fmt.Printf("%d adapters, all runnable.\n", len(specs))
		return nil

	case "call":
		if len(fs.Args()) < 1 {
			return fmt.Errorf("usage: mcpx adapter call <name>.<tool> '<json>'")
		}
		name, tool, ok := strings.Cut(fs.Arg(0), ".")
		if !ok {
			return fmt.Errorf("name the tool as <adapter>.<tool>")
		}
		var payload map[string]any
		if len(fs.Args()) > 1 {
			if err := json.Unmarshal([]byte(fs.Arg(1)), &payload); err != nil {
				return fmt.Errorf("arguments must be a JSON object: %w", err)
			}
		}
		for _, s := range specs {
			if s.Name != name {
				continue
			}
			res, err := s.Call(ctx, tool, payload)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.out(res)
			}
			if res.Stdout != "" {
				fmt.Print(res.Stdout)
			}
			if res.Stderr != "" {
				fmt.Fprint(os.Stderr, res.Stderr)
			}
			if res.ExitCode != 0 {
				return fmt.Errorf("%s exited %d", name, res.ExitCode)
			}
			return nil
		}
		return fmt.Errorf("no adapter named %q", name)

	case "tools":
		// The MCP tool list an adapter would present, so it can be checked
		// against what a host will see without starting anything.
		var out []mcpserver.Tool
		for _, s := range specs {
			for _, t := range s.Tools {
				out = append(out, mcpserver.Tool{
					Name:        s.Name + "_" + t.Name,
					Description: t.Description,
					InputSchema: t.Schema(),
				})
			}
		}
		return a.out(out)

	case "serve":
		// One adapter as a stdio MCP server. This is what the daemon spawns
		// for every declared adapter (see augmentAdapters), so an adapted
		// program is an upstream server like any other.
		// Files after the name replace paths.adapters, so the daemon can
		// say exactly which declarations it meant whatever the child's
		// environment or working directory.
		if len(fs.Args()) < 1 {
			return fmt.Errorf("usage: mcpx adapter serve <name> [file...]")
		}
		if len(fs.Args()) > 1 {
			if specs, err = loadAdapterFiles(fs.Args()[1:]); err != nil {
				return err
			}
		}
		for _, s := range specs {
			if s.Name != fs.Arg(0) {
				continue
			}
			srv := mcpserver.New(nil, s.Name, a.Version)
			srv.ExtrasOnly = true
			srv.OwnInstructions = s.Instructions
			srv = srv.WithExtras(adapterTools([]adapter.Spec{s}, false))
			return srv.ServeStdio(ctx, os.Stdin, os.Stdout)
		}
		return fmt.Errorf("no adapter named %q", fs.Arg(0))
	}
	return fmt.Errorf("no adapter subcommand %q; list, check, call, tools or serve", sub)
}

// augmentAdapters adds every declared adapter to cfg as a server of its
// own: `mcpx adapter serve <name>`, under the adapter's name.
//
// That is the whole of making an adapter first-class (#83, #102). Attached
// only as mcpserver extras, adapters reached mcpx's own tools/list and
// nothing else -- not the pool, so not `mcpx ls`, `types`, `search`,
// `catalog`, /v1 or the generated client a script calls. As a server they
// get all of those by the same path a configured server does.
func (a *App) augmentAdapters(cfg *config.Config) error {
	specs, err := a.loadAdapters()
	if err != nil || len(specs) == 0 {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("adapters need mcpx's own path to serve them: %w", err)
	}
	// The child reads the same files whatever its working directory.
	var files []string
	for _, p := range splitPathList(a.Settings().String("paths.adapters")) {
		if p == "" {
			continue
		}
		if abs, aerr := filepath.Abs(p); aerr == nil {
			p = abs
		}
		files = append(files, p)
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = map[string]*config.Server{}
	}
	for _, s := range specs {
		if _, dup := cfg.MCPServers[s.Name]; dup {
			return fmt.Errorf("adapter %q has the same name as a configured server; rename one", s.Name)
		}
		cfg.MCPServers[s.Name] = &config.Server{
			Name:    s.Name,
			Command: self,
			Args:    append([]string{"adapter", "serve", s.Name}, files...),
			// Instructions arrive from initialize, as a real server's do.
			Mcpx: &config.Extras{Description: s.Description},
		}
	}
	return nil
}
