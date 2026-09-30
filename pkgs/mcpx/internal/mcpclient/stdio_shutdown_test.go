package mcpclient

import (
	"strings"
	"testing"
	"time"
)

// Every revision's stdio shutdown (named for 2025-11-25; 2024-11-05 onward
// say the same): the client SHOULD close the server's
// stdin, wait for it to exit, SIGTERM if it does not within a reasonable
// time, and SIGKILL if it still does not.
// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#stdio
// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio
func TestStdioShutdown(t *testing.T) {
	// The graces are generous where the server is expected to exit on its
	// own (a loaded machine must not turn a clean exit into a signal) and
	// short where it is expected not to.
	const long, short = 30 * time.Second, 50 * time.Millisecond
	end := func(t *testing.T, script string, stdinGrace, termGrace time.Duration) string {
		t.Helper()
		tr, err := NewStdio(StdioOptions{Command: "sh", Args: []string{"-c", script},
			InheritEnv: true, StdinGrace: stdinGrace, TermGrace: termGrace})
		if err != nil {
			t.Fatal(err)
		}
		// Each script says "ready" once its traps are set, so a signal
		// cannot arrive before the script is in the state under test.
		if line, err := readLine(tr.stdout); err != nil || string(line) != "ready" {
			t.Fatalf("server never became ready: %q %v", line, err)
		}
		_ = tr.Close()
		<-tr.exited
		if tr.waitErr == nil {
			return "exited"
		}
		return tr.waitErr.Error()
	}

	t.Run("2025-11-25/lifecycle/stdio-shutdown-closes-stdin-and-lets-the-server-exit", func(t *testing.T) {
		// Exits on EOF, and would report a SIGTERM as a signal death.
		if got := end(t, "echo ready; cat >/dev/null", long, long); got != "exited" {
			t.Errorf("a server that exits on EOF was not allowed to: %s", got)
		}
	})

	t.Run("2025-11-25/lifecycle/stdio-shutdown-sigterms-a-server-that-ignores-eof", func(t *testing.T) {
		// Never reads stdin; dies to SIGTERM.
		if got := end(t, "echo ready; exec sleep 600", short, long); !strings.Contains(got, "terminated") {
			t.Errorf("want SIGTERM after the stdin grace, got %s", got)
		}
	})

	t.Run("2025-11-25/lifecycle/stdio-shutdown-sigkills-a-server-that-ignores-sigterm", func(t *testing.T) {
		// Ignoring a signal is inherited across exec, so sleep ignores it too.
		if got := end(t, `trap "" TERM; echo ready; exec sleep 600`, short, short); !strings.Contains(got, "killed") {
			t.Errorf("want SIGKILL after the term grace, got %s", got)
		}
	})
}
