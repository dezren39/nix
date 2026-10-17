package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func copyExecutable(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

func awaitDaemonOtherThan(t *testing.T, e *env, pid int) daemonDoc {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if d := e.daemonState(t); d.PID != 0 && d.PID != pid {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon was never replaced (still pid %d)", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestUpgradeKeepsTheChildAndHandsMainPIDToTheSuccessor(t *testing.T) {
	e := newEnv(t, oneServer)
	sock, ln := listenNotify(t)
	e.envVars = append(e.envVars, "MCPX_DAEMON_WATCH_CONFIG=false", "NOTIFY_SOCKET="+sock)
	e.run("call", "demo.echo", `{"message":"before"}`)
	before := e.daemonState(t)
	child := before.childOf("demo")
	if child == 0 {
		t.Fatalf("no live child before the upgrade: %+v", before)
	}

	next := filepath.Join(t.TempDir(), "mcpx-next")
	copyExecutable(t, e.mcpx, next)
	if out, err := e.tryWith(next, "upgrade"); err != nil {
		t.Fatalf("upgrade failed: %v\n%s", err, out)
	}

	after := awaitDaemonOtherThan(t, e, before.PID)
	if got := after.childOf("demo"); got != child {
		t.Fatalf("the child was replaced by the upgrade: pid %d -> %d", child, got)
	}
	if out := e.run("call", "demo.echo", `{"message":"after"}`); !strings.Contains(out, "after") {
		t.Fatalf("the upgraded daemon did not answer through the child:\n%s", out)
	}
	awaitMainPID(t, ln, after.PID)
	awaitExit(t, before.PID, "the previous daemon")
	if !processExists(child) {
		t.Fatalf("the child %d died with the previous daemon", child)
	}

	out, err := e.tryWith(next, "upgrade")
	if err != nil {
		t.Fatalf("a second upgrade failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "already current") {
		t.Fatalf("a second upgrade did not report the daemon as current:\n%s", out)
	}
}

func TestUpgradeWithNoDaemonToUpgradeFails(t *testing.T) {
	e := newEnv(t, oneServer)
	out, err := e.try("upgrade")
	if err == nil {
		t.Fatalf("an upgrade with no daemon running succeeded:\n%s", out)
	}
	if !strings.Contains(out, "no mcpx daemon is running") {
		t.Fatalf("the failure does not say why:\n%s", out)
	}
}
