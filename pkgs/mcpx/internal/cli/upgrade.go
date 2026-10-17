package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CmdUpgrade hands the running daemon over to this binary. It runs from the
// new build, so an activation script does not need the old daemon's path.
// It never starts a daemon: with none running there is nothing to upgrade.
func (a *App) CmdUpgrade(ctx context.Context, args []string) error {
	if err := parseFlags(a, newFlagSet("upgrade"), args); err != nil {
		return err
	}
	paths, _ := a.resolvePathsForConfig()
	c := NewClient(paths, a.ConfigPath)
	if !c.Ping(ctx) {
		return errors.New("no mcpx daemon is running for this configuration, so there is nothing to upgrade")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	start := unitStart(ctx)
	res, err := c.Upgrade(ctx, exe, start)
	if err != nil {
		return fmt.Errorf("upgrade: %w", err)
	}
	if res["status"] == "current" {
		fmt.Println("already current")
		return nil
	}
	if len(start) == 0 {
		fmt.Printf("upgraded the running daemon to %s (no unit start command: the successor takes this daemon's environment)\n", exe)
		return nil
	}
	fmt.Printf("upgraded the running daemon to %s, started through %s\n", exe, strings.Join(start, " "))
	return nil
}

// unitStart is what the mcpx unit runs ahead of the mcpx binary, such as the
// wrapper that sets the service's environment, or nil when the unit starts
// mcpx directly or there is no unit. Asked of systemd rather than assumed, so
// the successor is started the way the unit would start it.
func unitStart(ctx context.Context) []string {
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "show",
		"--property=ExecStart", "--value", "mcpx.service").Output()
	if err != nil {
		return nil
	}
	return startPrefix(string(out))
}

// startPrefix reads the argv from `systemctl show --property=ExecStart`,
// which prints `{ path=... ; argv[]=<argv> ; ignore_errors=no ; ... }`, and
// returns what comes before the mcpx binary. The unit's command is always
// `<prefix...> <mcpx> daemon`, so the prefix is everything before the last two.
func startPrefix(show string) []string {
	const key = "argv[]="
	i := strings.Index(show, key)
	if i < 0 {
		return nil
	}
	argv := show[i+len(key):]
	if j := strings.Index(argv, " ;"); j >= 0 {
		argv = argv[:j]
	}
	f := strings.Fields(argv)
	if len(f) < 2 || f[len(f)-1] != "daemon" || filepath.Base(f[len(f)-2]) != "mcpx" {
		return nil
	}
	if len(f) == 2 {
		return nil
	}
	return f[:len(f)-2]
}
