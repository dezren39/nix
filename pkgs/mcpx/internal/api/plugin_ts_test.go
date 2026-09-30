package api_test

import (
	"flag"
	"os"
	"testing"

	"github.com/dezren39/mcpx/internal/api"
)

var update = flag.Bool("update", false, "rewrite generated files instead of comparing them")

const pluginOpsPath = "../../plugin/opencode/mcpx/ops.gen.ts"

// TestPluginOpsAreGenerated fails when the plugin's operation methods no
// longer match the table -- an operation added, a parameter renamed -- so
// the plugin cannot fall behind /v1 the way its hand-written methods did.
func TestPluginOpsAreGenerated(t *testing.T) {
	want := api.PluginTS()
	if *update {
		if err := os.WriteFile(pluginOpsPath, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(pluginOpsPath)
	if err != nil {
		t.Fatalf("%s should exist: %v", pluginOpsPath, err)
	}
	if string(got) != want {
		t.Errorf("%s is stale; regenerate it with\n  go test ./internal/api -run TestPluginOpsAreGenerated -update", pluginOpsPath)
	}
}
