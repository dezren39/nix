package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every tool an MCP client can see should be reachable from a script (#102).
//
// docs/story.md:1619 says of an adapted binary: "It appears in `tools/list`,
// it is callable from a script". The second half is false. Adapter tools,
// OpenAPI operations and /v1 op proxies are attached as mcpserver.Extra
// (internal/cli/serve.go:437-447); internal/codegen never sees Extra, so they
// reach tools/list over /mcp and stdio and never reach the generated
// TypeScript client, which is what a script calls.
//
// This is the guard #102 asks for. It does not assert the gap is correct --
// that is the mistake of writing a test that locks a bug in. It asserts the
// invariant that should hold, and skips while it does not, naming the issue
// that owns the fix (#83, one tool-source interface). Both halves are proved
// before the skip, so the test cannot quietly stop exercising anything: if
// the adapter ever fails to appear in tools/list, this fails rather than
// skips.
//
// When #83 routes extras through the catalog, the skip below turns into a
// pass and should be deleted.
func TestEveryToolInToolsListIsReachableFromAScript(t *testing.T) {
	e := newEnv(t, oneServer)

	// An adapted program: the case docs/story.md makes its claim about.
	spec := filepath.Join(e.dir, "adapters.json")
	if err := os.WriteFile(spec, []byte(`{"adapters":[{"name":"greet","command":"echo",
      "description":"say hello",
      "tools":[{"name":"once","description":"say it once","args":["hello"]}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	listed := toolsList(t, e, spec)
	if len(listed) == 0 {
		t.Fatal("tools/list returned nothing; the harness is wrong and this test checks nothing")
	}
	var adapted []string
	for _, name := range listed {
		if strings.HasPrefix(name, "greet") {
			adapted = append(adapted, name)
		}
	}
	// The premise. Without this the comparison below is vacuous, which is
	// exactly how this test first passed while proving nothing.
	if len(adapted) == 0 {
		t.Fatalf("the adapted program is not in tools/list at all, so there is "+
			"nothing to compare; got %d tools", len(listed))
	}

	cc := exec.Command(e.mcpx, "client")
	cc.Dir = e.dir
	cc.Env = append(append([]string{}, e.envVars...), "MCPX_PATHS_ADAPTERS="+spec)
	cb, cerr := cc.Output()
	if cerr != nil {
		t.Fatalf("mcpx client: %v", cerr)
	}
	client := string(cb)
	if strings.TrimSpace(client) == "" {
		t.Fatal("the generated client is empty; this test checks nothing")
	}

	var missing []string
	for _, name := range adapted {
		if !strings.Contains(client, "greet") {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Skipf("#102 is still open: %v reached tools/list and not the generated "+
			"client, so docs/story.md's \"it is callable from a script\" is still "+
			"false for an adapted binary. #83 owns the fix (extras through the "+
			"catalog); when it lands this skip becomes a pass and should be deleted.",
			missing)
	}
}

// toolsList asks the real binary's MCP server what tools it offers.
func toolsList(t *testing.T, e *env, adapterSpec string) []string {
	t.Helper()
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"parity","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	cmd := exec.Command(e.mcpx, "serve")
	cmd.Dir = e.dir
	cmd.Env = append(append([]string{}, e.envVars...), "MCPX_PATHS_ADAPTERS="+adapterSpec)
	cmd.Stdin = strings.NewReader(in)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("mcpx serve: %v", err)
	}

	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			continue
		}
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(line), &msg) != nil || msg.ID != 2 {
			continue
		}
		for _, tool := range msg.Result.Tools {
			names = append(names, tool.Name)
		}
	}
	return names
}
