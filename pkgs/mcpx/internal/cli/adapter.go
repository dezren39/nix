package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	raw := a.Settings().String("paths.adapters")
	paths := splitPathList(raw)
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
	}
	return fmt.Errorf("no adapter subcommand %q; list, check, call or tools", sub)
}
