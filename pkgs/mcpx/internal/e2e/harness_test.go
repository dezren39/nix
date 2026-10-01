package e2e_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A child the harness kills at its deadline must say so, not "signal: killed".
func TestTheHarnessNamesItsOwnDeadlineAsTheKiller(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := harnessTimeout(ctx, exec.CommandContext(ctx, "sleep", "5").Run())
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("err = %v", err)
	}
	if err := harnessTimeout(context.Background(), nil); err != nil {
		t.Fatalf("a run that succeeded is not an error: %v", err)
	}
}
