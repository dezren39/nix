package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/dezren39/mcpx/internal/secrets"
)

// CmdSecrets gives servers the secrets they need, in memory only.
//
// A server that requires a secret with no value does not start, and a script
// that reaches it is refused before it runs. The value is the one thing a
// person must type, so it comes from standard input and never from the command
// line, where it would sit in shell history and in the process table.
func (a *App) CmdSecrets(ctx context.Context, args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		return a.secretsList(ctx, args)
	case "set":
		return a.secretsSet(ctx, args)
	case "unset", "rm", "delete":
		return a.secretsUnset(ctx, args)
	case "requests":
		return a.secretsRequests(ctx)
	case "request":
		return a.secretsRequest(ctx, args)
	case "decline":
		return a.secretsDecline(ctx, args)
	}
	return fmt.Errorf("no secrets subcommand %q; list, set, unset, requests, request or decline", sub)
}

func (a *App) secretsList(ctx context.Context, args []string) error {
	fs := newFlagSet("secrets")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	raw, err := c.Do(ctx, http.MethodGet, "/v1/secrets", nil)
	if err != nil {
		return err
	}
	var out struct {
		Secrets []secretRow `json:"secrets"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if a.JSON {
		return a.out(out)
	}
	if len(out.Secrets) == 0 {
		fmt.Println("no secrets are needed: no configured server requires one, and none has been given a value")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSET\tSOURCE")
	for _, s := range out.Secrets {
		source := "-"
		if s.Source != nil {
			source = *s.Source
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, yesNo(s.Set), source)
	}
	return tw.Flush()
}

// secretRow is one name in the listing. It has no field for a value, because
// the daemon never sends one.
type secretRow struct {
	Name   string  `json:"name"`
	Set    bool    `json:"set"`
	Source *string `json:"source"`
}

func (a *App) secretsSet(ctx context.Context, args []string) error {
	fs := newFlagSet("secrets")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		// The message names what is wrong without repeating what was typed:
		// with two words after set, the second may well be the value.
		return errors.New("usage: mcpx secrets set <NAME>, with the value on standard input\n" +
			"       printf '%s' \"$VALUE\" | mcpx secrets set <NAME>")
	}
	name := fs.Arg(0)
	if !secrets.ValidName(name) {
		return fmt.Errorf("%q is not an environment variable name", name)
	}
	value, err := readSecretValue(name)
	if err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	raw, err := c.Do(ctx, http.MethodPut, "/v1/secrets/"+urlEscape(name), mustMarshal(map[string]any{"value": value}))
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
	fmt.Printf("%s is set in the running daemon. It is not written to disk, and servers that need it can run now.\n", name)
	return nil
}

// readSecretValue reads one value from standard input. On a terminal the value
// is read with echo off, after a prompt on stderr. Anywhere else the whole of
// standard input is the value, less one trailing newline, as echo would add.
func readSecretValue(name string) (string, error) {
	if isTerminal(os.Stdin) {
		fmt.Fprintf(os.Stderr, "value for %s (input hidden): ", name)
		v, err := readHidden(os.Stdin)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		if v == "" {
			return "", fmt.Errorf("no value given for %s", name)
		}
		return v, nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	v := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	if v == "" {
		return "", fmt.Errorf("no value on standard input for %s; pipe one in, for example "+
			"printf '%%s' \"$VALUE\" | mcpx secrets set %s", name, name)
	}
	return v, nil
}

func (a *App) secretsUnset(ctx context.Context, args []string) error {
	fs := newFlagSet("secrets")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mcpx secrets unset <NAME>")
	}
	name := fs.Arg(0)
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	raw, err := c.Do(ctx, http.MethodDelete, "/v1/secrets/"+urlEscape(name), nil)
	if err != nil {
		return err
	}
	var out struct {
		Name string `json:"name"`
		Set  bool   `json:"set"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if a.JSON {
		return a.out(out)
	}
	if out.Set {
		fmt.Printf("%s's value from this command is gone. The environment still has one, so servers that need it are unchanged.\n", name)
		return nil
	}
	fmt.Printf("%s is no longer set. Servers that need it wait until it has a value again.\n", name)
	return nil
}

func (a *App) secretsRequests(ctx context.Context) error {
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	raw, err := c.Do(ctx, http.MethodGet, "/v1/secret-requests", nil)
	if err != nil {
		return err
	}
	var out struct {
		Requests []secrets.Request `json:"requests"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if a.JSON {
		return a.out(out)
	}
	if len(out.Requests) == 0 {
		fmt.Println("no secret requests")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tSERVER\tSECRETS")
	for _, r := range out.Requests {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, r.Status, r.Server, strings.Join(r.Secrets, ", "))
	}
	return tw.Flush()
}

func (a *App) secretsRequest(ctx context.Context, args []string) error {
	fs := newFlagSet("secrets")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mcpx secrets request <id>")
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	raw, err := c.Do(ctx, http.MethodGet, "/v1/secret-requests/"+urlEscape(fs.Arg(0)), nil)
	if err != nil {
		return err
	}
	return a.printSecretRequest(raw)
}

func (a *App) secretsDecline(ctx context.Context, args []string) error {
	fs := newFlagSet("secrets")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mcpx secrets decline <id>")
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	raw, err := c.Do(ctx, http.MethodDelete, "/v1/secret-requests/"+urlEscape(fs.Arg(0)), nil)
	if err != nil {
		return err
	}
	return a.printSecretRequest(raw)
}

func (a *App) printSecretRequest(raw []byte) error {
	var r secrets.Request
	if err := json.Unmarshal(raw, &r); err != nil {
		return err
	}
	if a.JSON {
		return a.out(r)
	}
	fmt.Printf("%s: %s, for %s, needs %s\n", r.ID, r.Status, r.Server, strings.Join(r.Secrets, ", "))
	return nil
}
