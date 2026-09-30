package mcpclient

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A child that refuses to start explains itself on stderr and exits. The
// first thing mcpx does to it is *write* -- the `initialize` frame -- and
// cmd.Wait closes our end of its stdin the moment it is reaped, so that write
// fails with "file already closed". Send used to sample the stderr ring at
// that instant; the goroutine filling it had not necessarily run, so the
// error carried "(stderr: )" and the server's own reason was lost. That is
// what failed TestStartFailureIsReportedWithStderr in CI.
//
// The race is made deterministic here rather than hoped for: a grandchild
// holds the stderr pipe open and prints after its parent has already exited
// and been reaped, so at the moment of the failed write the ring is
// guaranteed empty. Only a Send that waits for the drain can report the
// message.
func TestAFailedWriteCarriesStderrTheChildHadNotPrintedYet(t *testing.T) {
	const marker = "REFUSING-TO-START-MARKER"
	// The marker travels in the environment, not in the argument vector.
	// Spelled into the command line it would appear in t.label, which is in
	// the error text either way -- an assertion that passes with and without
	// the fix proves nothing.
	tr, err := NewStdio(StdioOptions{
		Command:    "sh",
		Args:       []string{"-c", "( sleep 0.4; echo \"$MCPX_TEST_MARKER\" >&2 ) & exit 3"},
		Env:        map[string]string{"MCPX_TEST_MARKER": marker},
		InheritEnv: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	// The parent is reaped well before the grandchild speaks.
	select {
	case <-tr.exited:
	case <-time.After(10 * time.Second):
		t.Fatal("child never exited")
	}
	if got := tr.stderr.Tail(400); got != "" {
		t.Fatalf("the ring should still be empty at this point, got %q", got)
	}

	err = tr.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if err == nil {
		t.Fatal("writing to a dead child should fail")
	}
	if !strings.Contains(err.Error(), marker) {
		t.Fatalf("the error should carry what the server said about refusing to start, got: %v", err)
	}
}
