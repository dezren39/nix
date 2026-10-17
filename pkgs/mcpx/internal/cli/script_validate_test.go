package cli

import (
	"context"
	"strings"
	"testing"
)

func TestExecValidateFlag(t *testing.T) {
	app := &App{}
	ctx := context.Background()

	// 1. Missing snippet
	err := app.CmdExec(ctx, []string{"--validate"})
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got: %v", err)
	}

	// 2. Syntax error in snippet
	err = app.CmdExec(ctx, []string{"--validate", "const x = (1 + 2;"})
	if err == nil {
		t.Fatalf("expected validation error for unbalanced paren")
	}
}

func TestRunPromptFlagUsage(t *testing.T) {
	app := &App{}
	ctx := context.Background()

	// Calling with --prompt should not fail with "usage: mcpx run <script.ts>"
	err := app.CmdRun(ctx, []string{"--prompt", "test intent"})
	// It should attempt to connect to daemon or return a daemon connection error, NOT a flag/usage error!
	if err != nil && strings.Contains(err.Error(), "usage: mcpx run") {
		t.Fatalf("run --prompt should not fail with usage error: %v", err)
	}
}
