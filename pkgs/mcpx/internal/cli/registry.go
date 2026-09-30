package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/registry"
)

// CmdRegistry searches a registry and adds servers from it.
func (a *App) CmdRegistry(ctx context.Context, args []string) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := newFlagSet("registry")
	// Flags are hoisted ahead of the positionals, because Go's parser stops
	// at the first non-flag argument and `search weather --limit 3` would
	// otherwise take "--limit" and "3" as part of the query. The failure is
	// silent and reads as the registry having no results.
	args = hoistFlags(args, map[string]bool{"limit": true, "config": true})
	// Zero rather than a number: the default is registry.limit, which a
	// config file, MCPX_REGISTRY_LIMIT or --registry-limit may have set. This
	// used to say 20, so none of those reached this command. It cannot be an
	// alias of the setting because search.limit already owns --limit.
	limitFlag := fs.Int("limit", 0, "results to show (default registry.limit)")
	local := fs.Bool("local", false, "prefer an installable package over a remote")
	write := fs.Bool("write", false, "with add: modify the config file instead of printing")
	cfgPath := fs.String("config", "", "with --write: which file to modify")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	client := registry.New(a.Settings().String("registry.url"), a.registryOptions())

	switch sub {
	case "search", "":
		query := strings.Join(fs.Args(), " ")
		limit := *limitFlag
		if limit <= 0 {
			limit = a.Settings().Int("registry.limit")
		}
		res, err := client.Search(ctx, query, limit)
		if err != nil {
			return err
		}
		if a.JSON {
			return a.out(res)
		}
		servers := res.Servers
		if len(servers) == 0 {
			// The registry matches names as a substring, so a sentence finds
			// nothing and the reason is not obvious from an empty list.
			fmt.Printf("Nothing matched %q.\n"+
				"The registry matches server names as a substring, so try one word.\n", query)
			return nil
		}
		for _, s := range servers {
			fmt.Printf("%-44s %s\n", registry.Namespace(s.Name), truncate(s.Description, 60))
			fmt.Printf("%-44s %s\n", "  "+s.Name, dimOffers(s))
		}
		fmt.Printf("\n%d servers. `mcpx registry show <name>` for detail, "+
			"`mcpx registry add <name>` to configure one.\n", len(servers))
		switch {
		case !res.Truncated:
		case len(servers) < limit:
			// Short of the limit and still truncated: the page cap stopped
			// it, and raising --limit would change nothing.
			fmt.Printf("%d shown; more exist -- the search stopped at registry.maxPages "+
				"requests, so raise that or registry.pageSize.\n", len(servers))
		default:
			fmt.Printf("%d shown; more exist -- raise --limit to see them.\n", len(servers))
		}
		return nil

	case "show":
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: mcpx registry show <name>")
		}
		s, err := client.Get(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		if a.JSON {
			return a.out(s)
		}
		fmt.Printf("%s\n%s\n\nversion   %s\n", s.Name, s.Description, s.Version)
		if s.Repository != nil {
			fmt.Printf("source    %s\n", s.Repository.URL)
		}
		if s.WebsiteURL != "" {
			fmt.Printf("website   %s\n", s.WebsiteURL)
		}
		in, ierr := s.ToInstall(*local)
		if ierr != nil {
			fmt.Printf("\n%v\n", ierr)
			return nil
		}
		fmt.Printf("\nmcpx would add it as %q: %s\n", in.Namespace, in.How)
		if in.Command != "" {
			fmt.Printf("  %s %s\n", in.Command, strings.Join(in.Args, " "))
		}
		if in.URL != "" {
			fmt.Printf("  %s\n", in.URL)
		}
		printNeeds(in)
		return nil

	case "add":
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: mcpx registry add <name> [--write]")
		}
		s, err := client.Get(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		in, err := s.ToInstall(*local)
		if err != nil {
			return err
		}
		entry := installEntry(*s, in)
		if !*write {
			// Printing by default. Editing somebody's configuration because
			// they asked what a server looks like would be a surprise, and
			// the printed form is exactly what --write would insert.
			doc, _ := json.MarshalIndent(map[string]any{
				"mcpServers": map[string]any{in.Namespace: entry},
			}, "", "  ")
			fmt.Println(string(doc))
			printNeeds(in)
			fmt.Printf("\nAdd it with `mcpx registry add %s --write`, "+
				"or paste the block above.\n", fs.Arg(0))
			return nil
		}
		path, err := a.addServerToConfig(*cfgPath, in.Namespace, entry)
		if err != nil {
			return err
		}
		fmt.Printf("added %s to %s\n", in.Namespace, path)
		printNeeds(in)
		return nil
	}
	return fmt.Errorf("no registry subcommand %q; search, show or add", sub)
}

func dimOffers(s registry.Server) string {
	var parts []string
	for _, p := range s.Packages {
		parts = append(parts, p.RegistryType)
	}
	for _, r := range s.Remotes {
		parts = append(parts, r.Type)
	}
	if len(parts) == 0 {
		return "(nothing installable)"
	}
	return strings.Join(parts, ", ")
}

// printNeeds reports what has to be supplied before a server will start.
//
// Said here rather than discovered at start time, because a server that exits
// immediately for a missing key looks broken, and the registry already told
// us which keys those are.
func printNeeds(in *registry.Install) {
	if len(in.Needs) == 0 {
		return
	}
	fmt.Println("\nIt needs these set before it will start:")
	for _, v := range in.Needs {
		note := v.Description
		if v.IsSecret {
			note = strings.TrimSpace(note + " (secret)")
		}
		fmt.Printf("  %-28s %s\n", v.Name, note)
	}
}

func installEntry(s registry.Server, in *registry.Install) map[string]any {
	entry := map[string]any{}
	if in.URL != "" {
		entry["url"] = in.URL
		if in.Transport != "" {
			entry["transport"] = in.Transport
		}
	} else {
		entry["command"] = in.Command
		if len(in.Args) > 0 {
			entry["args"] = in.Args
		}
	}
	if len(in.Env) > 0 {
		entry["env"] = in.Env
	}
	mcpx := map[string]any{"description": s.Description}
	// A server nobody has run yet gets the cautious default. Shared and
	// global is right for a stateless server and wrong for a stateful one,
	// and the registry does not say which this is.
	mcpx["sharing"] = "shared"
	mcpx["scope"] = "global"
	entry["mcpx"] = mcpx
	return entry
}

// addServerToConfig inserts a server into a configuration file.
//
// The read-modify-write lives in internal/config now, because the daemon and
// `mcpx servers add` need the identical thing and three copies of it would be
// three chances to truncate somebody's file.
func (a *App) addServerToConfig(explicit, name string, entry map[string]any) (string, error) {
	path := explicit
	if path == "" {
		cfg, _ := config.Load(a.ConfigPath)
		if cfg != nil && cfg.Path != "" {
			path = cfg.Path
		}
	}
	if path == "" {
		path = ".mcpx.json"
	}
	if err := config.AddServer(path, name, entry, false); err != nil {
		return "", err
	}
	// The daemon re-reads its configuration on refresh, so a server added
	// here is callable without a restart. Best effort: a write that
	// succeeded is not undone because no daemon was listening.
	if c := a.Client(); c != nil && c.Ping(context.Background()) {
		if _, err := c.Refresh(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "mcpx: added, but the daemon did not reload: %v\n", err)
		}
	}
	return path, nil
}

// registryServers is what the TUI and scripts read.
// registrySearch returns the whole result, truncation included. A caller that
// drops it cannot tell a complete answer from a cut one, which is the only
// thing that makes "raise --limit" the right advice.
func (a *App) registrySearch(ctx context.Context, query string, limit int) (registry.Results, error) {
	return registry.New(a.Settings().String("registry.url"), a.registryOptions()).Search(ctx, query, limit)
}

// hoistFlags moves flags ahead of positional arguments.
//
// Go's flag package stops parsing at the first non-flag, which is the right
// behaviour for a command that forwards the rest to something else and the
// wrong behaviour for one that takes a query. Rather than teach every user
// that flags come first, the arguments are reordered.
//
// valued names the flags that take a following value, since "--limit 3" is
// two arguments and "--json" is one, and moving only half of a pair would be
// worse than not moving it.
func hoistFlags(args []string, valued map[string]bool) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			rest = append(rest, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			continue // --limit=3 is self-contained
		}
		if valued[name] && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, rest...)
}

// registryOptions carries the knobs the registry client used to take from
// built-in constants, so registry.timeout, registry.pageSize and
// registry.maxPages mean something wherever they are set.
func (a *App) registryOptions() registry.Options {
	return registry.Options{
		Timeout:  a.Settings().Duration("registry.timeout"),
		PageSize: a.Settings().Int("registry.pageSize"),
		MaxPages: a.Settings().Int("registry.maxPages"),
	}
}
