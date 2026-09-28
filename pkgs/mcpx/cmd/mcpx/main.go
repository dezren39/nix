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
	if strings.ContainsAny(arg, "(){};=") || strings.Contains(arg, "await ") ||
		strings.Contains(arg, "console.") || strings.Contains(arg, "tools.") {
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

	args := os.Args[1:]
	// Global flags may appear before the subcommand.
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch {
		case args[0] == "--json":
			app.JSON = true
			args = args[1:]
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
			fmt.Fprintf(os.Stderr, "unknown global flag %q\n", args[0])
			os.Exit(2)
		}
	}

	if len(args) == 0 {
		_ = app.CmdHelp(ctx, nil)
		return
	}

	cmd, rest := args[0], args[1:]
	handlers := map[string]func(context.Context, []string) error{
		"ls":         app.CmdLs,
		"list":       app.CmdLs,
		"namespaces": app.CmdLs,
		"types":      app.CmdTypes,
		"catalog":    app.CmdCatalog,
		"search":     app.CmdSearch,
		"call":       app.CmdCall,
		"run":        app.CmdRun,
		"exec":       app.CmdExec,
		"client":     app.CmdClient,
		"status":     app.CmdStatus,
		"refresh":    app.CmdRefresh,
		"restart":    app.CmdRestart,
		"stop":       app.CmdStop,
		"daemons":    app.CmdDaemons,
		"scripts":    app.CmdScripts,
		"log":        app.CmdLog,
		"logs":       app.CmdLog,
		"stats":      app.CmdStats,
		"daemon":     app.CmdDaemon,
		"config":     app.CmdConfig,
		"init":       app.CmdInit,
		"help":       app.CmdHelp,
		"man":        app.CmdMan,
		"completion": app.CmdCompletion,
	}

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

	if err := h(ctx, rest); err != nil {
		if errors.Is(err, cli.ErrNoDaemon) {
			fmt.Fprintln(os.Stderr, "mcpx: daemon is not running and could not be started")
		} else {
			fmt.Fprintln(os.Stderr, "mcpx:", err)
		}
		os.Exit(1)
	}
}
