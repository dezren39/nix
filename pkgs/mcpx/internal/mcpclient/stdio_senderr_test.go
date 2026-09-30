package mcpclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A write to a server that is refusing to start must say why it refused.
//
// Recv waited for the child to be reaped and its stderr drained before
// reporting; Send did not. So whether a refusing server's reason reached the
// person depended on which side of the exchange noticed first, and on a
// loaded machine it was the write: internal/pool's
// TestStartFailureIsReportedWithStderr failed in the Nix sandbox with
//
//	initialize: write to .../fakemcp: write |1: broken pipe (stderr: )
//
// "a pipe broke" instead of "the server refused to start, and here is why".
// The same defect this package's NewStdio comment records having fixed on the
// read side, left standing on the write side.
//
// The fixture makes the losing order the only order, rather than hoping for
// it: the child drops the read end of its stdin first, so the next write is
// certain to fail, and only prints its reason afterwards. An unfixed Send
// returns before that reason exists and reports an empty stderr; a fixed one
// waits for the exit and the drain and reports the reason.
func TestAWriteToARefusingServerCarriesItsStderr(t *testing.T) {
	script := filepath.Join(t.TempDir(), "refuse.sh")
	const reason = "REFUSING: no credentials in the environment"
	// exec 0<&- drops the read end, so the parent's next write gets EPIPE
	// while the child is still alive and has said nothing yet.
	if err := os.WriteFile(script, []byte(
		"#!/bin/sh\nexec 0<&-\nsleep 0.5\necho '"+reason+"' >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	tr, err := NewStdio(StdioOptions{Command: script, InheritEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	// Retry until a write fails: the first one can land before the child has
	// run its first line.
	var sendErr error
	deadline := time.Now().Add(10 * time.Second)
	for sendErr == nil && time.Now().Before(deadline) {
		sendErr = tr.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		if sendErr == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if sendErr == nil {
		t.Fatal("writing to a server that closed its stdin should fail; it did not, " +
			"so this test checks nothing")
	}
	if !strings.Contains(sendErr.Error(), reason) {
		t.Fatalf("the error should carry the server's reason for refusing, got: %v", sendErr)
	}
}
