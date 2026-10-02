package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/daemon"
	"github.com/dezren39/mcpx/internal/settings"
)

// CmdSettings reads and changes settings.
//
// `mcpx config --schema` already printed the registry, but it printed the
// declarations: what exists, not what is in force. The question people
// actually have is the other one -- what is this set to, and why -- and
// answering it meant reading a config file, then the environment, then the
// command line, and reconciling three. This prints the answer and its
// provenance in one line.
func (a *App) CmdSettings(ctx context.Context, args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		return a.settingsList(ctx, args)
	case "get":
		return a.settingsGet(ctx, args)
	case "set":
		return a.settingsSet(ctx, args)
	case "unset":
		return a.settingsUnset(ctx, args)
	}
	return fmt.Errorf("no settings subcommand %q; list, get, set or unset", sub)
}

func (a *App) settingsList(ctx context.Context, args []string) error {
	fs := newFlagSet("settings")
	args = hoistFlags(args, map[string]bool{"scope": true})
	scope := fs.String("scope", "", "only settings read by daemon, client, call or plugin")
	withPlumbing := fs.Bool("plumbing", false, "include internal settings")
	changed := fs.Bool("changed", false, "only settings something has overridden")
	fromDaemon := fs.Bool("daemon", false, "ask the daemon what it resolved, rather than resolving here")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	filter := strings.ToLower(fs.Arg(0))

	var rows []settingRow
	if *fromDaemon {
		var err error
		if rows, err = a.remoteSettings(ctx, *scope, *withPlumbing, *changed); err != nil {
			return err
		}
	} else {
		rows = a.localSettings(*scope, *withPlumbing, *changed)
	}
	if filter != "" {
		kept := rows[:0]
		for _, r := range rows {
			if strings.Contains(strings.ToLower(r.Path), filter) ||
				strings.Contains(strings.ToLower(r.Short), filter) {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	if a.JSON {
		return a.out(map[string]any{"settings": rows})
	}
	if len(rows) == 0 {
		fmt.Println("nothing matched")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SETTING\tVALUE\tFROM\tSCOPE\tHOT")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			r.Path, quoteEmpty(r.Value), r.Source, r.Scope, yesNo(r.Hot))
	}
	tw.Flush()
	if !*withPlumbing {
		fmt.Println("\n(--plumbing also lists internal settings; --daemon shows what the daemon resolved)")
	}
	return nil
}

func (a *App) settingsGet(ctx context.Context, args []string) error {
	fs := newFlagSet("settings")
	// Go's parser stops at the first positional, and the path is one, so
	// `settings get x --daemon` would silently drop the flag and answer from
	// the wrong process -- which is exactly the class of bug this command
	// exists to expose.
	args = hoistFlags(args, nil)
	fromDaemon := fs.Bool("daemon", false, "ask the daemon rather than resolving here")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: mcpx settings get <path>")
	}
	path := fs.Arg(0)

	var row settingRow
	if *fromDaemon {
		rows, err := a.remoteSettings(ctx, "", true, false)
		if err != nil {
			return err
		}
		found := false
		for _, r := range rows {
			if r.Path == path {
				row, found = r, true
				break
			}
		}
		if !found {
			return fmt.Errorf("the daemon has no setting %q", path)
		}
	} else {
		decl, ok := a.Settings().Schema().Lookup(path)
		if !ok {
			return fmt.Errorf("no setting %q; `mcpx settings list` shows them all", path)
		}
		v, _ := a.Settings().Value(path)
		row = toRow(daemon.Describe(*decl, v))
	}
	if a.JSON {
		return a.out(row)
	}
	fmt.Printf("%s = %s\n", row.Path, quoteEmpty(row.Value))
	fmt.Printf("  from      %s\n", row.Source)
	if len(row.Shadowed) > 0 {
		fmt.Printf("  overrode  %s\n", strings.Join(row.Shadowed, ", "))
	}
	fmt.Printf("  default   %s\n", quoteEmpty(row.Default))
	fmt.Printf("  kind      %s\n", row.Kind)
	if len(row.Enum) > 0 {
		fmt.Printf("  one of    %s\n", settings.Setting{Enum: row.Enum, EnumAliases: row.EnumAliases}.EnumWords())
	}
	fmt.Printf("  scope     %s (%s)\n", row.Scope, hotWords(row.Hot))
	fmt.Printf("  flag      %s\n", row.Flag)
	fmt.Printf("  variable  %s\n", row.Env)
	if row.Short != "" {
		fmt.Printf("\n%s\n", row.Short)
	}
	if row.Long != "" {
		fmt.Printf("\n%s\n", row.Long)
	}
	return nil
}

func (a *App) settingsSet(ctx context.Context, args []string) error {
	fs := newFlagSet("settings")
	args = hoistFlags(args, map[string]bool{"persist": true})
	persist := fs.String("persist", "runtime",
		"where to keep the change: runtime, project or user")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: mcpx settings set <path> <value> [--persist project|user]")
	}
	path := fs.Arg(0)
	value := strings.Join(fs.Args()[1:], " ")
	// `settings set x=1` as well as `settings set x 1`, because both are
	// what people type and refusing one teaches nothing.
	if eq := strings.IndexByte(path, '='); eq >= 0 && fs.NArg() == 1 {
		path, value = path[:eq], path[eq+1:]
	}
	decl, ok := a.Settings().Schema().Lookup(path)
	if !ok {
		return fmt.Errorf("no setting %q; `mcpx settings list` shows them all", path)
	}
	// Before the daemon is asked, because a refusal from it falls through to
	// the local write below, which would then persist a key nothing reads.
	if err := decl.Writable(); err != nil {
		return err
	}
	value, err := settings.Normalize(*decl, value)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	// Through the daemon when there is one, so a hot setting takes effect at
	// once and the daemon's own view agrees with the file. Locally when
	// there is not, because refusing to write a config file for want of a
	// daemon would be absurd.
	if c, err := a.ensure(ctx); err == nil {
		body, _ := json.Marshal(map[string]string{"value": value, "persist": *persist})
		raw, derr := c.Do(ctx, http.MethodPut, "/v1/settings/"+urlEscape(path), body)
		if derr == nil {
			var out map[string]any
			if json.Unmarshal(raw, &out) == nil {
				if a.JSON {
					return a.out(out)
				}
				return printSetResult(path, value, out)
			}
		}
	}
	if *persist == "runtime" {
		return fmt.Errorf("a runtime change needs a running daemon; " +
			"use --persist project or --persist user to write it to a file")
	}
	file, err := config.ResolveWritePath(config.WriteScope(*persist), a.ConfigPath)
	if err != nil {
		return err
	}
	if err := config.SetKey(file, path, settings.TypedValue(*decl, value)); err != nil {
		return err
	}
	if a.JSON {
		return a.out(map[string]any{"path": path, "value": value, "written": file})
	}
	fmt.Printf("%s = %s, written to %s\n", path, quoteEmpty(value), file)
	return nil
}

func printSetResult(path, value string, out map[string]any) error {
	fmt.Printf("%s = %s\n", path, quoteEmpty(value))
	if w, _ := out["written"].(string); w != "" {
		fmt.Printf("  written to %s\n", w)
	}
	if applied, _ := out["applied"].(bool); applied {
		fmt.Println("  applied to the running daemon")
	}
	if note, _ := out["note"].(string); note != "" {
		fmt.Printf("  %s\n", note)
	}
	return nil
}

func (a *App) settingsUnset(ctx context.Context, args []string) error {
	fs := newFlagSet("settings")
	args = hoistFlags(args, map[string]bool{"persist": true})
	persist := fs.String("persist", "runtime",
		"what to drop: the runtime override, or the entry in the project or user file")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: mcpx settings unset <path>")
	}
	path := fs.Arg(0)
	if _, ok := a.Settings().Schema().Lookup(path); !ok {
		return fmt.Errorf("no setting %q", path)
	}

	if *persist != "runtime" {
		file, err := config.ResolveWritePath(config.WriteScope(*persist), a.ConfigPath)
		if err != nil {
			return err
		}
		removed, err := config.DeleteKey(file, path)
		if err != nil {
			return err
		}
		if a.JSON {
			return a.out(map[string]any{"path": path, "removed": removed, "file": file})
		}
		if !removed {
			fmt.Printf("%s was not set in %s\n", path, file)
			return nil
		}
		fmt.Printf("removed %s from %s\n", path, file)
		return nil
	}

	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	raw, err := c.Do(ctx, http.MethodDelete, "/v1/settings/"+urlEscape(path), nil)
	if err != nil {
		return err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if a.JSON {
		return a.out(out)
	}
	if removed, _ := out["removed"].(bool); !removed {
		fmt.Printf("%s had no runtime override\n", path)
		return nil
	}
	fmt.Printf("%s is back to %v (%v)\n", path, out["value"], out["source"])
	return nil
}

// settingRow is what both the local and the remote listing produce, so the
// two render identically. They came from one declaration; they should not
// look like two features.
type settingRow struct {
	Path        string              `json:"path"`
	Kind        string              `json:"kind"`
	Value       string              `json:"value"`
	Default     string              `json:"default"`
	Source      string              `json:"source"`
	Shadowed    []string            `json:"shadowed,omitempty"`
	Env         string              `json:"env"`
	Flag        string              `json:"flag"`
	Short       string              `json:"description,omitempty"`
	Long        string              `json:"detail,omitempty"`
	Enum        []string            `json:"enum,omitempty"`
	EnumAliases map[string][]string `json:"enumAliases,omitempty"`
	Scope       string              `json:"scope"`
	Hot         bool                `json:"hot"`
	Plumbing    bool                `json:"plumbing,omitempty"`
}

func toRow(v any) settingRow {
	b, _ := json.Marshal(v)
	var r settingRow
	_ = json.Unmarshal(b, &r)
	return r
}

func (a *App) localSettings(scope string, plumbing, changed bool) []settingRow {
	set := a.Settings()
	var out []settingRow
	for _, decl := range set.Schema().All() {
		if decl.Plumbing && !plumbing {
			continue
		}
		if scope != "" && !strings.EqualFold(decl.Scope.String(), scope) {
			continue
		}
		v, _ := set.Value(decl.Path)
		if changed && (v == nil || v.Origin.Layer == settings.LayerDefault) {
			continue
		}
		out = append(out, toRow(daemon.Describe(decl, v)))
	}
	return out
}

func (a *App) remoteSettings(ctx context.Context, scope string, plumbing, changed bool) ([]settingRow, error) {
	c, err := a.ensure(ctx)
	if err != nil {
		return nil, err
	}
	q := "?_"
	if scope != "" {
		q += "&scope=" + urlEscape(scope)
	}
	if plumbing {
		q += "&plumbing=1"
	}
	if changed {
		q += "&changed=1"
	}
	raw, err := c.Do(ctx, http.MethodGet, "/v1/settings"+q, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Settings []settingRow `json:"settings"`
	}
	return out.Settings, json.Unmarshal(raw, &out)
}

// ---- servers ----

// CmdServers lists, adds and removes configured servers.
//
// The answer to "can mcpx add an MCP server without me editing JSON", which
// until now was no everywhere except `mcpx registry add --write`, and that
// only for servers a public registry already knew about. A server somebody
// wrote this morning could not be added by any means except an editor.
func (a *App) CmdServers(ctx context.Context, args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		return a.serversList(ctx)
	case "add":
		return a.serversAdd(ctx, args)
	case "remove", "rm", "delete":
		return a.serversRemove(ctx, args)
	}
	return fmt.Errorf("no servers subcommand %q; list, add or remove", sub)
}

func (a *App) serversList(ctx context.Context) error {
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	raw, err := c.Do(ctx, http.MethodGet, "/v1/servers", nil)
	if err != nil {
		return err
	}
	var out struct {
		Servers []map[string]any `json:"servers"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if a.JSON {
		return a.out(out)
	}
	if len(out.Servers) == 0 {
		fmt.Println("no servers configured; `mcpx servers add <name> -- <command>` adds one")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tNAMESPACE\tHOW\tDEFINED IN")
	for _, s := range out.Servers {
		how, _ := s["command"].(string)
		if how == "" {
			how, _ = s["url"].(string)
		}
		if args, ok := s["args"].([]any); ok && len(args) > 0 {
			parts := make([]string, 0, len(args))
			for _, x := range args {
				parts = append(parts, fmt.Sprint(x))
			}
			how += " " + strings.Join(parts, " ")
		}
		fmt.Fprintf(tw, "%v\t%v\t%s\t%v\n", s["name"], s["namespace"], truncate(how, 48), s["from"])
	}
	return tw.Flush()
}

func (a *App) serversAdd(ctx context.Context, args []string) error {
	fs := newFlagSet("servers")
	// Everything after -- is the command, so a server's own flags cannot be
	// mistaken for mcpx's. Hoisting first, because Go stops parsing at the
	// first positional and the name is one.
	var after []string
	for i, v := range args {
		if v == "--" {
			args, after = args[:i], args[i+1:]
			break
		}
	}
	args = hoistFlags(args, map[string]bool{
		"url": true, "transport": true, "env": true, "header": true,
		"scope": true, "namespace": true, "sharing": true, "pool-scope": true,
		"description": true,
	})
	url := fs.String("url", "", "a remote server's endpoint, instead of a command")
	transport := fs.String("transport", "", "stdio, http or sse")
	env := newRepeated()
	fs.Var(env, "env", "KEY=VALUE for the server's environment (repeatable)")
	header := newRepeated()
	fs.Var(header, "header", "KEY=VALUE sent to a remote server (repeatable)")
	scope := fs.String("scope", "project", "which config file to write: project or user")
	namespace := fs.String("namespace", "", "expose it under this namespace instead of its name")
	sharing := fs.String("sharing", "", "shared or exclusive")
	poolScope := fs.String("pool-scope", "", "global, repo, worktree, cwd, session, parent-session, pid or call")
	desc := fs.String("description", "", "one line about what it is for")
	replace := fs.Bool("replace", false, "overwrite an entry of the same name")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: mcpx servers add <name> -- <command> [args...]\n" +
			"       mcpx servers add <name> --url https://...")
	}
	name := fs.Arg(0)
	command := ""
	var cmdArgs []string
	rest := append(fs.Args()[1:], after...)
	if len(rest) > 0 {
		command, cmdArgs = rest[0], rest[1:]
	}

	body := map[string]any{"name": name, "scope": *scope, "replace": *replace}
	if *url != "" {
		body["url"] = *url
		if len(header.values) > 0 {
			body["headers"] = pairs(header.values)
		}
	} else {
		if command == "" {
			return fmt.Errorf("a server needs either a command after -- or a --url")
		}
		body["command"] = command
		if len(cmdArgs) > 0 {
			body["args"] = cmdArgs
		}
	}
	if *transport != "" {
		body["transport"] = *transport
	}
	if len(env.values) > 0 {
		body["env"] = pairs(env.values)
	}
	mcpx := map[string]any{}
	for k, v := range map[string]string{
		"namespace": *namespace, "sharing": *sharing,
		"scope": *poolScope, "description": *desc,
	} {
		if v != "" {
			mcpx[k] = v
		}
	}
	if len(mcpx) > 0 {
		body["mcpx"] = mcpx
	}

	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	raw, err := c.Do(ctx, http.MethodPost, "/v1/servers", mustMarshal(body))
	if err != nil {
		return err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if a.JSON {
		return a.out(out)
	}
	fmt.Printf("added %s to %v; it is live now\n", name, out["written"])
	return nil
}

func (a *App) serversRemove(ctx context.Context, args []string) error {
	fs := newFlagSet("servers")
	args = hoistFlags(args, map[string]bool{"scope": true})
	scope := fs.String("scope", "", "which config file to edit; by default, the one that defines it")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: mcpx servers remove <name>")
	}
	name := fs.Arg(0)
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	q := ""
	if *scope != "" {
		q = "?scope=" + urlEscape(*scope)
	}
	raw, err := c.Do(ctx, http.MethodDelete, "/v1/servers/"+urlEscape(name)+q, nil)
	if err != nil {
		return err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if a.JSON {
		return a.out(out)
	}
	fmt.Printf("removed %s from %v; it is gone from the running daemon\n", name, out["written"])
	return nil
}

// ---- helpers ----

// repeated collects a flag given more than once.
type repeated struct{ values []string }

func newRepeated() *repeated           { return &repeated{} }
func (r *repeated) String() string     { return strings.Join(r.values, ",") }
func (r *repeated) Set(v string) error { r.values = append(r.values, v); return nil }
func pairs(vals []string) map[string]any {
	out := map[string]any{}
	for _, v := range vals {
		k, val, found := strings.Cut(v, "=")
		if !found {
			// A bare NAME means "pass mine through", which is how a secret
			// reaches a server without being written into a file.
			out[k] = os.Getenv(k)
			continue
		}
		out[k] = val
	}
	return out
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func quoteEmpty(v string) string {
	if v == "" {
		return `""`
	}
	return v
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func hotWords(hot bool) string {
	if hot {
		return "changeable at runtime"
	}
	return "read at startup; needs a restart"
}
