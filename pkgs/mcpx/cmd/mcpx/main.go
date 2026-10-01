// Command mcpx runs TypeScript against local MCP servers.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dezren39/mcpx/internal/cli"
	"github.com/dezren39/mcpx/internal/daemon"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.1.0"

// splitList accepts comma or space separated values.
func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
}

// looksRunnable reports whether a bare first argument should be run, and as
// what. It is deliberately conservative: a bare word that is not a file is
// left to fail as an unknown command, because guessing that `mcpx serach` was
// a snippet would replace a clear error with a baffling one.
func looksRunnable(arg string) string {
	if arg == "" || strings.HasPrefix(arg, "-") {
		return ""
	}
	for _, ext := range []string{".ts", ".js", ".mts", ".mjs"} {
		if strings.HasSuffix(arg, ext) {
			return "file"
		}
	}
	// Deliberately narrow. An "=" alone is not evidence of source -- a
	// mistyped flag has one -- and treating it as such would run a typo as a
	// program. These markers do not occur in a command name.
	if strings.ContainsAny(arg, "(){};") || strings.Contains(arg, "await ") ||
		strings.Contains(arg, "console.") || strings.Contains(arg, "tools.") ||
		strings.Contains(arg, "=>") {
		return "source"
	}
	if st, err := os.Stat(arg); err == nil && !st.IsDir() {
		return "file"
	}
	return ""
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app := &cli.App{Version: version, Paths: daemon.ResolvePaths()}
	// Script resolution is reached from places that do not carry an App, so
	// it is told once where to read settings from.
	cli.SetPlumbingSource(app)

	args := os.Args[1:]
	// Global flags may appear before the subcommand.
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch {
		case args[0] == "--json":
			app.JSON = true
			args = args[1:]
		case args[0] == "--tui":
			// A global flag as well as a subcommand, because it is as likely
			// to be reached for mid-command ("...actually, show me") as
			// chosen up front.
			if err := app.CmdTUI(ctx, nil); err != nil {
				fmt.Fprintln(os.Stderr, "mcpx:", err)
				os.Exit(1)
			}
			return
		case args[0] == "--profile" && len(args) > 1:
			app.Profile.Names = append(app.Profile.Names, splitList(args[1])...)
			args = args[2:]
		case strings.HasPrefix(args[0], "--profile="):
			app.Profile.Names = append(app.Profile.Names, splitList(strings.TrimPrefix(args[0], "--profile="))...)
			args = args[1:]
		case args[0] == "--skip-default":
			app.Profile.SkipDefault = true
			args = args[1:]
		case args[0] == "--all-profiles":
			app.Profile.All = true
			args = args[1:]
		case args[0] == "--config" && len(args) > 1:
			app.ConfigPath = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--config="):
			app.ConfigPath = strings.TrimPrefix(args[0], "--config=")
			args = args[1:]
		case args[0] == "--version" || args[0] == "-v":
			fmt.Println("mcpx", version)
			return
		case args[0] == "--help" || args[0] == "-h":
			_ = app.CmdHelp(ctx, nil)
			return
		default:
			// Any setting every command accepts is also a global flag, so
			// `mcpx --plumbing-strict-unknown-keys ls` means what
			// `mcpx ls --plumbing-strict-unknown-keys` does.
			rest, err := app.ApplyGlobalSettingFlags(args)
			if err != nil {
				fmt.Fprintln(os.Stderr, "mcpx:", err)
				os.Exit(2)
			}
			if len(rest) < len(args) {
				args = rest
				continue
			}
			fmt.Fprintf(os.Stderr, "unknown global flag %q\n", args[0])
			os.Exit(2)
		}
	}

	if len(args) == 0 {
		// Bare `mcpx` opens the browser when there is a terminal to draw on,
		// and prints help when there is not. Help was the old behaviour and
		// is still the right answer for a pipe; for a person at a prompt,
		// a wall of usage is a worse first impression than the thing itself.
		if err := app.CmdTUI(ctx, nil); err != nil {
			_ = app.CmdHelp(ctx, nil)
		}
		return
	}

	cmd, rest := args[0], args[1:]
	// The table lives in the cli package, beside Commands(), so a test can
	// hold the two together; here it drifted from the declared list by seven.
	handlers := app.Handlers()

	h, ok := handlers[cmd]
	if !ok {
		// An argument that is obviously a script or a snippet runs, rather
		// than being refused for not being a subcommand. The alternative --
		// insisting on `mcpx run` -- rejects the most natural thing to type
		// in order to protect a namespace that has no collisions in it.
		if kind := looksRunnable(cmd); kind != "" {
			if kind == "file" {
				h, rest = app.CmdRun, args
			} else {
				h, rest = app.CmdExec, args
			}
		} else {
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
			_ = app.CmdHelp(ctx, nil)
			os.Exit(2)
		}
	}

	// Settings are resolved before dispatch, so a contradiction in a config
	// file is reported once, up front, rather than by whichever command
	// happens to read the offending value first.
	if err := app.SettingsErr(); err != nil {
		fmt.Fprintln(os.Stderr, "mcpx:", err)
		os.Exit(2)
	}

	// An in-process daemon lives exactly as long as this command. Stopped on
	// every exit path, including an error, or its servers would outlive the
	// process that owns them.
	defer app.CloseInline()

	if err := h(ctx, rest); err != nil {
		var ec cli.ExitCoder
		if errors.As(err, &ec) {
			fmt.Fprintln(os.Stderr, "mcpx:", err)
			app.CloseInline()
			os.Exit(ec.ExitCode())
		}
		if errors.Is(err, cli.ErrNoDaemon) {
			fmt.Fprintln(os.Stderr, "mcpx: daemon is not running and could not be started")
		} else {
			fmt.Fprintln(os.Stderr, "mcpx:", err)
		}
		os.Exit(1)
	}
}
