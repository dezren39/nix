package cli

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// CmdDashboard serves or opens the live web dashboard.
func (a *App) CmdDashboard(ctx context.Context, args []string) error {
	fs := newFlagSet("dashboard")
	openFlag := fs.Bool("open", false, "open dashboard in default web browser")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}

	c := a.Client()
	if err := c.EnsureDaemon(ctx); err != nil {
		return err
	}
	ep, err := c.Endpoint(ctx)
	if err != nil {
		return err
	}
	dashURL := strings.TrimRight(ep, "/") + "/dashboard"
	fmt.Printf("mcpx dashboard running at %s\n", dashURL)
	if *openFlag {
		openBrowser(dashURL)
	}
	return nil
}

func openBrowser(target string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	_ = cmd.Start()
}
