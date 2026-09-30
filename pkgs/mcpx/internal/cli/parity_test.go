package cli

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/api"
)

var update = flag.Bool("update", false, "rewrite generated files instead of comparing them")

const (
	parityPath   = "../../docs/parity.md"
	daemonTSPath = "../../plugin/opencode/mcpx/daemon.ts"
)

// TestParityDocumentIsCurrent fails when docs/parity.md no longer matches the
// declarations it is generated from -- an operation, a command or a plugin
// method added without regenerating it.
func TestParityDocumentIsCurrent(t *testing.T) {
	ts, err := os.ReadFile(daemonTSPath)
	if err != nil {
		t.Fatal(err)
	}
	want := ParityDoc(string(ts))
	if *update {
		if err := os.WriteFile(parityPath, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(parityPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("docs/parity.md is stale; regenerate it with\n  go test ./internal/cli -run TestParityDocumentIsCurrent -update")
	}
}

// TestEveryCommandReachesAnOperationOrSaysWhy is the CLI -> /v1 direction of
// parity. A command with no operation behind it is something only the CLI
// can do; that may be right -- init writes into the current directory -- but
// it has to be said, or it is a gap nobody decided on.
func TestEveryCommandReachesAnOperationOrSaysWhy(t *testing.T) {
	reach := commandOps()
	for _, c := range Commands() {
		n := len(reach[c.Name])
		switch {
		case n == 0 && c.Local == "":
			t.Errorf("%s reaches no /v1 operation and does not say why (set Local, or name it as an operation's Command)", c.Name)
		case n > 0 && c.Local != "":
			t.Errorf("%s says it is local (%q) but is the CLI for %v", c.Name, c.Local, reach[c.Name])
		}
	}
}

func TestEveryPluginOpTagNamesAnOperation(t *testing.T) {
	ts, err := os.ReadFile(daemonTSPath)
	if err != nil {
		t.Fatal(err)
	}
	methods := PluginMethods(string(ts))
	if tags := strings.Count(string(ts), "@op "); tags != len(methods) {
		t.Errorf("daemon.ts has %d @op tags and %d were matched to a method; "+
			"the tag must end the doc comment directly above the method", tags, len(methods))
	}
	for _, m := range methods {
		if _, ok := api.ByName(m.Op); !ok {
			t.Errorf("DaemonClient.%s says it performs %q, which is not an operation", m.Method, m.Op)
		}
	}
}
