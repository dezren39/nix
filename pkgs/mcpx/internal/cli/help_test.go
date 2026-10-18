package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout swaps os.Stdout for a pipe while fn runs and returns what
// was written. Help is printed with fmt.Print, so this is the only way to
// see it from a test.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fnErr := fn()
	os.Stdout = old
	w.Close()
	out := <-done
	r.Close()
	return out, fnErr
}

// TestSubcommandHelpFlagPrintsCommandHelp is the regression test for issue
// #368: `mcpx call --help` printed "Usage of call:" followed by every
// global setting. It must print the same help as `mcpx help call` and
// report flag.ErrHelp, so the caller can treat it as success.
func TestSubcommandHelpFlagPrintsCommandHelp(t *testing.T) {
	want, err := (&App{}).helpText("call")
	if err != nil {
		t.Fatal(err)
	}
	for _, spelling := range []string{"--help", "-h", "-help"} {
		out, err := captureStdout(t, func() error {
			return (&App{}).CmdCall(context.Background(), []string{spelling})
		})
		if !errors.Is(err, flag.ErrHelp) {
			t.Errorf("%s: err = %v, want flag.ErrHelp", spelling, err)
		}
		if out != want {
			t.Errorf("%s: output is not `mcpx help call` output\ngot:\n%s\nwant:\n%s", spelling, out, want)
		}
		if strings.Contains(out, "Usage of call:") || strings.Contains(out, "artifacts-chunk-bytes") {
			t.Errorf("%s: output contains the flag-package settings dump", spelling)
		}
	}
}

// TestHandlersTurnHelpIntoSuccess pins the dispatch layer of #368: through
// Handlers() a help flag is a successful run, for commands that parse
// flags and for ones that never do (daemons, refresh).
func TestHandlersTurnHelpIntoSuccess(t *testing.T) {
	for _, cmd := range []string{"call", "daemons", "refresh"} {
		out, err := captureStdout(t, func() error {
			return (&App{}).Handlers()[cmd](context.Background(), []string{"--help"})
		})
		if err != nil {
			t.Errorf("%s --help: err = %v, want nil", cmd, err)
		}
		if !strings.Contains(out, "mcpx "+cmd) {
			t.Errorf("%s --help: output does not name the command:\n%s", cmd, out)
		}
		if strings.Contains(out, "Usage of "+cmd+":") {
			t.Errorf("%s --help: output contains the flag-package settings dump", cmd)
		}
	}
}

// TestWantsHelpMirrorsTheFlagParser pins which positions count as a help
// request: flag position only, never a flag's value, a positional's
// trailing arguments, or anything after "--".
func TestWantsHelpMirrorsTheFlagParser(t *testing.T) {
	fs := flag.NewFlagSet("call", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("session", "", "")
	fs.Bool("raw", false, "")
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"--help"}, true},
		{[]string{"-h"}, true},
		{[]string{"-help"}, true},
		{[]string{"--raw", "--help"}, true},
		{[]string{"--raw=true", "-h"}, true},
		{[]string{"--session", "s", "--help"}, true},
		{[]string{"--session=s", "--help"}, true},
		{[]string{"--session", "--help"}, false}, // --help is the session's value
		{[]string{"--session=--help"}, false},
		{[]string{"ns.tool", "--help"}, false}, // after a positional: the call's argument
		{[]string{"--", "--help"}, false},
		{[]string{"--bogus", "--help"}, false}, // the parser reports --bogus first
		{nil, false},
	}
	for _, c := range cases {
		if got := wantsHelp(fs, c.args); got != c.want {
			t.Errorf("wantsHelp(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

// TestHelpTextFallsBackToParentCommand: a subcommand's flag set is named
// for both words ("log record"), and its help is the parent command's.
func TestHelpTextFallsBackToParentCommand(t *testing.T) {
	got, err := (&App{}).helpText("log record")
	if err != nil {
		t.Fatal(err)
	}
	want, err := (&App{}).helpText("log")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("helpText(\"log record\") is not helpText(\"log\")")
	}
	if _, err := (&App{}).helpText("no-such-command"); err == nil {
		t.Error("helpText for an unknown command: want an error, got nil")
	}
}
